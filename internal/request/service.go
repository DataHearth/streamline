// Package request is the media-request subsystem: users request movies/shows,
// admins approve (creating the monitored library item) or deny. It serves both
// verticals and is the purpose of the request_only role.
package request

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/events"
	"github.com/datahearth/streamline/internal/otelx"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var tracer = otel.Tracer("github.com/datahearth/streamline/internal/request")

var (
	// ErrDuplicate is returned when the media is already requested (active) or
	// already in the library.
	ErrDuplicate = errors.New("request: already requested or in library")
	// ErrRequestNotFound is returned by Approve/Deny/Reopen for an unknown id
	// so callers can answer 404 instead of 500.
	ErrRequestNotFound = errors.New("request not found")
)

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
	qualityProfile string,
) (*ent.Request, error) {
	ctx, span := tracer.Start(ctx, "request.create",
		trace.WithAttributes(
			attribute.String("media.type", mediaType),
			attribute.Int("media.id", int(mediaID)),
		))
	defer span.End()

	existing, err := s.db.FindActiveRequest(ctx, mediaType, mediaID)
	if err != nil {
		return nil, otelx.RecordSpanError(span, err)
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
	row, err := s.db.CreateRequest(ctx, db.CreateRequestParams{
		MediaType:      mediaType,
		MediaID:        mediaID,
		Title:          title,
		RequesterID:    requesterID,
		QualityProfile: qualityProfile,
	})
	if err != nil {
		// The partial unique index over active (media_type, media_id) is the
		// real dedup gate — the lookups above only save a round-trip and lose
		// to a concurrent request that inserts between them and this write.
		if ent.IsConstraintError(err) {
			return nil, ErrDuplicate
		}
		return nil, otelx.RecordSpanError(span, err)
	}
	// The request system is the only bridge between a user's ask and a library
	// change, and none of its four transitions logged anything — so "who
	// requested this, who approved it, and why was that one denied" was
	// unanswerable from the log stream.
	slog.InfoContext(ctx, "media requested",
		"request.id", row.ID,
		"media.type", mediaType,
		"media.id", mediaID,
		"user.id", requesterID)
	return row, nil
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
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("request %d: %w", id, ErrRequestNotFound)
		}
		return nil, otelx.RecordSpanError(span, err)
	}
	// The library row Add creates carries its own `added` event via the ent
	// hook. This one is the half the hook cannot see: that the addition came
	// from a request, and which one.
	var (
		scope   events.Scope
		ownerID uint32
	)
	switch req.MediaType {
	case "movie":
		m, _, err := s.movies.Add(ctx, req.MediaID, qualityProfile)
		if err != nil {
			return nil, otelx.RecordSpanError(
				span, fmt.Errorf("approve: add movie: %w", err),
			)
		}
		scope, ownerID = events.ScopeMovie, m.ID
	case "tvshow":
		show, err := s.shows.Add(ctx, req.MediaID, qualityProfile)
		if err != nil {
			return nil, otelx.RecordSpanError(
				span, fmt.Errorf("approve: add show: %w", err),
			)
		}
		scope, ownerID = events.ScopeSeries, show.ID
	}
	if err := s.db.ApproveRequest(ctx, id, adminID); err != nil {
		return nil, otelx.RecordSpanError(span, err)
	}
	if ownerID != 0 {
		if err := events.Record(
			ctx, nil, events.TypeRequestApproved, scope, ownerID,
			map[string]any{
				"request_id":  id,
				"approved_by": adminID,
			},
		); err != nil {
			slog.WarnContext(ctx, "record request approval event failed",
				"request.id", id, "error", err)
		}
	}
	slog.InfoContext(ctx, "request approved",
		"request.id", id, "media.type", req.MediaType, "user.id", adminID)
	return s.db.GetRequest(ctx, id)
}

// Deny and Reopen are the negative half of the same workflow as Approve, and
// were the untraced half — a slow or failing denial showed up in no span at
// all while its sibling was fully covered.
func (s *Service) Deny(
	ctx context.Context,
	id, adminID uint32,
	reason string,
) (*ent.Request, error) {
	ctx, span := tracer.Start(ctx, "request.deny",
		trace.WithAttributes(attribute.Int("request.id", int(id))))
	defer span.End()

	if err := s.db.DenyRequest(ctx, id, adminID, reason); err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("request %d: %w", id, ErrRequestNotFound)
		}
		return nil, otelx.RecordSpanError(span, err)
	}
	slog.InfoContext(ctx, "request denied",
		"request.id", id, "user.id", adminID, "reason", reason)
	return s.db.GetRequest(ctx, id)
}

func (s *Service) Reopen(ctx context.Context, id uint32) (*ent.Request, error) {
	ctx, span := tracer.Start(ctx, "request.reopen",
		trace.WithAttributes(attribute.Int("request.id", int(id))))
	defer span.End()

	if err := s.db.ReopenRequest(ctx, id); err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("request %d: %w", id, ErrRequestNotFound)
		}
		return nil, otelx.RecordSpanError(span, err)
	}
	slog.InfoContext(ctx, "request reopened", "request.id", id)
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
		qualityProfile string,
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
