package subsonic

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	g "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/appaccess"
	"github.com/datahearth/streamline/internal/ffmpeg"
)

type fakeTranscoder struct {
	path  string
	calls []ffmpeg.AudioTranscode
	out   string
	err   error
	// pause, when set, writes out one byte at a time with pause between.
	pause time.Duration
}

func (t *fakeTranscoder) FFmpegPath() string { return t.path }

func (t *fakeTranscoder) TranscodeAudio(
	_ context.Context,
	_ string,
	o ffmpeg.AudioTranscode,
	w io.Writer,
) error {
	t.calls = append(t.calls, o)
	if t.pause > 0 {
		for i := range len(t.out) {
			if _, err := io.WriteString(w, t.out[i:i+1]); err != nil {
				return err
			}
			time.Sleep(t.pause)
		}
		return t.err
	}
	if t.out != "" {
		if _, err := io.WriteString(w, t.out); err != nil {
			return err
		}
	}
	return t.err
}

var _ = g.DescribeTable(
	"streamPlan",
	func(suffix string, srcKbps int, format string, maxKbps int, want *ffmpeg.AudioTranscode) {
		got, convert := streamPlan(
			suffix,
			uint32(srcKbps),
			format,
			maxKbps,
		)
		if want == nil {
			Expect(convert).To(BeFalse())
			return
		}
		Expect(convert).To(BeTrue())
		Expect(got).To(Equal(*want))
	},
	g.Entry("no format, no cap: the original", "flac", 900, "", 0, nil),
	g.Entry("raw always wins", "flac", 900, "raw", 128, nil),
	g.Entry("the file's own format", "mp3", 320, "mp3", 0, nil),
	g.Entry("a format this server cannot make", "mp3", 320, "flac", 0, nil),
	g.Entry("under the cap", "mp3", 128, "", 192, nil),
	g.Entry("over the cap", "flac", 900, "", 192,
		&ffmpeg.AudioTranscode{Format: "mp3", BitrateKbps: 192}),
	g.Entry("unknown bitrate under a cap", "flac", 0, "", 256,
		&ffmpeg.AudioTranscode{Format: "mp3", BitrateKbps: 256}),
	g.Entry("another format at its default", "flac", 900, "opus", 0,
		&ffmpeg.AudioTranscode{Format: "opus", BitrateKbps: 128}),
	g.Entry("a format and a cap", "flac", 900, "OPUS", 96,
		&ffmpeg.AudioTranscode{Format: "opus", BitrateKbps: 96}),
	g.Entry("a cap past the encoder's ceiling", "flac", 900, "aac", 1411,
		&ffmpeg.AudioTranscode{Format: "aac", BitrateKbps: 320}),
	g.Entry("a cap below the floor", "flac", 900, "", 8,
		&ffmpeg.AudioTranscode{Format: "mp3", BitrateKbps: 32}),
	g.Entry(
		"an unknown format over the cap falls back to mp3",
		"flac",
		900,
		"wma",
		128,
		&ffmpeg.AudioTranscode{Format: "mp3", BitrateKbps: 128},
	),
	g.Label("unit"),
)

var _ = g.Describe("parseLRC", g.Label("unit"), func() {
	g.It(
		"reads timed lines, several stamps per line, sorted, with the offset",
		func() {
			p := parseLRC(
				"\ufeff[ar:Nirvana]\n[offset:+500]\n[00:12.50]Load up\n[00:05.00][01:00]Hello\n",
			)
			Expect(p.synced).To(BeTrue())
			starts := make([]int64, 0, len(p.lines))
			words := make([]string, 0, len(p.lines))
			for _, l := range p.lines {
				starts = append(starts, *l.Start)
				words = append(words, l.Value)
			}
			Expect(starts).To(Equal([]int64{4500, 12000, 59500}))
			Expect(words).To(Equal([]string{"Hello", "Load up", "Hello"}))
		},
	)

	g.It("keeps untimed text line by line, stanza breaks included", func() {
		p := parseLRC("\nFirst line\n\nSecond verse\n\n")
		Expect(p.synced).To(BeFalse())
		Expect(p.plain()).To(Equal("First line\n\nSecond verse"))
	})

	g.It("reads one- and three-digit fractions", func() {
		p := parseLRC("[00:01.5]a\n[00:02.250]b")
		Expect(*p.lines[0].Start).To(BeEquivalentTo(1500))
		Expect(*p.lines[1].Start).To(BeEquivalentTo(2250))
	})
})

