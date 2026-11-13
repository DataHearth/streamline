-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_artists" table
CREATE TABLE `new_artists` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `mbid` text NOT NULL, `name` text NOT NULL, `sort_name` text NULL, `overview` text NULL, `overview_source` text NULL, `overview_fr` text NULL, `overview_source_fr` text NULL, `monitor` text NOT NULL DEFAULT ('all'), `type` text NULL, `origin` text NULL, `since` integer NULL, `genre` text NULL, `deezer_id` integer NULL, `wikidata_id` text NULL, `quality_profile` text NULL, `last_refreshed_at` datetime NULL, `details_fetched_at` datetime NULL, `art_sidecar_hash` text NULL);
-- copy rows from old table "artists" to new temporary table "new_artists"
INSERT INTO `new_artists` (`id`, `create_time`, `update_time`, `mbid`, `name`, `sort_name`, `overview`, `overview_source`, `overview_fr`, `overview_source_fr`, `monitor`, `type`, `origin`, `since`, `genre`, `deezer_id`, `wikidata_id`, `quality_profile`, `last_refreshed_at`, `details_fetched_at`, `art_sidecar_hash`) SELECT `id`, `create_time`, `update_time`, `mbid`, `name`, `sort_name`, `overview`, `overview_source`, `overview_fr`, `overview_source_fr`, `monitor`, `type`, `origin`, `since`, `genre`, `deezer_id`, `wikidata_id`, `quality_profile`, `last_refreshed_at`, `details_fetched_at`, `art_sidecar_hash` FROM `artists`;
-- drop "artists" table after copying rows
DROP TABLE `artists`;
-- rename temporary table "new_artists" to "artists"
ALTER TABLE `new_artists` RENAME TO `artists`;
-- create index "artists_mbid_key" to table: "artists"
CREATE UNIQUE INDEX `artists_mbid_key` ON `artists` (`mbid`);
-- create index "artist_create_time" to table: "artists"
CREATE INDEX `artist_create_time` ON `artists` (`create_time`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
