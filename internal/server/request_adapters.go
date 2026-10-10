package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/album"
	entbook "github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/bookseries"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/media/book"
	"github.com/datahearth/streamline/internal/media/music"
	"github.com/datahearth/streamline/internal/request"
)

// artistRequestAdder lets the request service add an artist without knowing
// music.AddParams.
type artistRequestAdder struct{ svc *music.Service }

func (a artistRequestAdder) AddArtist(
	ctx context.Context,
	mbid, monitor, qualityProfile string,
) error {
	_, err := a.svc.Add(ctx, music.AddParams{
		MBID:           mbid,
		Monitor:        monitor,
		QualityProfile: qualityProfile,
	})
	if errors.Is(err, music.ErrArtistExists) {
		return fmt.Errorf("%w: %w", request.ErrAlreadyInLibrary, err)
	}
	return err
}

// albumRequestMonitor monitors one album of a library artist for the request
// service, looking for the release group once more after a refresh when the
// artist's stored list lacks it.
type albumRequestMonitor struct {
	svc   *music.Service
	store db.Store
}

func (a albumRequestMonitor) MonitorAlbum(
	ctx context.Context,
	artistMBID, albumMBID string,
) (bool, error) {
	row, err := a.store.FindArtistByMBID(ctx, artistMBID)
	if err != nil {
		return false, err
	}
	if row == nil {
		return false, fmt.Errorf(
			"%w: artist %s",
			request.ErrAlbumNotFound,
			artistMBID,
		)
	}
	artist, err := a.svc.Get(ctx, row.ID)
	if err != nil {
		return false, err
	}
	target := findAlbum(artist, albumMBID)
	if target == nil {
		artist, err = a.svc.RefreshOne(ctx, row.ID)
		if err != nil {
			return false, err
		}
		if target = findAlbum(artist, albumMBID); target == nil {
			return false, fmt.Errorf("%w: %s", request.ErrAlbumNotFound, albumMBID)
		}
	}
	if target.Monitored && target.Status == album.StatusAvailable {
		return true, nil
	}
	return false, a.svc.SetAlbumMonitored(ctx, target.ID, true)
}

func findAlbum(a *ent.Artist, mbid string) *ent.Album {
	for _, al := range a.Edges.Albums {
		if al.Mbid == mbid {
			return al
		}
	}
	return nil
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
