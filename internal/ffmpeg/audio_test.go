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
