-- rename scheduled_job rows to the media-scoped job names
-- (movie-*/tv-* prefixes); jobs.state.Seed only inserts missing rows, so
-- without this the old rows are orphaned and their paused state is lost.
-- OR REPLACE because a DB booted on the renamed jobs before this migration
-- existed already carries a freshly seeded row under the new name; the unique
-- index on `name` would otherwise abort the rename. REPLACE drops that empty
-- row and keeps the original, which is the one holding the run history.
UPDATE OR REPLACE `scheduled_jobs` SET `name` = 'movie-rss-sync' WHERE `name` = 'rss-sync';
UPDATE OR REPLACE `scheduled_jobs` SET `name` = 'movie-missing-search' WHERE `name` = 'missing-search';
UPDATE OR REPLACE `scheduled_jobs` SET `name` = 'movie-metadata-refresh' WHERE `name` = 'metadata-refresh';
UPDATE OR REPLACE `scheduled_jobs` SET `name` = 'movie-orphan-scan' WHERE `name` = 'orphan-scan';
UPDATE OR REPLACE `scheduled_jobs` SET `name` = 'tv-orphan-scan' WHERE `name` = 'series-orphan-scan';
