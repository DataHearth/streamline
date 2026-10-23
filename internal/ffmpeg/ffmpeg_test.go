package ffmpeg

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CLI", Label("unit", "ffmpeg"), func() {
	writeFakeFFprobe := func(dir, script string) {
		GinkgoHelper()
		p := filepath.Join(dir, "ffprobe")
		Expect(os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755)).To(Succeed())
	}

	It("is unavailable when the binary is missing", func() {
		c := NewCLI(GinkgoT().TempDir())
		Expect(c.Available()).To(BeFalse())
		_, err := c.Probe(context.Background(), "/nope.mkv")
		Expect(err).To(HaveOccurred())
	})

	It("probes through a resolved binary", func() {
		dir := GinkgoT().TempDir()
		writeFakeFFprobe(dir, `cat <<'EOF'
{"streams":[{"codec_type":"video","codec_name":"h264","width":1920,"height":1080}],"format":{"format_name":"matroska","duration":"5400.0","bit_rate":"8000000"}}
EOF`)
		c := NewCLI(dir)
		Expect(c.Available()).To(BeTrue())
		// ResolvedPath must be exec.LookPath's own return value, not the
		// dir/ffprobe path NewCLI joined to build the lookup — on unix these
		// happen to match once the file exists, but the code must not rely
		// on that (see NewCLI: it stores LookPath's result, not the join).
		Expect(c.ResolvedPath()).To(Equal(filepath.Join(dir, "ffprobe")))
		info, err := c.Probe(context.Background(), "/any.mkv")
		Expect(err).NotTo(HaveOccurred())
		Expect(info.VideoCodec).To(Equal("h264"))
	})

	It("maps a non-zero exit to ErrUnreadable", func() {
		dir := GinkgoT().TempDir()
		writeFakeFFprobe(dir, "exit 1")
		c := NewCLI(dir)
		_, err := c.Probe(context.Background(), "/any.mkv")
		Expect(err).To(MatchError(ErrUnreadable))
	})

	writeFakeFFmpeg := func(dir, script string) {
		GinkgoHelper()
		p := filepath.Join(dir, "ffmpeg")
		Expect(os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o755)).To(Succeed())
	}

	It("resolves ffmpeg alongside ffprobe and reports its version", func() {
		dir := GinkgoT().TempDir()
		writeFakeFFprobe(dir, "exit 0")
		writeFakeFFmpeg(
			dir,
			`echo "ffmpeg version 7.1.1 Copyright (c) 2000-2025 the FFmpeg developers"`,
		)
		c := NewCLI(dir)
		Expect(c.FFmpegPath()).NotTo(BeEmpty())
		version, err := c.Version(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(version).To(Equal("7.1.1"))
	})

	It(
		"reports no ffmpeg path and errors on Version when the binary is missing",
		func() {
			dir := GinkgoT().TempDir()
			writeFakeFFprobe(dir, "exit 0")
			c := NewCLI(dir)
			Expect(c.FFmpegPath()).To(BeEmpty())
			_, err := c.Version(context.Background())
			Expect(err).To(HaveOccurred())
		},
	)
})
