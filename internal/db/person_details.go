package db

import (
	"context"
	"fmt"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/person"
	"github.com/datahearth/streamline/internal/metadata"
)

// PeopleNeedingDetails returns the people credited on one title whose
// biographical record has never been fetched — details_fetched_at is nil.
//
// It is the work list for the post-commit enrichment pass, and the reason a
// person credited on thirty titles costs one provider call rather than
// thirty: once the stamp is set the row stops being returned here, whichever
// title asks next.
//
// The Credits count is not populated — this is the enrichment work list, not
// a display query, and the rollup costs a join over the whole credits table.
func (db *DB) PeopleNeedingDetails(
	ctx context.Context,
	owner CastOwner,
	ownerID uint32,
) ([]Person, error) {
	ownedBy, err := creditsOf(owner, ownerID)
	if err != nil {
		return nil, err
	}
	rows, err := db.client.Person.Query().
		Where(
			person.DetailsFetchedAtIsNil(),
			person.HasCreditsWith(ownedBy),
		).
		Order(ent.Asc(person.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("people needing details: %w", err)
	}

	out := make([]Person, 0, len(rows))
	for _, p := range rows {
		out = append(out, personRow(p))
	}
	return out, nil
}

// SavePersonDetails writes one person's biographical record and stamps
// details_fetched_at, which is what stops the row being fetched again.
//
// Only a successful provider lookup reaches here: a failed one deliberately
// leaves the stamp nil so a later refresh retries.
func (db *DB) SavePersonDetails(
	ctx context.Context,
	id uint32,
	d metadata.PersonDetails,
) error {
	upd := db.client.Person.UpdateOneID(id).
		SetBiography(d.Biography).
		SetKnownFor(d.KnownFor).
		SetBirthday(d.Birthday).
		SetDeathday(d.Deathday).
		SetPlaceOfBirth(d.PlaceOfBirth).
		SetImdbID(d.IMDbID).
		SetInstagramID(d.InstagramID).
		SetTwitterID(d.TwitterID).
		SetDetailsFetchedAt(time.Now())
	// The person record's portrait is the same provider's canonical image as
	// the cast entry's, so writing it is a no-op where the cast already
	// carried one and fills the hole where it did not — TVDB frequently lists
	// a cast member with no image but holds one on the people record.
	if d.ProfileURL != "" {
		upd = upd.SetProfileURL(d.ProfileURL)
	}
	if err := upd.Exec(ctx); err != nil {
		return fmt.Errorf("save person %d details: %w", id, err)
	}
	return nil
}
