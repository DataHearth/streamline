// Package request is the media-request subsystem: users request movies/shows,
// admins approve (creating the monitored library item) or deny. It serves both
// verticals and is the purpose of the request_only role.
package request

import (
	"context"
	"errors"
	"fmt"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("github.com/datahearth/streamline/internal/request")

// ErrDuplicate is returned when the media is already requested (active) or
// already in the library.
var ErrDuplicate = errors.New("request: already requested or in library")

// MovieAdder / ShowAdder are the slices of the media services this needs —
// declared at the consumer so the request service depends only on what it uses.
type MovieAdder interface {
	Add(
		ctx context.Context,
		tmdbID uint32,
		qualityProfile string,
	) (*ent.Movie, string, error)
	GetByTMDBID(ctx context.Context, tmdbID uint32) (*ent.Movie, error)
}

type ShowAdder interface {
	Add(
		ctx context.Context,
		tvdbID uint32,
		qualityProfile string,
	) (*ent.TVShow, error)
}

type Service struct {
	db     db.Store
	movies MovieAdder
	shows  ShowAdder
}

func NewService(store db.Store, movies MovieAdder, shows ShowAdder) *Service {
	return &Service{db: store, movies: movies, shows: shows}
}

func (s *Service) Create(
	ctx context.Context,
	mediaType string,
	mediaID uint32,
	title string,
	requesterID uint32,
) (*ent.Request, error) {
	ctx, span := tracer.Start(ctx, "request.create",
		trace.WithAttributes(
			attribute.String("media.type", mediaType),
			attribute.Int("media.id", int(mediaID)),
		))
	defer span.End()

	existing, err := s.db.FindActiveRequest(ctx, mediaType, mediaID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, ErrDuplicate
	}
	if mediaType == "movie" {
		if m, err := s.movies.GetByTMDBID(ctx, mediaID); err == nil && m != nil {
			return nil, ErrDuplicate
		}
	} else {
		if sh, err := s.db.FindTVShowByTVDBID(ctx, mediaID); err == nil &&
			sh != nil {
			return nil, ErrDuplicate
		}
	}
	return s.db.CreateRequest(ctx, db.CreateRequestParams{
		MediaType:   mediaType,
		MediaID:     mediaID,
		Title:       title,
		RequesterID: requesterID,
	})
}

// Approve adds the requested item to the library with qualityProfile (empty
// resolves to the server default) and marks the request approved.
func (s *Service) Approve(
	ctx context.Context,
	id, adminID uint32,
	qualityProfile string,
) (*ent.Request, error) {
	ctx, span := tracer.Start(ctx, "request.approve",
		trace.WithAttributes(attribute.Int("request.id", int(id))))
	defer span.End()

	req, err := s.db.GetRequest(ctx, id)
	if err != nil {
		return nil, err
	}
	switch req.MediaType {
	case "movie":
		if _, _, err := s.movies.Add(ctx, req.MediaID, qualityProfile); err != nil {
			return nil, fmt.Errorf("approve: add movie: %w", err)
		}
	case "tvshow":
		if _, err := s.shows.Add(ctx, req.MediaID, qualityProfile); err != nil {
			return nil, fmt.Errorf("approve: add show: %w", err)
		}
	}
	if err := s.db.ApproveRequest(ctx, id, adminID); err != nil {
		return nil, err
	}
	return s.db.GetRequest(ctx, id)
}

func (s *Service) Deny(
	ctx context.Context,
	id, adminID uint32,
	reason string,
) (*ent.Request, error) {
	if err := s.db.DenyRequest(ctx, id, adminID, reason); err != nil {
		return nil, err
	}
	return s.db.GetRequest(ctx, id)
}

func (s *Service) Reopen(ctx context.Context, id uint32) (*ent.Request, error) {
	if err := s.db.ReopenRequest(ctx, id); err != nil {
		return nil, err
	}
	return s.db.GetRequest(ctx, id)
}

func (s *Service) List(
	ctx context.Context,
	p db.ListRequestsParams,
) ([]*ent.Request, int, error) {
	return s.db.ListRequests(ctx, p)
}

func (s *Service) Get(ctx context.Context, id uint32) (*ent.Request, error) {
	return s.db.GetRequest(ctx, id)
}

// Manager is the request service surface consumed by REST handlers.
type Manager interface {
	Create(
		ctx context.Context,
		mediaType string,
		mediaID uint32,
		title string,
		requesterID uint32,
	) (*ent.Request, error)
	Approve(
		ctx context.Context,
		id, adminID uint32,
		qualityProfile string,
	) (*ent.Request, error)
	Get(ctx context.Context, id uint32) (*ent.Request, error)
	Deny(
		ctx context.Context,
		id, adminID uint32,
		reason string,
	) (*ent.Request, error)
	Reopen(ctx context.Context, id uint32) (*ent.Request, error)
	List(
		ctx context.Context,
		p db.ListRequestsParams,
	) ([]*ent.Request, int, error)
}

var _ Manager = (*Service)(nil)
