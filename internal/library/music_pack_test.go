package library

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
)

var _ = Describe("Pack folders", Label("unit", "library"), func() {
	year := func(y int) *time.Time {
		t := time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC)
		return &t
	}
	albums := []*ent.Album{
		{ID: 1, Title: "Nevermind", ReleaseDate: year(1991)},
		{ID: 2, Title: "In Utero", ReleaseDate: year(1993)},
		{ID: 3, Title: "Greatest Hits", ReleaseDate: year(1995)},
		{ID: 4, Title: "Greatest Hits", ReleaseDate: year(2005)},
	}

	DescribeTable(
		"MatchPackDir",
		func(dir string, want uint32) {
			got := MatchPackDir(dir, albums)
			if want == 0 {
				Expect(got).To(BeNil())
				return
			}
			Expect(got).NotTo(BeNil())
			Expect(got.ID).To(Equal(want))
		},
		Entry("plain title", "/dl/Nirvana/Nevermind", uint32(1)),
		Entry(
			"year and format tags",
			"/dl/Nirvana/Nevermind (1991) [FLAC]",
			uint32(1),
		),
		Entry(
			"artist and year prefix",
			"/dl/Nirvana - 1993 - In Utero [FLAC]",
			uint32(2),
		),
		Entry("dots for spaces", "/dl/In.Utero.1993.FLAC", uint32(0)),
		Entry("dots around a title", "/dl/Nirvana.-.In.Utero", uint32(2)),
		Entry("case and punctuation folded", "/dl/NEVERMIND!", uint32(1)),
		Entry(
			"the year picks between two albums of one title",
			"/dl/Greatest Hits (2005)",
			uint32(4),
		),
		Entry(
			"no year, two albums of one title: ambiguous",
			"/dl/Greatest Hits",
			uint32(0),
		),
		Entry("a folder that names no linked album", "/dl/Bleach", uint32(0)),
	)

	DescribeTable(
		"PackAlbumDir keeps disc subfolders with their album",
		func(file, want string) {
			Expect(PackAlbumDir(file)).To(Equal(want))
		},
		Entry("flat", "/dl/Nevermind/01.flac", "/dl/Nevermind"),
		Entry("CD1", "/dl/Greatest Hits/CD1/01.flac", "/dl/Greatest Hits"),
		Entry("Disc 2", "/dl/Greatest Hits/Disc 2/01.flac", "/dl/Greatest Hits"),
		Entry("disk3 in lower case", "/dl/Box/disk3/01.flac", "/dl/Box"),
		Entry(
			"a folder that merely starts with cd",
			"/dl/CDs of 1991/01.flac",
			"/dl/CDs of 1991",
		),
	)
})
