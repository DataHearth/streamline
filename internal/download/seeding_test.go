package download

import (
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SeedingSet", Label("unit", "download"), func() {
	set := NewSeedingSet(
		true,
		[]string{"/dl/Album", "/dl/single.flac"},
		[]string{"ABC"},
	)

	It("owns what lies inside a torrent's content, and nothing beside it", func() {
		Expect(set.Owns("/dl/Album/01.flac")).To(BeTrue())
		Expect(set.Owns("/dl/single.flac")).To(BeTrue())
		Expect(set.Owns("/dl/Album Deluxe/01.flac")).To(BeFalse())
		Expect(set.Owns("/music/Album/01.flac")).To(BeFalse())
	})

	It("matches hashes whatever their case", func() {
		Expect(set.Live("abc")).To(BeTrue())
		Expect(set.Live("def")).To(BeFalse())
	})

	It(
		"ties a hard link to the listed torrent whose content holds its bytes",
		func() {
			dl, lib := GinkgoT().TempDir(), GinkgoT().TempDir()
			seeded := filepath.Join(dl, "Album", "01.flac")
			Expect(os.MkdirAll(filepath.Dir(seeded), 0o755)).To(Succeed())
			Expect(os.WriteFile(seeded, []byte("audio"), 0o644)).To(Succeed())
			linked := filepath.Join(lib, "01.flac")
			Expect(os.Link(seeded, linked)).To(Succeed())
			copied := filepath.Join(lib, "02.flac")
			Expect(os.WriteFile(copied, []byte("audio"), 0o644)).To(Succeed())

			set := NewSeedingSet(true, []string{filepath.Join(dl, "Album")}, nil)
			Expect(set.SharesData(linked)).To(BeTrue())
			Expect(
				set.SharesData(copied),
			).To(BeFalse(), "same size and bytes, other file")

			gone := NewSeedingSet(true, []string{filepath.Join(dl, "Other")}, nil)
			Expect(gone.SharesData(linked)).To(BeFalse())
		},
	)
})