var _ = g.Describe("media", g.Label("integration"), func() {
	var (
		f     *fixture
		trans *fakeTranscoder
		path  string
		audio = []byte("0123456789abcdef")
	)

	g.BeforeEach(func() {
		ctx := context.Background()
		f = newFixture(ctx)
		path = filepath.Join(g.GinkgoT().TempDir(), "01.flac")
		Expect(os.WriteFile(path, audio, 0o600)).To(Succeed())
		Expect(f.client.MediaFile.Update().SetPath(path).Exec(ctx)).To(Succeed())
		trans = &fakeTranscoder{path: "/usr/bin/ffmpeg", out: "MP3DATA"}
		f.handler = New(Deps{
			Ent:        f.client,
			Tracker:    appaccess.NewTracker(f.client),
			Transcoder: trans,
		}).Routes()
	})

	raw := func(endpoint string, extra url.Values) *httptest.ResponseRecorder {
		g.GinkgoHelper()
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, httptest.NewRequest(
			http.MethodGet,
			"/"+endpoint+"?"+signedQuery("sesame", extra).Encode(),
			nil,
		))
		return rec
	}

	g.It(
		"transcodes when the client caps the bitrate, starting at timeOffset",
		func() {
			rec := raw("stream", url.Values{
				"id": {tr(f.withFile)}, "maxBitRate": {"128"}, "timeOffset": {"42"},
			})
			Expect(rec.Code).To(Equal(http.StatusOK))
			Expect(rec.Header().Get("Content-Type")).To(Equal("audio/mpeg"))
			Expect(rec.Body.String()).To(Equal("MP3DATA"))
			Expect(trans.calls).To(Equal([]ffmpeg.AudioTranscode{
				{Format: "mp3", BitrateKbps: 128, OffsetSec: 42},
			}))
		},
	)

	g.It("sends the original when no conversion is asked for", func() {
		rec := raw("stream", url.Values{"id": {tr(f.withFile)}, "maxBitRate": {"0"}})
		Expect(rec.Body.Bytes()).To(Equal(audio))
		Expect(trans.calls).To(BeEmpty())
	})

	g.It("sends the original when ffmpeg is missing", func() {
		trans.path = ""
		rec := raw("stream", url.Values{"id": {tr(f.withFile)}, "format": {"mp3"}})
		Expect(rec.Body.Bytes()).To(Equal(audio))
	})

	g.It("answers a Subsonic error when ffmpeg fails before its first byte", func() {
		trans.out = ""
		trans.err = errors.New("boom")
		rec := raw("stream", url.Values{"id": {tr(f.withFile)}, "format": {"opus"}})
		Expect(rec.Header().Get("Content-Type")).To(HavePrefix("application/json"))
		Expect(rec.Body.String()).To(ContainSubstring(`"code":0`))
	})

	g.It(
		"sends the original instead of queueing when every transcode slot is busy",
		func() {
			h := New(
				Deps{
					Ent:        f.client,
					Tracker:    appaccess.NewTracker(f.client),
					Transcoder: trans,
				},
			)
			for range maxTranscodes {
				h.transcodes <- struct{}{}
			}
			rec := httptest.NewRecorder()
			h.Routes().ServeHTTP(rec, httptest.NewRequest(
				http.MethodGet,
				"/stream?"+signedQuery(
					"sesame",
					url.Values{"id": {tr(f.withFile)}, "format": {"mp3"}},
				).Encode(),
				nil,
			))
			Expect(rec.Body.Bytes()).To(Equal(audio))
			Expect(trans.calls).To(BeEmpty())

			<-h.transcodes
			rec = httptest.NewRecorder()
			h.Routes().ServeHTTP(rec, httptest.NewRequest(
				http.MethodGet,
				"/stream?"+signedQuery(
					"sesame",
					url.Values{"id": {tr(f.withFile)}, "format": {"mp3"}},
				).Encode(),
				nil,
			))
			Expect(rec.Body.String()).To(Equal("MP3DATA"))
			Expect(
				h.transcodes,
			).To(HaveLen(maxTranscodes-1), "the slot is given back")
		},
	)

	g.It("answers HEAD on a transcode with its type and no body", func() {
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, httptest.NewRequest(
			http.MethodHead,
			"/stream?"+signedQuery(
				"sesame",
				url.Values{"id": {tr(f.withFile)}, "format": {"opus"}},
			).Encode(),
			nil,
		))
		Expect(rec.Code).To(Equal(http.StatusOK))
		Expect(rec.Header().Get("Content-Type")).To(Equal("audio/ogg"))
		Expect(rec.Body.Len()).To(BeZero())
		Expect(trans.calls).To(BeEmpty())
	})

	g.It("downloads the original as an attachment", func() {
		rec := raw("download", url.Values{"id": {tr(f.withFile)}, "format": {"mp3"}})
		Expect(rec.Body.Bytes()).To(Equal(audio))
		Expect(
			rec.Header().Get("Content-Disposition"),
		).To(Equal(`attachment; filename=01.flac`))
		Expect(trans.calls).To(BeEmpty())
	})

	g.It("reads lyrics from a sidecar .lrc", func() {
		lrc := filepath.Join(filepath.Dir(path), "01.lrc")
		Expect(
			os.WriteFile(
				lrc,
				[]byte("[00:01.00]Load up on guns\n[00:03.00]Bring your friends\n"),
				0o600,
			),
		).
			To(Succeed())

		resp := okResp(
			f.call("getLyricsBySongId", url.Values{"id": {tr(f.withFile)}}),
		)
		sl := list(obj(resp, "lyricsList"), "structuredLyrics")
		Expect(sl).To(HaveLen(1))
		Expect(sl[0]["synced"]).To(BeTrue())
		lines := list(sl[0], "line")
		Expect(lines[1]["start"]).To(BeEquivalentTo(3000))
		Expect(lines[1]["value"]).To(Equal("Bring your friends"))

		ly := obj(okResp(f.call("getLyrics", url.Values{
			"artist": {"nirvana"}, "title": {"smells like teen spirit"},
		})), "lyrics")
		Expect(ly["value"]).To(Equal("Load up on guns\nBring your friends"))
		Expect(ly["artist"]).To(Equal("Nirvana"))
	})

	g.It("answers empty lyrics when there are none", func() {
		resp := okResp(
			f.call("getLyricsBySongId", url.Values{"id": {tr(f.withFile)}}),
		)
		Expect(list(obj(resp, "lyricsList"), "structuredLyrics")).To(BeEmpty())
		ly := obj(
			okResp(f.call("getLyrics", url.Values{"title": {"nothing"}})),
			"lyrics",
		)
		Expect(ly["value"]).To(BeEmpty())
	})

	g.It("has no avatars", func() {
		Expect(
			errCode(
				f.call(
					"getAvatar",
					url.Values{"username": {"listener@example.com"}},
				),
			),
		).
			To(BeEquivalentTo(70))
	})

	g.It("estimates an unprobed file's bitrate from the track length", func() {
		ctx := context.Background()
		// 3.2 MB over 200 s is 128 kbps, well under a 320 cap.
		Expect(f.client.MediaFile.Update().
			SetBitrate(0).
			SetDurationSeconds(0).
			SetSize(3_200_000).
			Exec(ctx)).To(Succeed())
		Expect(f.client.Track.UpdateOneID(f.withFile.ID).
			SetDuration(200).
			Exec(ctx)).To(Succeed())

		rec := raw(
			"stream",
			url.Values{"id": {tr(f.withFile)}, "maxBitRate": {"320"}},
		)
		Expect(rec.Body.Bytes()).To(Equal(audio))
		Expect(trans.calls).To(BeEmpty())
	})

	g.It("transcodes for a timeOffset even when no conversion is asked for", func() {
		rec := raw(
			"stream",
			url.Values{"id": {tr(f.withFile)}, "timeOffset": {"30"}},
		)
		Expect(rec.Body.String()).To(Equal("MP3DATA"))
		Expect(trans.calls).To(Equal([]ffmpeg.AudioTranscode{
			{Format: "mp3", BitrateKbps: 320, OffsetSec: 30},
		}))
	})

	g.It("refuses a timeOffset it cannot honour rather than play from 0", func() {
		trans.path = ""
		rec := raw("stream", url.Values{
			"id": {tr(f.withFile)}, "format": {"mp3"}, "timeOffset": {"30"},
		})
		Expect(rec.Body.String()).To(ContainSubstring(`"code":0`))
		Expect(rec.Body.Bytes()).NotTo(Equal(audio))

		trans.path = "/usr/bin/ffmpeg"
		h := New(Deps{
			Ent:        f.client,
			Tracker:    appaccess.NewTracker(f.client),
			Transcoder: trans,
		})
		for range maxTranscodes {
			h.transcodes <- struct{}{}
		}
		wait := offsetSlotWait
		offsetSlotWait = 50 * time.Millisecond
		g.DeferCleanup(func() { offsetSlotWait = wait })
		rec = httptest.NewRecorder()
		h.Routes().ServeHTTP(rec, httptest.NewRequest(
			http.MethodGet,
			"/stream?"+signedQuery("sesame", url.Values{
				"id": {tr(f.withFile)}, "format": {"mp3"}, "timeOffset": {"30"},
			}).Encode(),
			nil,
		))
		Expect(rec.Body.String()).To(ContainSubstring(`"code":0`))
		Expect(trans.calls).To(BeEmpty())
	})

	g.It("keeps a slow stream going past the listener's write timeout", func() {
		trans.out = strings.Repeat("x", 8)
		trans.pause = 60 * time.Millisecond
		srv := httptest.NewUnstartedServer(f.handler)
		srv.Config.WriteTimeout = 200 * time.Millisecond
		srv.Start()
		g.DeferCleanup(srv.Close)

		resp, err := http.Get(
			srv.URL + "/stream?" + signedQuery("sesame", url.Values{
				"id": {tr(f.withFile)}, "maxBitRate": {"128"},
			}).Encode(),
		)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		got, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(got)).To(Equal(trans.out))
	})

	g.It("sends a file larger than one sendfile step whole", func() {
		big := make([]byte, idleChunk*2+12345)
		for i := range big {
			big[i] = byte(i % 251)
		}
		Expect(os.WriteFile(path, big, 0o600)).To(Succeed())
		srv := httptest.NewServer(f.handler)
		g.DeferCleanup(srv.Close)

		resp, err := http.Get(
			srv.URL + "/download?" + signedQuery("sesame", url.Values{
				"id": {tr(f.withFile)},
			}).Encode(),
		)
		Expect(err).NotTo(HaveOccurred())
		defer resp.Body.Close()
		got, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(big))
	})
})

var _ = g.Describe("parseLRC tags", g.Label("unit"), func() {
	g.It("keeps a section header in plain lyrics and drops the ID tags", func() {
		p := parseLRC("[ar: Rihanna]\n[ti:Work]\n[Chorus: Rihanna]\nwork work\n")
		values := make([]string, 0, len(p.lines))
		for _, l := range p.lines {
			values = append(values, l.Value)
		}
		Expect(values).To(Equal([]string{"[Chorus: Rihanna]", "work work"}))
	})
})
