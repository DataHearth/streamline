// Package appaccess records when a user's Subsonic password or OPDS token was
// last used and by which client. It sits on the hot path of media streaming,
// so the bookkeeping is debounced and the write never runs on the request.
package appaccess

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/observability"
)

type Kind uint8

const (
	Subsonic Kind = iota + 1
	OPDS
)

const (
	touchWindow  = 5 * time.Minute
	touchTimeout = 5 * time.Second
	clientMaxLen = 64
)

type key struct {
	userID uint32
	kind   Kind
}

type entry struct {
	credentialSince time.Time
	lastAttempt     time.Time
}

type Tracker struct {
	client *ent.Client

	mu      sync.Mutex
	entries map[key]entry
}

func NewTracker(client *ent.Client) *Tracker {
	return &Tracker{client: client, entries: map[key]entry{}}
}

// Touch records a successful authentication. credentialSince is the
// credential's created_at as the authenticate step loaded it (zero for a row
// that predates the column). A rotation changes it, so the first call with the
// new secret is never swallowed by the old secret's window, and the guarded
// UPDATE matches nothing for a credential rotated or deleted since.
func (t *Tracker) Touch(
	ctx context.Context,
	userID uint32,
	kind Kind,
	credentialSince time.Time,
	client string,
) {
	credentialSince = credentialSince.UTC()
	now := time.Now()
	k := key{userID: userID, kind: kind}

	t.mu.Lock()
	prev, seen := t.entries[k]
	if seen && prev.credentialSince.Equal(credentialSince) &&
		now.Sub(prev.lastAttempt) < touchWindow {
		t.mu.Unlock()
		return
	}
	t.entries[k] = entry{credentialSince: credentialSince, lastAttempt: now}
	t.mu.Unlock()

	client = sanitizeClient(client)
	ctx = context.WithoutCancel(ctx)
	go func() {
		defer observability.RecoverPanic(ctx, "appaccess touch", func() {
			slog.WarnContext(ctx, "app access touch panicked")
		})
		ctx, cancel := context.WithTimeout(ctx, touchTimeout)
		defer cancel()
		if err := t.write(ctx, k, credentialSince, now, client); err != nil {
			slog.WarnContext(ctx, "record app access failed",
				"user.id", userID, "error", err)
		}
	}()
}

func (t *Tracker) write(
	ctx context.Context,
	k key,
	credentialSince, at time.Time,
	client string,
) error {
	upd := t.client.User.Update()
	switch k.kind {
	case Subsonic:
		upd = upd.Where(user.ID(k.userID), subsonicCreatedAt(credentialSince)).
			SetSubsonicLastUsedAt(at).
			SetSubsonicLastClient(client)
	case OPDS:
		upd = upd.Where(user.ID(k.userID), opdsCreatedAt(credentialSince)).
			SetOpdsLastUsedAt(at).
			SetOpdsLastClient(client)
	}
	_, err := upd.Save(ctx)
	return err
}

func sanitizeClient(s string) string {
	s = strings.Map(func(r rune) rune {
		if !unicode.IsPrint(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	for len(s) > clientMaxLen {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

func subsonicCreatedAt(since time.Time) predicate.User {
	if since.IsZero() {
		return user.SubsonicCreatedAtIsNil()
	}
	return user.SubsonicCreatedAtEQ(since)
}

func opdsCreatedAt(since time.Time) predicate.User {
	if since.IsZero() {
		return user.OpdsCreatedAtIsNil()
	}
	return user.OpdsCreatedAtEQ(since)
}
