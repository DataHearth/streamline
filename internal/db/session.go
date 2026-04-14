package db

import (
	"context"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/session"
	"github.com/datahearth/streamline/ent/user"
)

type CreateSessionParams struct {
	JTI       string
	UserID    uint32
	IP        string
	UserAgent string
	ExpiresAt time.Time
}

func (db *DB) CreateSession(
	ctx context.Context,
	p CreateSessionParams,
) (*ent.Session, error) {
	b := db.client.Session.Create().
		SetJti(p.JTI).
		SetUserID(p.UserID).
		SetExpiresAt(p.ExpiresAt)
	if p.IP != "" {
		b.SetIP(p.IP)
	}
	if p.UserAgent != "" {
		b.SetUserAgent(p.UserAgent)
	}
	return b.Save(ctx)
}

func (db *DB) FindSessionByJTI(
	ctx context.Context,
	jti string,
) (*ent.Session, error) {
	return db.client.Session.Query().Where(session.Jti(jti)).Only(ctx)
}

func (db *DB) TouchSession(ctx context.Context, jti string, when time.Time) error {
	_, err := db.client.Session.Update().
		Where(session.Jti(jti)).
		SetLastSeenAt(when).
		Save(ctx)
	return err
}

// RevokeSessionByJTI marks the session with the given jti as revoked. A second
// call against an already-revoked row is a no-op.
func (db *DB) RevokeSessionByJTI(
	ctx context.Context,
	jti string,
	when time.Time,
) error {
	_, err := db.client.Session.Update().
		Where(
			session.Jti(jti),
			session.RevokedAtIsNil(),
		).
		SetRevokedAt(when).
		Save(ctx)
	return err
}

// RevokeUserSessionByID revokes a session owned by userID. Returns the number
// of rows affected (0 if already revoked, missing, or not owned by userID).
func (db *DB) RevokeUserSessionByID(
	ctx context.Context,
	userID, sessionID uint32,
	when time.Time,
) (int, error) {
	return db.client.Session.Update().
		Where(
			session.IDEQ(sessionID),
			session.HasUserWith(user.IDEQ(userID)),
			session.RevokedAtIsNil(),
		).
		SetRevokedAt(when).
		Save(ctx)
}

// UserSessionExists reports whether a session with sessionID exists and is
// owned by userID, regardless of its revoked state.
func (db *DB) UserSessionExists(
	ctx context.Context,
	userID, sessionID uint32,
) (bool, error) {
	return db.client.Session.Query().
		Where(
			session.IDEQ(sessionID),
			session.HasUserWith(user.IDEQ(userID)),
		).
		Exist(ctx)
}

func (db *DB) RevokeAllUserSessions(
	ctx context.Context,
	userID uint32,
	when time.Time,
) error {
	_, err := db.client.Session.Update().
		Where(
			session.HasUserWith(user.IDEQ(userID)),
			session.RevokedAtIsNil(),
		).
		SetRevokedAt(when).
		Save(ctx)
	return err
}

func (db *DB) RevokeOtherUserSessions(
	ctx context.Context,
	userID uint32,
	keepJTI string,
	when time.Time,
) error {
	_, err := db.client.Session.Update().
		Where(
			session.HasUserWith(user.IDEQ(userID)),
			session.RevokedAtIsNil(),
			session.JtiNEQ(keepJTI),
		).
		SetRevokedAt(when).
		Save(ctx)
	return err
}

func (db *DB) ListUserSessions(
	ctx context.Context,
	userID uint32,
) ([]*ent.Session, error) {
	return db.client.Session.Query().
		Where(session.HasUserWith(user.IDEQ(userID))).
		// id is the tiebreak so two sessions sharing a last_seen_at (e.g. both
		// just created) still sort newest-first deterministically.
		Order(ent.Desc(session.FieldLastSeenAt), ent.Desc(session.FieldID)).
		All(ctx)
}

func (db *DB) PurgeExpiredSessions(
	ctx context.Context,
	before time.Time,
) (int, error) {
	return db.client.Session.Delete().
		Where(session.ExpiresAtLT(before)).
		Exec(ctx)
}

// TruncateSessions deletes every row in the sessions table. Called by
// auth.RotateJWTSecret to invalidate every outstanding session in one shot
// (old tokens are also signed with the old secret, so this is belt-and-
// braces — but it stops dead rows accumulating).
func (db *DB) TruncateSessions(ctx context.Context) error {
	_, err := db.client.Session.Delete().Exec(ctx)
	return err
}
