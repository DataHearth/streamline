package server

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/datahearth/streamline/ent"
	entbook "github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/media/music"
	"github.com/datahearth/streamline/internal/metadata"
	"github.com/datahearth/streamline/internal/request"
)

// artistRequestAdder lets the request service add an artist without knowing
// music.AddParams.
type artistRequestAdder struct{ svc *music.Service }

func (a artistRequestAdder) AddArtist(
	ctx context.Context,
	mbid, _, qualityProfile string,
) error {
	_, err := a.svc.Add(ctx, music.AddParams{
		MBID:           mbid,
		Monitored:      true,
		QualityProfile: qualityProfile,
	})
	if errors.Is(err, music.ErrArtistExists) {
		return fmt.Errorf("%w: %w", request.ErrAlreadyInLibrary, err)
	}
	return err
}

// bookRequestAdder adds requested books through the book service and answers
// the request service's in-library questions from the library tables. Series
// are not modelled in the library yet, so a series request cannot be approved.
type bookRequestAdder struct {
	client *ent.Client
	store  db.Store
	svc    *book.Service
	meta   metadata.BookProvider
}

// AddBook makes exactly the requested slot(s) of one book wanted. An absent
// author is added with monitor policy none so the rest of its bibliography
// stays out of the want list.
func (b bookRequestAdder) AddBook(
	ctx context.Context,
	hardcoverID uint32,
	monitor, qualityProfile string,
) error {
	if b.meta == nil {
		return book.ErrNotConfigured
	}
	details, err := b.meta.GetBook(ctx, hardcoverID)
	if err != nil {
		return fmt.Errorf("get book: %w", err)
	}
	row, err := b.store.FindAuthorByHardcoverID(ctx, details.AuthorHardcover)
	if err != nil {
		return err
	}
	var author *ent.Author
	if row == nil {
		author, err = b.svc.Add(ctx, book.AddParams{
			HardcoverID:             details.AuthorHardcover,
			Monitored:               true,
			MonitorPolicy:           "none",
			EbookQualityProfile:     qualityProfile,
			AudiobookQualityProfile: qualityProfile,
		})
	} else {
		author, err = b.svc.Get(ctx, row.ID)
	}
	if err != nil {
		return err
	}
	i := slices.IndexFunc(author.Edges.Books, func(bk *ent.Book) bool {
		return bk.HardcoverID == hardcoverID
	})
	if i < 0 {
		return fmt.Errorf(
			"book %d not in author %d", hardcoverID, details.AuthorHardcover,
		)
	}
	kinds := []string{monitor}
	if monitor == "both" {
		kinds = []string{"ebook", "audiobook"}
	}
	for _, k := range kinds {
		if err := b.svc.SetBookSlot(
			ctx,
			author.Edges.Books[i].ID,
			k,
			true,
		); err != nil {
			return fmt.Errorf("set %s slot: %w", k, err)
		}
	}
	return nil
}

func (bookRequestAdder) AddSeries(context.Context, uint32, string, string) error {
	return request.ErrUnavailable
}

func (b bookRequestAdder) HasBook(
	ctx context.Context,
	hardcoverID uint32,
) (bool, error) {
	return b.client.Book.Query().
		Where(entbook.HardcoverID(hardcoverID)).Exist(ctx)
}

// HasSeries is false: the library holds no series rows to match against.
func (bookRequestAdder) HasSeries(context.Context, uint32) (bool, error) {
	return false, nil
}
