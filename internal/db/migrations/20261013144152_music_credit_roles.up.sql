-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_albums" table
CREATE TABLE `new_albums` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `mbid` text NOT NULL, `release_mbid` text NULL, `barcode` text NULL, `title` text NOT NULL, `type` text NOT NULL DEFAULT ('album'), `release_date` datetime NULL, `original_date` text NULL, `edition_date` text NULL, `genres` json NULL, `monitored` bool NOT NULL DEFAULT (true), `monitor_pinned` bool NOT NULL DEFAULT (false), `tags_managed` bool NOT NULL DEFAULT (true), `art_sidecar_hash` text NULL, `grab_failures` integer NOT NULL DEFAULT (0), `last_search_at` datetime NULL, `status` text NOT NULL DEFAULT ('wanted'), `failure_reason` text NULL, `label` text NULL, `catalog_number` text NULL, `country` text NULL, `media` text NULL, `studio` text NULL, `metadata_fetched_at` datetime NULL, `credits_fetched_at` datetime NULL, `credits_version` integer NOT NULL DEFAULT (0), `artist_albums` integer NOT NULL, CONSTRAINT `albums_artists_albums` FOREIGN KEY (`artist_albums`) REFERENCES `artists` (`id`) ON DELETE CASCADE);
-- copy rows from old table "albums" to new temporary table "new_albums"
INSERT INTO `new_albums` (`id`, `create_time`, `update_time`, `mbid`, `release_mbid`, `barcode`, `title`, `type`, `release_date`, `original_date`, `edition_date`, `genres`, `monitored`, `monitor_pinned`, `tags_managed`, `art_sidecar_hash`, `grab_failures`, `last_search_at`, `status`, `failure_reason`, `label`, `catalog_number`, `country`, `media`, `studio`, `metadata_fetched_at`, `credits_fetched_at`, `artist_albums`) SELECT `id`, `create_time`, `update_time`, `mbid`, `release_mbid`, `barcode`, `title`, `type`, `release_date`, `original_date`, `edition_date`, `genres`, `monitored`, `monitor_pinned`, `tags_managed`, `art_sidecar_hash`, `grab_failures`, `last_search_at`, `status`, `failure_reason`, `label`, `catalog_number`, `country`, `media`, `studio`, `metadata_fetched_at`, `credits_fetched_at`, `artist_albums` FROM `albums`;
-- drop "albums" table after copying rows
DROP TABLE `albums`;
-- rename temporary table "new_albums" to "albums"
ALTER TABLE `new_albums` RENAME TO `albums`;
-- create index "albums_mbid_key" to table: "albums"
CREATE UNIQUE INDEX `albums_mbid_key` ON `albums` (`mbid`);
-- create index "album_artist_albums" to table: "albums"
CREATE INDEX `album_artist_albums` ON `albums` (`artist_albums`);
-- create index "album_status" to table: "albums"
CREATE INDEX `album_status` ON `albums` (`status`);
-- create index "album_metadata_fetched_at" to table: "albums"
CREATE INDEX `album_metadata_fetched_at` ON `albums` (`metadata_fetched_at`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
