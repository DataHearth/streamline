-- Everything in persons and credits came from the backfill: the tables were
-- created one migration earlier and this is the only thing that has ever
-- written to them at this schema version. Emptying both is therefore the exact
-- reversal, and it loses nothing — the legacy JSON `cast` columns this read
-- from are left untouched by the up migration.
DELETE FROM `credits`;
DELETE FROM `persons`;
-- Give the ids back too, so re-applying the backfill produces the same rows.
DELETE FROM `sqlite_sequence` WHERE `name` IN ('credits', 'persons');
