-- reverse: rename scheduled_job rows to the media-scoped job names
UPDATE OR REPLACE `scheduled_jobs` SET `name` = 'rss-sync' WHERE `name` = 'movie-rss-sync';
UPDATE OR REPLACE `scheduled_jobs` SET `name` = 'missing-search' WHERE `name` = 'movie-missing-search';
UPDATE OR REPLACE `scheduled_jobs` SET `name` = 'metadata-refresh' WHERE `name` = 'movie-metadata-refresh';
UPDATE OR REPLACE `scheduled_jobs` SET `name` = 'orphan-scan' WHERE `name` = 'movie-orphan-scan';
UPDATE OR REPLACE `scheduled_jobs` SET `name` = 'series-orphan-scan' WHERE `name` = 'tv-orphan-scan';
