package book

import (
	"context"
	"log/slog"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/bookcontribution"
	"github.com/datahearth/streamline/internal/observability"
)

const (
	coverKind = "books"
	photoKind = "authors"
)

// artJob is the art one book or series brings: its cover, from the CDN
// address the provider already returned, and the photos of its creators.
type artJob struct {
	bookID  uint32
	cover   string
	authors []*ent.Author
}

// creatorAuthors lists the people of a book's or series' contributions that
// are shown with a photo: authors, writers and artists. A colorist, a cover
// artist, a translator or a narrator has none.
func creatorAuthors(rows []*ent.BookContribution) []*ent.Author {
	var out []*ent.Author
	seen := map[uint32]bool{}
	for _, c := range rows {
		a := c.Edges.Author
		if a == nil || seen[a.ID] || a.ImageSource == "" {
			continue
		}
		switch c.Role {
		case bookcontribution.RoleAuthor,
			bookcontribution.RoleWriter,
			bookcontribution.RoleArtist:
			seen[a.ID] = true
			out = append(out, a)
		}
	}
	return out
}

// jobFor builds the art of a freshly written book.
func jobFor(b *ent.Book, cover string) artJob {
	return artJob{
		bookID:  b.ID,
		cover:   cover,
		authors: creatorAuthors(b.Edges.Contributions),
	}
}

// fetchArtInBackground is best effort and runs after the commit: a missing
// cover or photo is routine and must not fail the add that triggered it. It
// reaches the CDN only; no Hardcover request is spent on a picture.
func (s *Service) fetchArtInBackground(ctx context.Context, jobs ...artJob) {
	if len(jobs) == 0 {
		return
	}
	bg := context.WithoutCancel(ctx)
	go func() {
		defer observability.RecoverPanic(bg, "book.fetch_art", nil)
		for _, j := range jobs {
			if j.cover != "" {
				if err := s.posters.Fetch(
					bg,
					coverKind,
					j.bookID,
					j.cover,
				); err != nil {
					slog.WarnContext(bg, "book cover fetch failed",
						"book.id", j.bookID, "error", err)
				}
			}
			for _, a := range j.authors {
				if err := s.posters.Fetch(
					bg,
					photoKind,
					a.ID,
					a.ImageSource,
				); err != nil {
					slog.WarnContext(bg, "author photo fetch failed",
						"author.id", a.ID, "error", err)
				}
			}
		}
	}()
}
