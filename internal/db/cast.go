package db

import (
	"context"
	"errors"
	"fmt"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/credit"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/person"
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/ent/tvshow"
	"github.com/datahearth/streamline/internal/metadata"
)

// CastOwner names which title kind a set of credits hangs off. Credit carries
// one optional edge per kind and exactly one of them is ever set — the
// MediaEvent pattern — so the owner has to be named explicitly rather than
// inferred from an id.
type CastOwner string

const (
	CastOwnerMovie  CastOwner = "movie"
	CastOwnerSeries CastOwner = "series"
)

var errUnknownCastOwner = errors.New("unknown cast owner")

// ReplaceCast persists a title's cast into persons/credits, replacing that
// owner's credits wholesale so a metadata refresh cannot accumulate
// duplicates.
//
// An empty incoming list is a no-op, not a wipe. Cast comes from a different
// provider call than the rest of a title's metadata, so "no cast" is far more
// often "that call failed or was never made" than "this title has no actors"
// — the same reason UpdateTVShowMetadata leaves the stored cast alone. A title
// that genuinely has no cast simply never gains credits in the first place.
func (db *DB) ReplaceCast(
	ctx context.Context,
	owner CastOwner,
	ownerID uint32,
	cast []metadata.CastMember,
) error {
	if len(cast) == 0 {
		return nil
	}
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin cast transaction: %w", err)
	}
	if err := replaceCast(ctx, tx.Client(), owner, ownerID, cast); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// replaceCast is ReplaceCast's body against an arbitrary client, so a caller
// that already holds a transaction (CreateMovie, CreateTVShow, the two
// metadata updates) folds the credits into it rather than opening a second
// one. SQLite has a single writer: a nested db.client.Tx would be a different
// connection blocking on the lock its own caller holds.
func replaceCast(
	ctx context.Context,
	cl *ent.Client,
	owner CastOwner,
	ownerID uint32,
	cast []metadata.CastMember,
) error {
	if len(cast) == 0 {
		return nil
	}
	ownedBy, err := creditsOf(owner, ownerID)
	if err != nil {
		return err
	}
	if _, err := cl.Credit.Delete().Where(ownedBy).Exec(ctx); err != nil {
		return fmt.Errorf("delete existing credits: %w", err)
	}

	creates := make([]*ent.CreditCreate, 0, len(cast))
	// Billing order is the entry's position in the provider's list. The column
	// is uint8 and every provider caps cast well below that, so saturating is
	// unreachable in practice — it exists so a longer list keeps its tail at
	// the bottom of the billing instead of wrapping around to the top.
	var order uint8
	for _, c := range cast {
		// Person.name is NotEmpty and a nameless entry has nothing to resolve
		// or display; the provider gave us a hole, not a person.
		if c.Name == "" {
			continue
		}
		personID, err := resolvePerson(ctx, cl, c)
		if err != nil {
			return err
		}
		b := cl.Credit.Create().
			SetPersonID(personID).
			SetCharacter(c.Character).
			SetOrder(order)
		switch owner {
		case CastOwnerMovie:
			b = b.SetMovieID(ownerID)
		case CastOwnerSeries:
			b = b.SetTvShowID(ownerID)
		}
		creates = append(creates, b)
		if order < ^uint8(0) {
			order++
		}
	}
	if len(creates) == 0 {
		return nil
	}
	if _, err := cl.Credit.CreateBulk(creates...).Save(ctx); err != nil {
		return fmt.Errorf("create credits: %w", err)
	}
	return nil
}

// CastEntry is one credited person on one title: the person as the library
// knows them, plus the character that belongs to this pairing. PersonID is the
// persons row id, which is what the API keys /people/{id} by — a cast entry
// without it cannot link anywhere, since a TVDB-sourced person has no tmdb id.
type CastEntry struct {
	PersonID   uint32
	TMDBID     uint32
	TVDBID     uint32
	Name       string
	Character  string
	ProfileURL string
}

