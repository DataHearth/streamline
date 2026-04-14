package db

import (
	"context"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/request"
	"github.com/datahearth/streamline/ent/user"
)

type CreateRequestParams struct {
	MediaType   string // "movie" | "tvshow"
	MediaID     uint32
	Title       string
	RequesterID uint32
}

type ListRequestsParams struct {
	Status      string // "" = all
	MediaType   string // "" = all
	RequesterID uint32 // 0 = all requesters (admin view)
	Offset      uint32
	Limit       uint32
}

func (db *DB) CreateRequest(
	ctx context.Context,
	p CreateRequestParams,
) (*ent.Request, error) {
	return db.client.Request.Create().
		SetMediaType(request.MediaType(p.MediaType)).
		SetMediaID(p.MediaID).
		SetTitle(p.Title).
		SetRequesterID(p.RequesterID).
		Save(ctx)
}

// FindActiveRequest returns an existing pending/approved/available request for
// the given media, or nil when none exists (used for dedup).
func (db *DB) FindActiveRequest(
	ctx context.Context,
	mediaType string,
	mediaID uint32,
) (*ent.Request, error) {
	row, err := db.client.Request.Query().
		Where(
			request.MediaTypeEQ(request.MediaType(mediaType)),
			request.MediaIDEQ(mediaID),
			request.StatusIn(
				request.StatusPending,
				request.StatusApproved,
				request.StatusAvailable,
			),
		).First(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return row, err
}

func (db *DB) ListRequests(
	ctx context.Context,
	p ListRequestsParams,
) ([]*ent.Request, int, error) {
	q := db.client.Request.Query().WithRequester().WithApprovedBy()
	if p.Status != "" {
		q = q.Where(request.StatusEQ(request.Status(p.Status)))
	}
	if p.MediaType != "" {
		q = q.Where(request.MediaTypeEQ(request.MediaType(p.MediaType)))
	}
	if p.RequesterID != 0 {
		q = q.Where(request.HasRequesterWith(user.IDEQ(p.RequesterID)))
	}
	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	limit := int(p.Limit)
	if limit <= 0 {
		limit = 50
	}
	rows, err := q.Order(ent.Desc(request.FieldCreateTime)).
		Offset(int(p.Offset)).Limit(limit).All(ctx)
	return rows, total, err
}

func (db *DB) GetRequest(ctx context.Context, id uint32) (*ent.Request, error) {
	return db.client.Request.Query().Where(request.IDEQ(id)).
		WithRequester().WithApprovedBy().Only(ctx)
}

func (db *DB) ApproveRequest(ctx context.Context, id, adminID uint32) error {
	return db.client.Request.UpdateOneID(id).
		SetStatus(request.StatusApproved).SetApprovedByID(adminID).Exec(ctx)
}

func (db *DB) DenyRequest(
	ctx context.Context,
	id, adminID uint32,
	reason string,
) error {
	return db.client.Request.UpdateOneID(id).
		SetStatus(request.StatusDenied).
		SetApprovedByID(adminID).
		SetReason(reason).
		Exec(ctx)
}

func (db *DB) ReopenRequest(ctx context.Context, id uint32) error {
	return db.client.Request.UpdateOneID(id).
		SetStatus(request.StatusPending).
		ClearApprovedBy().
		SetReason("").
		Exec(ctx)
}

// MarkRequestsAvailable flips every approved request for the given media to
// available. Called best-effort from the importer success paths.
func (db *DB) MarkRequestsAvailable(
	ctx context.Context,
	mediaType string,
	mediaID uint32,
) error {
	_, err := db.client.Request.Update().
		Where(
			request.MediaTypeEQ(request.MediaType(mediaType)),
			request.MediaIDEQ(mediaID),
			request.StatusEQ(request.StatusApproved),
		).SetStatus(request.StatusAvailable).Save(ctx)
	return err
}

func (db *DB) CountRequestsByStatus(
	ctx context.Context,
	status request.Status,
) (int, error) {
	return db.client.Request.Query().
		Where(request.StatusEQ(status)).Count(ctx)
}
