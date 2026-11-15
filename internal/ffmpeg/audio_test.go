package ffmpeg

import (
	"context"
	"os/exec"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
