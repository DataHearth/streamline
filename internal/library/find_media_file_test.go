package library

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func writeSizedFile(dir, name string, size int64) string {
	GinkgoHelper()
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(f.Close)
	Expect(f.Truncate(size)).To(Succeed())
	return p
}

var _ = Describe("FindMediaFile", Label("unit", "library"), func() {
	It("skips files below 50MB", func() {
		dir := GinkgoT().TempDir()
		writeSizedFile(dir, "small.mkv", 1024)
		_, err := FindMediaFile(dir)
		Expect(err).To(MatchError(ErrNoMedia))
	})

	It(`skips files whose basename matches \bsample\b (case-insensitive)`, func() {
		dir := GinkgoT().TempDir()
		writeSizedFile(dir, "sample.mkv", 60<<20)
		writeSizedFile(dir, "movie.Sample.mkv", 60<<20)
		_, err := FindMediaFile(dir)
		Expect(err).To(MatchError(ErrSampleOnly))
	})

	It("returns ErrMultipleMedia when >1 non-sample candidate remains", func() {
		dir := GinkgoT().TempDir()
		writeSizedFile(dir, "a.mkv", 60<<20)
		writeSizedFile(dir, "b.mkv", 60<<20)
		_, err := FindMediaFile(dir)
		Expect(err).To(MatchError(ErrMultipleMedia))
	})

	It("returns the single candidate path when one remains", func() {
		dir := GinkgoT().TempDir()
		writeSizedFile(dir, "sample.mkv", 60<<20)
		writeSizedFile(dir, "movie.mkv", 60<<20)
		writeSizedFile(dir, "subs.srt", 60<<20)
		p, err := FindMediaFile(dir)
		Expect(err).NotTo(HaveOccurred())
		Expect(p).To(Equal(filepath.Join(dir, "movie.mkv")))
	})

	It("returns ErrNoMedia on empty dir", func() {
		_, err := FindMediaFile(GinkgoT().TempDir())
		Expect(err).To(MatchError(ErrNoMedia))
	})

	It("accepts a single file path that passes filters", func() {
		dir := GinkgoT().TempDir()
		p := writeSizedFile(dir, "Movie.2024.1080p.mkv", 60<<20)
		got, err := FindMediaFile(p)
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(p))
	})

	It("rejects a single-file path below the size threshold", func() {
		dir := GinkgoT().TempDir()
		p := writeSizedFile(dir, "tiny.mkv", 1024)
		_, err := FindMediaFile(p)
		Expect(err).To(MatchError(ErrNoMedia))
	})
})
