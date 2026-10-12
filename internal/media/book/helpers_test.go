package book

import (
	"time"

	"github.com/stretchr/testify/mock"

	entbook "github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/bookedition"
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

var anyCtx = mock.Anything

// ids matches a request for exactly these Hardcover ids, in order.
func ids(want ...uint32) any {
	return mock.MatchedBy(func(got []uint32) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	})
}

// seriesRecord is a skeleton of n numbered volumes of series 50, whose
// Hardcover book ids are 1000+position.
func seriesRecord(name string, n int) *metadata.SeriesRecord {
	const base = 1000
	sk := &metadata.SeriesRecord{
		HardcoverID: 50, Name: name, Description: "About " + name,
		PrimaryBooks: numeric.SaturateU32(n), Books: numeric.SaturateU32(n),
		AuthorHardcoverID: 20, AuthorName: "Eiichiro Oda",
		AuthorImageURL: "https://img/oda.jpg",
	}
	for i := 1; i <= n; i++ {
		sk.Volumes = append(sk.Volumes, metadata.SeriesVolumeRef{
			Position: float64(i), BookHardcoverID: base + uint32(i),
		})
	}
	return sk
}

// seriesRec is a batched series read of n volumes, one book per position,
// whose Hardcover book ids are id*100+position.
func seriesRec(id uint32, name string, n int) metadata.SeriesLookupRecord {
	r := metadata.SeriesLookupRecord{
		HardcoverID: id, Name: name, Slug: "slug-" + itoa(id),
		PrimaryBooks: numeric.SaturateU32(n), Books: numeric.SaturateU32(n),
		AuthorName: "Isaac Asimov",
	}
	for i := 1; i <= n; i++ {
		r.Volumes = append(r.Volumes, metadata.SeriesLookupVolume{
			Position:        float64(i),
			BookHardcoverID: id*100 + uint32(i),
			UsersCount:      1,
			ImageURL:        "https://img/s" + itoa(id*100+uint32(i)) + ".jpg",
		})
	}
	return r
}

// volumeRec is a Hardcover record for one volume of a series.
func volumeRec(id uint32) *metadata.BookRecord {
	r := rec(id, "Volume "+itoa(id))
	r.Kind = metadata.BookKindManga
	r.Credits = []metadata.BookCredit{{
		AuthorHardcoverID: 20, Name: "Eiichiro Oda", Role: metadata.RoleWriter,
	}}
	return r
}

// bookEditionOf matches a book's editions of one publisher.
func bookEditionOf(bookID uint32, publisher string) predicate.BookEdition {
	return bookedition.And(
		bookedition.HasBookWith(entbook.ID(bookID)),
		bookedition.Publisher(publisher),
	)
}

// dbSeriesWithStubs is a future-monitored series of two never-hydrated stubs
// whose chosen edition is French, Mnemos.
func dbSeriesWithStubs() db.CreateSeriesParams {
	stub := func(hc uint32, position float64) db.BookSeed {
		return db.BookSeed{
			HardcoverID:       hc,
			Title:             "One Piece #" + itoa(uint32(position)),
			Kind:              "manga",
			PreferredLanguage: "fr",
			SeriesPosition:    &position,
		}
	}
	return db.CreateSeriesParams{
		HardcoverID:      50,
		Title:            "One Piece",
		AuthorName:       "Eiichiro Oda",
		Kind:             "manga",
		Monitor:          "future",
		EditionLanguage:  "fr",
		EditionPublisher: "Mnemos",
		RefreshedAt:      time.Now(),
		Volumes:          []db.BookSeed{stub(1001, 1), stub(1002, 2)},
	}
}
