package db

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/credit"
	"github.com/datahearth/streamline/ent/person"
)

// ErrPersonNotFound reports a person id with no row in the library.
var ErrPersonNotFound = errors.New("person not found")

// Person is one cast member rolled up over the whole library.
type Person struct {
	// ID is the persons row id, and the only identity the API pages and
	// looks up by. The provider ids below are data the person carries, not a
	// key: series cast comes from TVDB with no tmdb id at all, so keying on
	// tmdb_id collapsed every such actor into one.
	ID         uint32
	TMDBID     uint32
	TVDBID     uint32
	Name       string
	ProfileURL string
	PersonBio
	// Credits counts the library items — movies plus series — the person is
	// credited on, not the credit rows: a person listed twice on one title is
	// one credit.
	Credits uint32
}

// PersonBio is the biographical record the provider person-detail call fills
// in, shared by both person shapes the API serves. Every field is frequently
// absent: neither provider guarantees a biography or a death date, and a
// TVDB-sourced person has no KnownFor at all.
type PersonBio struct {
	Biography    string
	KnownFor     string
	Birthday     string
	Deathday     string
	PlaceOfBirth string
	IMDbID       string
	InstagramID  string
	TwitterID    string
}

// personRow projects a stored row onto the API-facing shape, minus the
// Credits rollup no single-row query computes.
func personRow(p *ent.Person) Person {
	return Person{
		ID:         p.ID,
		TMDBID:     p.TmdbID,
		TVDBID:     p.TvdbID,
		Name:       p.Name,
		ProfileURL: p.ProfileURL,
		PersonBio:  personBio(p),
	}
}

func personBio(p *ent.Person) PersonBio {
	return PersonBio{
		Biography:    p.Biography,
		KnownFor:     p.KnownFor,
		Birthday:     p.Birthday,
		Deathday:     p.Deathday,
		PlaceOfBirth: p.PlaceOfBirth,
		IMDbID:       p.ImdbID,
		InstagramID:  p.InstagramID,
		TwitterID:    p.TwitterID,
	}
}

type ListPeopleParams struct {
	// Query matches the person's name, folded on both sides.
	Query  string
	Offset uint32
	Limit  uint16
}

// MovieCredit is one movie a person is credited on, with the character they
// play there — the character belongs to the pairing, not to the person.
type MovieCredit struct {
	Movie     *ent.Movie
	Character string
}

// SeriesCredit is the series counterpart of MovieCredit.
type SeriesCredit struct {
	Series    *ent.TVShow
	Character string
}

// PersonCredits is everything the library knows about one person: their
// identity and both credit lists.
type PersonCredits struct {
	ID         uint32
	TMDBID     uint32
	TVDBID     uint32
	Name       string
	ProfileURL string
	PersonBio
	Movies []MovieCredit
	Series []SeriesCredit
}

// creditItemKey counts a person's credits by library item rather than by
// credit row. A credit belongs to exactly one owner, so concatenating the
// null owner yields NULL and COALESCE falls through to the other one; the
// prefix keeps a movie id and a series id of the same number apart.
func creditItemKey(cr *entsql.SelectTable) string {
	return fmt.Sprintf(
		"COUNT(DISTINCT COALESCE('movie:' || %s, 'series:' || %s))",
		cr.C(credit.MovieColumn), cr.C(credit.TvShowColumn),
	)
}

