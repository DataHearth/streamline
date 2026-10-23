-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_media_files" table
CREATE TABLE `new_media_files` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `path` text NOT NULL, `size` integer NOT NULL, `quality` text NULL, `format` text NULL, `release_group` text NULL, `source` text NOT NULL DEFAULT ('auto'), `last_seen_at` datetime NULL, `missing_since` datetime NULL, `container` text NULL, `duration_seconds` integer NULL, `video_codec` text NULL, `width` integer NULL, `height` integer NULL, `audio_codec` text NULL, `audio_channels` integer NULL, `bitrate` integer NULL, `audio_tracks` integer NULL, `audio_langs` text NULL, `sub_langs` text NULL, `probed_at` datetime NULL, `parsed_source` text NULL, `parsed_resolution` text NULL, `parsed_codec` text NULL, `transcoded_at` datetime NULL, `size_before` integer NULL, `episode_media_files` integer NULL, `movie_media_files` integer NULL, `track_media_files` integer NULL, CONSTRAINT `media_files_episodes_media_files` FOREIGN KEY (`episode_media_files`) REFERENCES `episodes` (`id`) ON DELETE CASCADE, CONSTRAINT `media_files_movies_media_files` FOREIGN KEY (`movie_media_files`) REFERENCES `movies` (`id`) ON DELETE CASCADE, CONSTRAINT `media_files_tracks_media_files` FOREIGN KEY (`track_media_files`) REFERENCES `tracks` (`id`) ON DELETE CASCADE);
-- copy rows from old table "media_files" to new temporary table "new_media_files"
INSERT INTO `new_media_files` (`id`, `create_time`, `update_time`, `path`, `size`, `quality`, `format`, `release_group`, `source`, `last_seen_at`, `missing_since`, `container`, `duration_seconds`, `video_codec`, `width`, `height`, `audio_codec`, `audio_channels`, `bitrate`, `audio_tracks`, `audio_langs`, `sub_langs`, `probed_at`, `parsed_source`, `parsed_resolution`, `parsed_codec`, `transcoded_at`, `size_before`, `episode_media_files`, `movie_media_files`) SELECT `id`, `create_time`, `update_time`, `path`, `size`, `quality`, `format`, `release_group`, `source`, `last_seen_at`, `missing_since`, `container`, `duration_seconds`, `video_codec`, `width`, `height`, `audio_codec`, `audio_channels`, `bitrate`, `audio_tracks`, `audio_langs`, `sub_langs`, `probed_at`, `parsed_source`, `parsed_resolution`, `parsed_codec`, `transcoded_at`, `size_before`, `episode_media_files`, `movie_media_files` FROM `media_files`;
-- drop "media_files" table after copying rows
DROP TABLE `media_files`;
-- rename temporary table "new_media_files" to "media_files"
ALTER TABLE `new_media_files` RENAME TO `media_files`;
-- create index "mediafile_episode_media_files" to table: "media_files"
CREATE INDEX `mediafile_episode_media_files` ON `media_files` (`episode_media_files`);
-- create index "mediafile_movie_media_files" to table: "media_files"
CREATE INDEX `mediafile_movie_media_files` ON `media_files` (`movie_media_files`);
-- create index "mediafile_track_media_files" to table: "media_files"
CREATE INDEX `mediafile_track_media_files` ON `media_files` (`track_media_files`);
-- create index "mediafile_probed_at" to table: "media_files"
CREATE INDEX `mediafile_probed_at` ON `media_files` (`probed_at`) WHERE probed_at IS NULL;
-- create "new_download_records" table
CREATE TABLE `new_download_records` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `title` text NOT NULL, `quality` text NULL, `size` integer NULL, `status` text NOT NULL DEFAULT ('downloading'), `torrent_hash` text NULL, `release_group` text NULL, `save_path` text NULL, `import_attempts` integer NOT NULL DEFAULT (0), `failure_reason` text NULL, `imported_at` datetime NULL, `indexer_name` text NULL, `download_client_name` text NULL, `replace_mode` text NOT NULL DEFAULT ('none'), `hold_reasons` json NULL, `verification_bypassed` bool NOT NULL DEFAULT (false), `selected_files` json NULL, `selected_bytes` integer NULL, `selection_state` text NOT NULL DEFAULT ('skipped'), `album_download_records` integer NULL, `episode_download_records` integer NULL, `movie_download_records` integer NULL, CONSTRAINT `download_records_albums_download_records` FOREIGN KEY (`album_download_records`) REFERENCES `albums` (`id`) ON DELETE CASCADE, CONSTRAINT `download_records_episodes_anchored_download_records` FOREIGN KEY (`episode_download_records`) REFERENCES `episodes` (`id`) ON DELETE CASCADE, CONSTRAINT `download_records_movies_download_records` FOREIGN KEY (`movie_download_records`) REFERENCES `movies` (`id`) ON DELETE CASCADE);
-- copy rows from old table "download_records" to new temporary table "new_download_records"
INSERT INTO `new_download_records` (`id`, `create_time`, `update_time`, `title`, `quality`, `size`, `status`, `torrent_hash`, `release_group`, `save_path`, `import_attempts`, `failure_reason`, `imported_at`, `indexer_name`, `download_client_name`, `replace_mode`, `hold_reasons`, `verification_bypassed`, `selected_files`, `selected_bytes`, `selection_state`, `episode_download_records`, `movie_download_records`) SELECT `id`, `create_time`, `update_time`, `title`, `quality`, `size`, `status`, `torrent_hash`, `release_group`, `save_path`, `import_attempts`, `failure_reason`, `imported_at`, `indexer_name`, `download_client_name`, `replace_mode`, `hold_reasons`, `verification_bypassed`, `selected_files`, `selected_bytes`, `selection_state`, `episode_download_records`, `movie_download_records` FROM `download_records`;
-- drop "download_records" table after copying rows
DROP TABLE `download_records`;
-- rename temporary table "new_download_records" to "download_records"
ALTER TABLE `new_download_records` RENAME TO `download_records`;
-- create index "downloadrecord_selection_state" to table: "download_records"
CREATE INDEX `downloadrecord_selection_state` ON `download_records` (`selection_state`);
-- create index "downloadrecord_status" to table: "download_records"
CREATE INDEX `downloadrecord_status` ON `download_records` (`status`);
-- create index "downloadrecord_torrent_hash" to table: "download_records"
CREATE INDEX `downloadrecord_torrent_hash` ON `download_records` (`torrent_hash`);
-- create index "downloadrecord_update_time_id" to table: "download_records"
CREATE INDEX `downloadrecord_update_time_id` ON `download_records` (`update_time`, `id`);
-- create index "downloadrecord_movie_download_records" to table: "download_records"
CREATE INDEX `downloadrecord_movie_download_records` ON `download_records` (`movie_download_records`);
-- create index "downloadrecord_episode_download_records" to table: "download_records"
CREATE INDEX `downloadrecord_episode_download_records` ON `download_records` (`episode_download_records`);
-- create "albums" table
CREATE TABLE `albums` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `mbid` text NOT NULL, `release_mbid` text NULL, `title` text NOT NULL, `type` text NOT NULL DEFAULT ('album'), `release_date` datetime NULL, `monitored` bool NOT NULL DEFAULT (true), `grab_failures` integer NOT NULL DEFAULT (0), `last_search_at` datetime NULL, `status` text NOT NULL DEFAULT ('wanted'), `artist_albums` integer NOT NULL, CONSTRAINT `albums_artists_albums` FOREIGN KEY (`artist_albums`) REFERENCES `artists` (`id`) ON DELETE CASCADE);
-- create index "albums_mbid_key" to table: "albums"
CREATE UNIQUE INDEX `albums_mbid_key` ON `albums` (`mbid`);
-- create index "album_artist_albums" to table: "albums"
CREATE INDEX `album_artist_albums` ON `albums` (`artist_albums`);
-- create index "album_status" to table: "albums"
CREATE INDEX `album_status` ON `albums` (`status`);
-- create "artists" table
CREATE TABLE `artists` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `mbid` text NOT NULL, `name` text NOT NULL, `sort_name` text NULL, `overview` text NULL, `monitored` bool NOT NULL DEFAULT (true), `path` text NULL, `quality_profile` text NULL, `last_refreshed_at` datetime NULL);
-- create index "artists_mbid_key" to table: "artists"
CREATE UNIQUE INDEX `artists_mbid_key` ON `artists` (`mbid`);
-- create "tracks" table
CREATE TABLE `tracks` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `mbid` text NULL, `title` text NOT NULL, `disc` integer NOT NULL DEFAULT (1), `position` integer NOT NULL, `duration` integer NULL DEFAULT (0), `album_tracks` integer NOT NULL, CONSTRAINT `tracks_albums_tracks` FOREIGN KEY (`album_tracks`) REFERENCES `albums` (`id`) ON DELETE CASCADE);
-- create index "track_album_tracks" to table: "tracks"
CREATE INDEX `track_album_tracks` ON `tracks` (`album_tracks`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
