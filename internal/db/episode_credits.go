package db

import (
	"context"
	"fmt"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/credit"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/person"
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/internal/metadata"
)

// episodeCreditsSettle is how long after airing an episode's credits are
// still read again: TVDB's guest and crew listings keep filling in for a
// while, and stay put after that.
const episodeCreditsSettle = 30 * 24 * time.Hour

type EpisodeCreditsRef struct {
	ID     uint32
	TVDBID uint32
}

func (db *DB) EpisodesNeedingCredits(
	ctx context.Context,
	now time.Time,
) ([]EpisodeCreditsRef, error) {
	rows, err := db.client.Episode.Query().
		Where(
			episode.TvdbIDNEQ(0),
			episode.AirDateNotNil(),
			episode.AirDateLTE(now),
		).
		Select(
			episode.FieldID,
			episode.FieldTvdbID,
			episode.FieldAirDate,
			episode.FieldCreditsFetchedAt,
		).
		Order(ent.Asc(episode.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("episodes needing credits: %w", err)
	}
	// ponytail: the window is checked here, not in SQL — the driver stores
	// times as Go prints them, which SQLite's date functions cannot read — so
	// every aired episode is read once a tick. A settled flag set by the
	// writer is the way out if the episodes table outgrows that.
	out := make([]EpisodeCreditsRef, 0, len(rows))
	for _, e := range rows {
		if e.CreditsFetchedAt != nil &&
			!e.CreditsFetchedAt.Before(e.AirDate.Add(episodeCreditsSettle)) {
			continue
		}
		out = append(out, EpisodeCreditsRef{ID: e.ID, TVDBID: e.TvdbID})
	}
	return out, nil
}

func (db *DB) ReplaceEpisodeCredits(
	ctx context.Context,
	episodeID uint32,
	credits []metadata.CastMember,
) error {
	tx, err := db.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin episode credits transaction: %w", err)
	}
	cl := tx.Client()
	if _, err := cl.Credit.Delete().
		Where(credit.HasEpisodeWith(episode.IDEQ(episodeID))).
		Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("delete episode credits: %w", err)
	}
	if err := replaceCast(
		ctx,
		cl,
		CastOwnerEpisode,
		episodeID,
		credits,
	); err != nil {
		tx.Rollback()
		return err
	}
	if err := cl.Episode.UpdateOneID(episodeID).
		SetCreditsFetchedAt(time.Now()).
		Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("stamp episode credits: %w", err)
	}
	return tx.Commit()
}

func (db *DB) PeopleWithPhotos(
	ctx context.Context,
	owners ...CastOwner,
) ([]Person, error) {
	kinds := make([]predicate.Credit, 0, len(owners))
	for _, o := range owners {
		switch o {
		case CastOwnerMovie:
			kinds = append(kinds, credit.HasMovie())
		case CastOwnerSeries:
			kinds = append(kinds, credit.HasTvShow())
		case CastOwnerEpisode:
			kinds = append(kinds, credit.HasEpisode())
		default:
			return nil, fmt.Errorf("%w: %q", errUnknownCastOwner, o)
		}
	}
	rows, err := db.client.Person.Query().
		Where(
			person.ProfileURLNEQ(""),
			person.HasCreditsWith(credit.Or(kinds...)),
		).
		Select(person.FieldID, person.FieldName, person.FieldProfileURL).
		Order(ent.Asc(person.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("people with photos: %w", err)
	}
	out := make([]Person, 0, len(rows))
	for _, p := range rows {
		out = append(out, Person{ID: p.ID, Name: p.Name, ProfileURL: p.ProfileURL})
	}
	return out, nil
}

func (db *DB) PersonPhotoURL(ctx context.Context, id uint32) (string, error) {
	p, err := db.client.Person.Query().
		Where(person.ID(id)).
		Select(person.FieldProfileURL).
		Only(ctx)
	if ent.IsNotFound(err) {
		return "", ErrPersonNotFound
	}
	if err != nil {
		return "", fmt.Errorf("person photo: %w", err)
	}
	return p.ProfileURL, nil
}
