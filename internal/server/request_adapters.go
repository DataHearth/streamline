package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/datahearth/streamline/ent"
	entbook "github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/internal/media/music"
	"github.com/datahearth/streamline/internal/request"
)

// errBookAddUnavailable is what approving a book or series request answers
// until the books service exposes AddBook and AddSeries.
var errBookAddUnavailable = errors.New(
	"adding a requested book or series is not available yet",
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

// bookRequestAdder answers the request service's in-library questions from
// the library tables.
type bookRequestAdder struct{ client *ent.Client }

func (bookRequestAdder) AddBook(context.Context, uint32, string, string) error {
	return errBookAddUnavailable
}

func (bookRequestAdder) AddSeries(context.Context, uint32, string, string) error {
	return errBookAddUnavailable
}

func (b bookRequestAdder) HasBook(
	ctx context.Context,
	hardcoverID uint32,
) (bool, error) {
	return b.client.Book.Query().
		Where(entbook.HardcoverID(hardcoverID)).Exist(ctx)
}

// HasSeries is false until the library holds series.
func (bookRequestAdder) HasSeries(context.Context, uint32) (bool, error) {
	return false, nil
}
