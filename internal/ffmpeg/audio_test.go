package ffmpeg

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("AudioTranscodeArgs", Label("unit", "ffmpeg"), func() {
	It("seeks before the input and encodes the first audio stream", func() {
		args, err := AudioTranscodeArgs("/m/a.flac", AudioTranscode{
			Format: "opus", BitrateKbps: 96, OffsetSec: 30,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(args).To(Equal([]string{
			"-hide_banner", "-nostdin", "-loglevel", "error",
			"-ss", "30",
			"-i", "/m/a.flac",
			"-map", "0:a:0",
			"-vn", "-sn", "-dn",
			"-map_metadata", "-1",
			"-c:a", "libopus",
			"-b:a", "96k",
			"-f", "ogg",
			"pipe:1",
		}))
	})

	It("omits the seek at offset zero", func() {
		args, err := AudioTranscodeArgs(
			"/m/a.flac",
			AudioTranscode{Format: "mp3", BitrateKbps: 192},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(args).NotTo(ContainElement("-ss"))
		Expect(args).To(ContainElements("libmp3lame", "192k", "mp3"))
	})

	It("refuses a format it cannot produce", func() {
		_, err := AudioTranscodeArgs("/m/a.flac", AudioTranscode{Format: "flac"})
		Expect(err).To(MatchError(ErrUnknownAudioFormat))
	})

	It("reports a missing ffmpeg", func() {
		c := &CLI{}
		Expect(c.TranscodeAudio(context.Background(), "/m/a.flac",
			AudioTranscode{Format: "mp3", BitrateKbps: 128}, &bytes.Buffer{})).
			To(MatchError(ErrNoFFmpeg))
	})
})

var _ = Describe(
	"TranscodeAudio against the real ffmpeg",
	Label("e2e", "ffmpeg"),
	func() {
		var (
			cli *CLI
			src string
		)

		BeforeEach(func() {
			if _, err := exec.LookPath("ffmpeg"); err != nil {
				Skip("real ffmpeg is not on PATH")
			}
			cli = NewCLI("")
			src = filepath.Join(GinkgoT().TempDir(), "tone.flac")
			//nolint:gosec // the test drives the ffmpeg it just resolved
			out, err := exec.Command(cli.FFmpegPath(), "-hide_banner", "-loglevel", "error",
				"-f", "lavfi", "-i", "sine=frequency=440:duration=3", src).
				CombinedOutput()
			Expect(err).NotTo(HaveOccurred(), string(out))
		})

		It("streams an MP3", func() {
			var buf bytes.Buffer
			Expect(cli.TranscodeAudio(
				context.Background(),
				src,
				AudioTranscode{
					Format:      "mp3",
					BitrateKbps: 128,
					OffsetSec:   1,
				},
				&buf,
			)).To(Succeed())
			Expect(buf.Len()).To(BeNumerically(">", 1000))
			head := buf.Bytes()[:3]
			Expect(
				string(head) == "ID3" || (head[0] == 0xFF && head[1]&0xE0 == 0xE0),
			).To(BeTrue())
		})

		It("fails with ffmpeg's own message on an unreadable source", func() {
			err := cli.TranscodeAudio(
				context.Background(),
				filepath.Join(GinkgoT().TempDir(), "nope.flac"),
				AudioTranscode{Format: "mp3", BitrateKbps: 128},
				&bytes.Buffer{},
			)
			Expect(err).To(MatchError(ContainSubstring("ffmpeg transcode")))
		})

		It("stops quietly when the listener goes away", func() {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err := cli.TranscodeAudio(
				ctx,
				src,
				AudioTranscode{Format: "mp3", BitrateKbps: 128},
				&bytes.Buffer{},
			)
			Expect(err).To(MatchError(context.Canceled))
		})
	},
)

var _ = Describe("ConvertToFLAC", Label("unit", "ffmpeg"), func() {
	It("encodes the first audio stream as FLAC and keeps the tags", func() {
		Expect(FLACConvertArgs("/d/a.wav", "/m/a.flac")).To(Equal([]string{
			"-hide_banner", "-nostdin", "-loglevel", "error", "-y",
			"-i", "/d/a.wav",
			"-map", "0:a:0",
			"-vn", "-sn", "-dn",
			"-c:a", "flac",
			"-f", "flac",
			"/m/a.flac",
		}))
	})

	It("fails without an ffmpeg binary", func() {
		c := &CLI{}
		Expect(c.ConvertToFLAC(context.Background(), "a.wav", "a.flac")).
			To(MatchError(ErrNoFFmpeg))
	})

	Context("with real ffmpeg", func() {
		var (
			c   *CLI
			dir string
		)
		BeforeEach(func() {
			if _, err := exec.LookPath("ffmpeg"); err != nil {
				Skip("real ffmpeg is not on PATH")
			}
			c = NewCLI("")
			dir = GinkgoT().TempDir()
		})

		wav := func(codec string) string {
			GinkgoHelper()
			src := filepath.Join(dir, codec+".wav")
			//nolint:gosec // the test drives ffmpeg with its own fixed codecs
			Expect(exec.Command("ffmpeg", "-loglevel", "error", "-y",
				"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
				"-c:a", codec, src).Run()).To(Succeed())
			return src
		}

		DescribeTable("writes a FLAC of the source's rate",
			func(codec string, depth uint8) {
				dst := filepath.Join(dir, "out.flac")
				Expect(c.ConvertToFLAC(context.Background(), wav(codec), dst)).
					To(Succeed())
				info, err := c.ProbeAudio(context.Background(), dst)
				Expect(err).NotTo(HaveOccurred())
				Expect(info.Codec).To(Equal("flac"))
				Expect(info.SampleRateHz).To(Equal(uint32(44100)))
				Expect(info.BitDepth).To(Equal(depth))
			},
			Entry("16-bit integer", "pcm_s16le", uint8(16)),
			Entry("24-bit integer", "pcm_s24le", uint8(24)),
			Entry("float, rounded to 24-bit", "pcm_f32le", uint8(24)),
		)

		It("leaves nothing behind when the source cannot be read", func() {
			dst := filepath.Join(dir, "out.flac")
			Expect(c.ConvertToFLAC(
				context.Background(), filepath.Join(dir, "missing.wav"), dst,
			)).NotTo(Succeed())
			Expect(dst).NotTo(BeAnExistingFile())
		})
	})
})