// ListPeople pages the library's cast members by credit count. Counting,
// filtering, ordering and paging all happen in SQL — the alternative is
// loading every person and every credit into Go to count names.
func (db *DB) ListPeople(
	ctx context.Context,
	p ListPeopleParams,
) ([]Person, uint32, error) {
	var totalRows []struct {
		Total uint32 `sql:"total"`
	}
	err := db.client.Person.Query().
		Modify(func(s *entsql.Selector) {
			joinCredits(s)
			s.Select(entsql.As(
				"COUNT(DISTINCT "+s.C(person.FieldID)+")", "total",
			))
			peopleWhere(s, p.Query)
		}).
		Scan(ctx, &totalRows)
	if err != nil {
		return nil, 0, fmt.Errorf("count people: %w", err)
	}
	var total uint32
	if len(totalRows) > 0 {
		total = totalRows[0].Total
	}
	if total == 0 {
		return []Person{}, 0, nil
	}

	var rows []struct {
		ID           uint32 `sql:"id"`
		TMDBID       uint32 `sql:"tmdb_id"`
		TVDBID       uint32 `sql:"tvdb_id"`
		Name         string `sql:"name"`
		ProfileURL   string `sql:"profile_url"`
		Biography    string `sql:"biography"`
		KnownFor     string `sql:"known_for"`
		Birthday     string `sql:"birthday"`
		Deathday     string `sql:"deathday"`
		PlaceOfBirth string `sql:"place_of_birth"`
		IMDbID       string `sql:"imdb_id"`
		InstagramID  string `sql:"instagram_id"`
		TwitterID    string `sql:"twitter_id"`
		Credits      uint32 `sql:"credits"`
	}
	err = db.client.Person.Query().
		Modify(func(s *entsql.Selector) {
			cr := joinCredits(s)
			credits := creditItemKey(cr)
			s.Select(
				entsql.As(s.C(person.FieldID), "id"),
				entsql.As(s.C(person.FieldTmdbID), "tmdb_id"),
				entsql.As(s.C(person.FieldTvdbID), "tvdb_id"),
				entsql.As(s.C(person.FieldName), "name"),
				entsql.As(s.C(person.FieldProfileURL), "profile_url"),
				entsql.As(s.C(person.FieldBiography), "biography"),
				entsql.As(s.C(person.FieldKnownFor), "known_for"),
				entsql.As(s.C(person.FieldBirthday), "birthday"),
				entsql.As(s.C(person.FieldDeathday), "deathday"),
				entsql.As(s.C(person.FieldPlaceOfBirth), "place_of_birth"),
				entsql.As(s.C(person.FieldImdbID), "imdb_id"),
				entsql.As(s.C(person.FieldInstagramID), "instagram_id"),
				entsql.As(s.C(person.FieldTwitterID), "twitter_id"),
				entsql.As(credits, "credits"),
			)
			peopleWhere(s, p.Query)
			// Ordered by the aggregate itself rather than by its output alias:
			// which one an alias resolves to next to a real column of the same
			// name is not worth relying on.
			s.GroupBy(s.C(person.FieldID)).
				OrderExpr(entsql.Raw(
					credits + " DESC, " + s.C(person.FieldName) + " ASC",
				)).
				Limit(int(p.Limit)).
				Offset(int(p.Offset))
		}).
		Scan(ctx, &rows)
	if err != nil {
		return nil, 0, fmt.Errorf("list people: %w", err)
	}

	out := make([]Person, 0, len(rows))
	for _, r := range rows {
		out = append(out, Person{
			ID:         r.ID,
			TMDBID:     r.TMDBID,
			TVDBID:     r.TVDBID,
			Name:       r.Name,
			ProfileURL: r.ProfileURL,
			PersonBio: PersonBio{
				Biography:    r.Biography,
				KnownFor:     r.KnownFor,
				Birthday:     r.Birthday,
				Deathday:     r.Deathday,
				PlaceOfBirth: r.PlaceOfBirth,
				IMDbID:       r.IMDbID,
				InstagramID:  r.InstagramID,
				TwitterID:    r.TwitterID,
			},
			Credits: r.Credits,
		})
	}
	return out, total, nil
}

// joinCredits inner-joins the credits table, which both drops a person
// nothing credits and gives the count something to aggregate over.
func joinCredits(s *entsql.Selector) *entsql.SelectTable {
	cr := entsql.Dialect(s.Dialect()).Table(credit.Table).As("person_credit")
	s.Join(cr).On(s.C(person.FieldID), cr.C(credit.PersonColumn))
	return cr
}

func peopleWhere(s *entsql.Selector, query string) {
	if query != "" {
		s.Where(foldContains(s, person.FieldName, query))
	}
}

// PersonCredits returns every library item the given person is credited on.
// ErrPersonNotFound when no such person row exists.
func (db *DB) PersonCredits(
	ctx context.Context,
	id uint32,
) (*PersonCredits, error) {
	p, err := db.client.Person.Query().
		Where(person.ID(id)).
		WithCredits(func(q *ent.CreditQuery) {
			q.WithMovie().WithTvShow()
		}).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrPersonNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("person credits: %w", err)
	}

	out := &PersonCredits{
		ID:         p.ID,
		TMDBID:     p.TmdbID,
		TVDBID:     p.TvdbID,
		Name:       p.Name,
		ProfileURL: p.ProfileURL,
		PersonBio:  personBio(p),
		Movies:     make([]MovieCredit, 0, len(p.Edges.Credits)),
		Series:     make([]SeriesCredit, 0, len(p.Edges.Credits)),
	}
	for _, c := range p.Edges.Credits {
		switch {
		case c.Edges.Movie != nil:
			out.Movies = append(out.Movies, MovieCredit{
				Movie:     c.Edges.Movie,
				Character: c.Character,
			})
		case c.Edges.TvShow != nil:
			out.Series = append(out.Series, SeriesCredit{
				Series:    c.Edges.TvShow,
				Character: c.Character,
			})
		}
	}
	slices.SortFunc(out.Movies, func(a, b MovieCredit) int {
		return strings.Compare(a.Movie.Title, b.Movie.Title)
	})
	slices.SortFunc(out.Series, func(a, b SeriesCredit) int {
		return strings.Compare(a.Series.Title, b.Series.Title)
	})
	return out, nil
}
