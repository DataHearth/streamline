package metadata

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func raw(
	id uint32,
	lang, publisher string,
	format int,
	popularity uint32,
) RawEdition {
	return RawEdition{
		HardcoverID: id, Language: lang, Publisher: publisher,
		Title: fmt.Sprintf("T%d", id), Popularity: popularity,
		ReadingFormatID: format,
	}
}

func ids(eds []EditionRecord) []uint32 {
	out := make([]uint32, 0, len(eds))
	for _, e := range eds {
		out = append(out, e.HardcoverID)
	}
	return out
}

var _ = Describe("SelectEditions", Label("unit", "metadata"), func() {
	It("keeps the most popular edition per language, publisher and format", func() {
		got, _ := SelectEditions([]RawEdition{
			raw(1, "en", "Tor", 4, 10),
			raw(2, "en", "Tor", 4, 90),
			raw(3, "en", "Orbit", 4, 5),
			raw(4, "en", "Tor", 2, 7),
		}, nil, nil, 2005)

		Expect(ids(got)).To(ConsistOf(uint32(2), uint32(3), uint32(4)))
	})

	It("breaks a popularity tie on the lower id", func() {
		got, _ := SelectEditions([]RawEdition{
			raw(8, "en", "Tor", 4, 5),
			raw(3, "en", "Tor", 4, 5),
		}, nil, nil, 0)

		Expect(ids(got)).To(Equal([]uint32{3}))
	})

	It("reads the format from the reading format id", func() {
		got, _ := SelectEditions([]RawEdition{
			raw(1, "en", "", 4, 1),
			raw(2, "en", "", 2, 1),
		}, nil, nil, 0)

		formats := map[uint32]string{}
		for _, e := range got {
			formats[e.HardcoverID] = e.Format
		}
		Expect(
			formats,
		).To(Equal(map[uint32]string{1: FormatEbook, 2: FormatAudiobook}))
	})

	It(
		"counts a paperback as an ebook only for a language with no ebook of its own",
		func() {
			got, _ := SelectEditions(
				[]RawEdition{raw(1, "en", "Tor", 4, 1)},
				[]RawEdition{
					raw(2, "en", "Tor", 1, 99),
					raw(3, "fr", "Glenat", 1, 5),
					raw(4, "ja", "", 0, 2),
				},
				nil, 0,
			)

			// English already has an ebook; French and Japanese only have paper.
			Expect(ids(got)).To(ConsistOf(uint32(1), uint32(3), uint32(4)))
			for _, e := range got {
				Expect(e.Format).To(Equal(FormatEbook))
			}
		},
	)

	It("drops an edition with no language", func() {
		got, _ := SelectEditions([]RawEdition{raw(1, "", "Tor", 4, 1)}, nil, nil, 0)
		Expect(got).To(BeEmpty())
	})

	It("counts an edition listed in two pools once", func() {
		e := raw(1, "en", "Tor", 4, 1)
		got, _ := SelectEditions([]RawEdition{e}, []RawEdition{e}, &e, 0)
		Expect(got).To(HaveLen(1))
	})

	It(
		"marks the most popular winner in the earliest edition's language as original",
		func() {
			earliest := raw(9, "fr", "Glenat", 1, 1)
			got, lang := SelectEditions([]RawEdition{
				raw(1, "en", "Tor", 4, 90),
				raw(2, "fr", "Glenat", 4, 10),
				raw(3, "fr", "Kana", 4, 30),
				raw(4, "fr", "Audible", 2, 3),
			}, nil, &earliest, 0)

			Expect(lang).To(Equal("fr"))
			original := map[uint32]bool{}
			for _, e := range got {
				original[e.HardcoverID] = e.Original
			}
			// One original per format: the most popular French ebook and audiobook.
			Expect(original[3]).To(BeTrue())
			Expect(original[4]).To(BeTrue())
			Expect(original[1]).To(BeFalse())
			Expect(original[2]).To(BeFalse())
		},
	)

	It("marks nothing original when the earliest edition is unknown", func() {
		got, lang := SelectEditions(
			[]RawEdition{raw(1, "en", "Tor", 4, 1)},
			nil,
			nil,
			0,
		)
		Expect(lang).To(BeEmpty())
		Expect(got[0].Original).To(BeFalse())
	})

	It("fills a missing year from the release date, then the book's year", func() {
		dated := raw(1, "en", "A", 4, 1)
		d := time.Date(2010, 6, 1, 0, 0, 0, 0, time.UTC)
		dated.ReleaseDate = &d
		undated := raw(2, "fr", "B", 4, 1)
		withYear := raw(3, "de", "C", 4, 1)
		withYear.Year = 1999

		got, _ := SelectEditions(
			[]RawEdition{dated, undated, withYear},
			nil,
			nil,
			2005,
		)

		years := map[uint32]uint16{}
		for _, e := range got {
			years[e.HardcoverID] = e.Year
		}
		Expect(years).To(Equal(map[uint32]uint16{1: 2010, 2: 2005, 3: 1999}))
	})

	It(
		"caps at forty by popularity but keeps the best of every language and format",
		func() {
			digital := make([]RawEdition, 0, 63)
			for i := range 60 {
				digital = append(
					digital,
					raw(uint32(i+1), "en", fmt.Sprintf("P%d", i), 4, uint32(1000-i)),
				)
			}
			digital = append(digital,
				raw(500, "fr", "Glenat", 4, 1),
				raw(501, "fr", "Kana", 4, 0),
				raw(502, "ja", "Shueisha", 2, 0),
			)

			got, _ := SelectEditions(digital, nil, nil, 0)

			selected := ids(got)
			Expect(len(selected)).To(BeNumerically(">=", 40))
			Expect(len(selected)).To(BeNumerically("<=", 43))
			// The unpopular French and Japanese winners survive the cap.
			Expect(selected).To(ContainElements(uint32(500), uint32(502)))
			// The most popular English one does too; the 60th does not.
			Expect(selected).To(ContainElement(uint32(1)))
			Expect(selected).NotTo(ContainElement(uint32(60)))
		},
	)
})
