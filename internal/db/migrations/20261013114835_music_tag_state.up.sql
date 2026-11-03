-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_media_files" table
CREATE TABLE `new_media_files` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `path` text NOT NULL, `size` integer NOT NULL, `quality` text NULL, `format` text NULL, `release_group` text NULL, `source` text NOT NULL DEFAULT ('auto'), `last_seen_at` datetime NULL, `missing_since` datetime NULL, `container` text NULL, `duration_seconds` integer NULL, `video_codec` text NULL, `width` integer NULL, `height` integer NULL, `audio_codec` text NULL, `audio_channels` integer NULL, `bitrate` integer NULL, `audio_tracks` integer NULL, `audio_langs` text NULL, `sub_langs` text NULL, `probed_at` datetime NULL, `parsed_source` text NULL, `parsed_resolution` text NULL, `parsed_codec` text NULL, `transcoded_at` datetime NULL, `size_before` integer NULL, `book_kind` text NULL, `tags_state` text NULL, `tags_hash` text NULL, `tagged_at` datetime NULL, `tags_mtime` integer NULL, `tags_size` integer NULL, `tags_checked_at` datetime NULL, `tag_failures` integer NOT NULL DEFAULT (0), `tags_error` text NULL, `book_media_files` integer NULL, `episode_media_files` integer NULL, `movie_media_files` integer NULL, `track_media_files` integer NULL, CONSTRAINT `media_files_books_media_files` FOREIGN KEY (`book_media_files`) REFERENCES `books` (`id`) ON DELETE CASCADE, CONSTRAINT `media_files_episodes_media_files` FOREIGN KEY (`episode_media_files`) REFERENCES `episodes` (`id`) ON DELETE CASCADE, CONSTRAINT `media_files_movies_media_files` FOREIGN KEY (`movie_media_files`) REFERENCES `movies` (`id`) ON DELETE CASCADE, CONSTRAINT `media_files_tracks_media_files` FOREIGN KEY (`track_media_files`) REFERENCES `tracks` (`id`) ON DELETE CASCADE);
-- copy rows from old table "media_files" to new temporary table "new_media_files"
INSERT INTO `new_media_files` (`id`, `create_time`, `update_time`, `path`, `size`, `quality`, `format`, `release_group`, `source`, `last_seen_at`, `missing_since`, `container`, `duration_seconds`, `video_codec`, `width`, `height`, `audio_codec`, `audio_channels`, `bitrate`, `audio_tracks`, `audio_langs`, `sub_langs`, `probed_at`, `parsed_source`, `parsed_resolution`, `parsed_codec`, `transcoded_at`, `size_before`, `book_kind`, `book_media_files`, `episode_media_files`, `movie_media_files`, `track_media_files`) SELECT `id`, `create_time`, `update_time`, `path`, `size`, `quality`, `format`, `release_group`, `source`, `last_seen_at`, `missing_since`, `container`, `duration_seconds`, `video_codec`, `width`, `height`, `audio_codec`, `audio_channels`, `bitrate`, `audio_tracks`, `audio_langs`, `sub_langs`, `probed_at`, `parsed_source`, `parsed_resolution`, `parsed_codec`, `transcoded_at`, `size_before`, `book_kind`, `book_media_files`, `episode_media_files`, `movie_media_files`, `track_media_files` FROM `media_files`;
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
-- create index "mediafile_book_media_files" to table: "media_files"
CREATE INDEX `mediafile_book_media_files` ON `media_files` (`book_media_files`);
-- create index "mediafile_tags_state" to table: "media_files"
CREATE INDEX `mediafile_tags_state` ON `media_files` (`tags_state`);
-- create index "mediafile_probed_at" to table: "media_files"
CREATE INDEX `mediafile_probed_at` ON `media_files` (`probed_at`) WHERE probed_at IS NULL;
-- create index "mediafile_create_time" to table: "media_files"
CREATE INDEX `mediafile_create_time` ON `media_files` (`create_time`);
-- add column "release_track_mbid" to table: "tracks"
ALTER TABLE `tracks` ADD COLUMN `release_track_mbid` text NULL;
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
