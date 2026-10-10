package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/datahearth/streamline/ent"
	entbook "github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/bookseries"
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/media/music"
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
		Monitor:        "all",
		QualityProfile: qualityProfile,
	})
	if errors.Is(err, music.ErrArtistExists) {
		return fmt.Errorf("%w: %w", request.ErrAlreadyInLibrary, err)
	}
	return err
}

// bookRequestAdder adds a requested book or series through the books service
// and answers the request service's in-library questions from the library
// tables.
type bookRequestAdder struct {
	svc    book.Manager
	client *ent.Client
}

func (b bookRequestAdder) AddBook(
	ctx context.Context,
	hardcoverID uint32,
	monitor, qualityProfile string,
) error {
	_, err := b.svc.AddBook(ctx, book.AddBookParams{
		HardcoverID:    hardcoverID,
		Monitor:        monitor,
		QualityProfile: qualityProfile,
	})
	if errors.Is(err, book.ErrBookExists) {
		return fmt.Errorf("%w: %w", request.ErrAlreadyInLibrary, err)
	}
	return err
}

func (b bookRequestAdder) AddSeries(
	ctx context.Context,
	hardcoverID uint32,
	monitor, qualityProfile string,
) error {
	_, err := b.svc.AddSeries(ctx, book.AddSeriesParams{
		HardcoverID:    hardcoverID,
		Monitor:        monitor,
		QualityProfile: qualityProfile,
	})
	if errors.Is(err, book.ErrSeriesExists) {
		return fmt.Errorf("%w: %w", request.ErrAlreadyInLibrary, err)
	}
	return err
}

func (b bookRequestAdder) HasBook(
	ctx context.Context,
	hardcoverID uint32,
) (bool, error) {
	return b.client.Book.Query().
		Where(entbook.HardcoverID(hardcoverID)).Exist(ctx)
}

func (b bookRequestAdder) HasSeries(
	ctx context.Context,
	hardcoverID uint32,
) (bool, error) {
	return b.client.BookSeries.Query().
		Where(bookseries.HardcoverID(hardcoverID)).Exist(ctx)
}