// TitleCast returns a title's cast in billing order — the order the provider
// listed them in, stamped onto the credit at write time.
func (db *DB) TitleCast(
	ctx context.Context,
	owner CastOwner,
	ownerID uint32,
) ([]CastEntry, error) {
	ownedBy, err := creditsOf(owner, ownerID)
	if err != nil {
		return nil, err
	}
	rows, err := db.client.Credit.Query().
		Where(ownedBy).
		WithPerson().
		// order is a uint8 the whole cast can share once a provider lists more
		// entries than it can hold, so id breaks the tie and keeps the tail in
		// the sequence it was written.
		Order(ent.Asc(credit.FieldOrder), ent.Asc(credit.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("title cast: %w", err)
	}

	out := make([]CastEntry, 0, len(rows))
	for _, c := range rows {
		p := c.Edges.Person
		if p == nil {
			continue
		}
		out = append(out, CastEntry{
			PersonID:   p.ID,
			TMDBID:     p.TmdbID,
			TVDBID:     p.TvdbID,
			Name:       p.Name,
			Character:  c.Character,
			ProfileURL: p.ProfileURL,
		})
	}
	return out, nil
}

func creditsOf(owner CastOwner, ownerID uint32) (predicate.Credit, error) {
	switch owner {
	case CastOwnerMovie:
		return credit.HasMovieWith(movie.IDEQ(ownerID)), nil
	case CastOwnerSeries:
		return credit.HasTvShowWith(tvshow.IDEQ(ownerID)), nil
	default:
		return nil, fmt.Errorf("%w: %q", errUnknownCastOwner, owner)
	}
}

// resolvePerson maps one incoming cast entry onto a persons row, creating it
// when nothing matches.
//
// Identity is resolved in this order, each step falling through to the next on
// a miss: tmdb_id, then tvdb_id, then the folded name. The order is the
// load-bearing part. A provider id *is* an identity; a name is only a guess at
// one, so it is the last resort and never overrides an id that matched. A row
// found by tmdb_id is that person even if the name has since changed
// upstream, and two different people who share a name stay two rows as long as
// either carries an id — the name fallback refuses any candidate whose own id
// contradicts the incoming one.
//
// The fallback is what makes enrichment possible: a person first seen
// name-only (a backfill of the legacy JSON cast, which carried no ids for
// TVDB entries) is found by name on the next metadata refresh and gains their
// real provider id then. Ids only ever go 0 → non-zero; a non-zero id is
// never rewritten to a different non-zero one, since that would silently
// re-point every existing credit at somebody else.
func resolvePerson(
	ctx context.Context,
	cl *ent.Client,
	c metadata.CastMember,
) (uint32, error) {
	found, err := matchPerson(ctx, cl, c)
	if err != nil {
		return 0, err
	}
	if found == nil {
		row, err := cl.Person.Create().
			SetTmdbID(c.TMDBID).
			SetTvdbID(c.TVDBID).
			SetName(c.Name).
			SetProfileURL(c.ProfileURL).
			Save(ctx)
		if err != nil {
			return 0, fmt.Errorf("create person %q: %w", c.Name, err)
		}
		return row.ID, nil
	}

	upd := cl.Person.UpdateOne(found)
	var enriched bool
	if found.TmdbID == 0 && c.TMDBID != 0 {
		upd, enriched = upd.SetTmdbID(c.TMDBID), true
	}
	if found.TvdbID == 0 && c.TVDBID != 0 {
		upd, enriched = upd.SetTvdbID(c.TVDBID), true
	}
	if found.ProfileURL == "" && c.ProfileURL != "" {
		upd, enriched = upd.SetProfileURL(c.ProfileURL), true
	}
	if enriched {
		if err := upd.Exec(ctx); err != nil {
			return 0, fmt.Errorf("enrich person %q: %w", c.Name, err)
		}
	}
	return found.ID, nil
}

// matchPerson runs the lookup cascade, returning nil when no stored row is
// this person.
func matchPerson(
	ctx context.Context,
	cl *ent.Client,
	c metadata.CastMember,
) (*ent.Person, error) {
	if c.TMDBID != 0 {
		row, err := firstPerson(ctx, cl, person.TmdbIDEQ(c.TMDBID))
		if err != nil || row != nil {
			return row, err
		}
	}
	if c.TVDBID != 0 {
		row, err := firstPerson(ctx, cl, person.TvdbIDEQ(c.TVDBID))
		if err != nil || row != nil {
			return row, err
		}
	}
	rows, err := cl.Person.Query().
		Where(foldedNameEQ(c.Name)).
		Order(ent.Asc(person.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("match person by name %q: %w", c.Name, err)
	}
	for _, row := range rows {
		if compatible(row.TmdbID, c.TMDBID) && compatible(row.TvdbID, c.TVDBID) {
			return row, nil
		}
	}
	return nil, nil
}

// compatible reports whether a stored id and an incoming one can belong to the
// same person: either side being 0 is "unknown", which anything satisfies.
func compatible(stored, incoming uint32) bool {
	return stored == 0 || incoming == 0 || stored == incoming
}

func firstPerson(
	ctx context.Context,
	cl *ent.Client,
	p predicate.Person,
) (*ent.Person, error) {
	row, err := cl.Person.Query().Where(p).Order(ent.Asc(person.FieldID)).First(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("match person: %w", err)
	}
	return row, nil
}

// foldedNameEQ matches a stored name against an incoming one with both sides
// case- and accent-folded, so "Renée Zellweger" and "Renee Zellweger" from two
// providers are one person rather than two.
func foldedNameEQ(name string) predicate.Person {
	folded := foldText(name)
	return func(s *entsql.Selector) {
		s.Where(entsql.ExprP(
			"fold("+s.C(person.FieldName)+") = ?", folded,
		))
	}
}
