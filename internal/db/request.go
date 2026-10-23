package db

import (
	"context"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/request"
	"github.com/datahearth/streamline/ent/user"
)

type CreateRequestParams struct {
	MediaType      string
	MediaID        uint32
	MediaMBID      string
	Title          string
	RequesterID    uint32
	QualityProfile string // "" = no preference
	// ArtistMBID, ArtistName and RequestedAs are the album-request hints; the
	// requester's wording is kept in RequestedAs once approval replaces the
	// pair with the verified artist.
	ArtistMBID  string
	ArtistName  string
	RequestedAs string
}

type ListRequestsParams struct {
	Status      string   // "" = all
	MediaTypes  []string // empty = all
	RequesterID uint32   // 0 = all requesters (admin view)
	Offset      uint32
	Limit       uint32
}

func (db *DB) CreateRequest(
	ctx context.Context,
	p CreateRequestParams,
) (*ent.Request, error) {
	c := db.client.Request.Create().
		SetMediaType(request.MediaType(p.MediaType)).
		SetMediaID(p.MediaID).
		SetTitle(p.Title).
		SetRequesterID(p.RequesterID).
		SetQualityProfile(p.QualityProfile)
	if p.MediaMBID != "" {
		c = c.SetMediaMbid(p.MediaMBID)
	}
	if p.ArtistMBID != "" {
		c = c.SetArtistMbid(p.ArtistMBID)
	}
	if p.ArtistName != "" {
		c = c.SetArtistName(p.ArtistName)
	}
	if p.RequestedAs != "" {
		c = c.SetRequestedAs(p.RequestedAs)
	}
	row, err := c.Save(ctx)
	if err != nil {
		return nil, err
	}
	return db.GetRequest(ctx, row.ID)
}

// FindActiveRequestByMBID is FindActiveRequest for the MusicBrainz-keyed types.
func (db *DB) FindActiveRequestByMBID(
	ctx context.Context,
	mediaType, mbid string,
) (*ent.Request, error) {
	row, err := db.client.Request.Query().
		Where(
			request.MediaTypeEQ(request.MediaType(mediaType)),
			request.MediaMbidEQ(mbid),
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

// MarkRequestAvailable flips one request, for rows MarkRequestsAvailable's
// media_id key cannot reach.
func (db *DB) MarkRequestAvailable(ctx context.Context, id uint32) error {
	return db.client.Request.UpdateOneID(id).
		SetStatus(request.StatusAvailable).
		Exec(ctx)
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
	if len(p.MediaTypes) > 0 {
		types := make([]request.MediaType, len(p.MediaTypes))
		for i, t := range p.MediaTypes {
			types[i] = request.MediaType(t)
		}
		q = q.Where(request.MediaTypeIn(types...))
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

// ApproveAlbumRequest approves and replaces the requester's artist hints with
// the verified artist in one statement, so a reader never sees an approved
// request still carrying the unverified pair.
func (db *DB) ApproveAlbumRequest(
	ctx context.Context,
	id, adminID uint32,
	artistMBID, artistName string,
) error {
	return db.client.Request.UpdateOneID(id).
		SetStatus(request.StatusApproved).
		SetApprovedByID(adminID).
		SetArtistMbid(artistMBID).
		SetArtistName(artistName).
		Exec(ctx)
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

// MarkRequestsAvailableByMBID is MarkRequestsAvailable for the MusicBrainz-keyed
// types.
func (db *DB) MarkRequestsAvailableByMBID(
	ctx context.Context,
	mediaType, mbid string,
) error {
	_, err := db.client.Request.Update().
		Where(
			request.MediaTypeEQ(request.MediaType(mediaType)),
			request.MediaMbidEQ(mbid),
			request.StatusEQ(request.StatusApproved),
		).SetStatus(request.StatusAvailable).Save(ctx)
	return err
}

func (db *DB) CountRequestsByStatus(
	ctx context.Context,
	status request.Status,
	requesterID uint32,
) (int, error) {
	q := db.client.Request.Query().Where(request.StatusEQ(status))
	if requesterID != 0 {
		q = q.Where(request.HasRequesterWith(user.IDEQ(requesterID)))
	}
	return q.Count(ctx)
}
