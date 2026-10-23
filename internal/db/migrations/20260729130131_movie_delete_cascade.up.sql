-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_download_records" table
CREATE TABLE `new_download_records` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `title` text NOT NULL, `quality` text NULL, `size` integer NULL, `status` text NOT NULL DEFAULT ('downloading'), `torrent_hash` text NULL, `release_group` text NULL, `save_path` text NULL, `import_attempts` integer NOT NULL DEFAULT (0), `failure_reason` text NULL, `imported_at` datetime NULL, `indexer_name` text NULL, `download_client_name` text NULL, `replace_existing` bool NOT NULL DEFAULT (false), `episode_download_records` integer NULL, `movie_download_records` integer NULL, CONSTRAINT `download_records_episodes_download_records` FOREIGN KEY (`episode_download_records`) REFERENCES `episodes` (`id`) ON DELETE CASCADE, CONSTRAINT `download_records_movies_download_records` FOREIGN KEY (`movie_download_records`) REFERENCES `movies` (`id`) ON DELETE CASCADE);
-- copy rows from old table "download_records" to new temporary table "new_download_records"
INSERT INTO `new_download_records` (`id`, `create_time`, `update_time`, `title`, `quality`, `size`, `status`, `torrent_hash`, `release_group`, `save_path`, `import_attempts`, `failure_reason`, `imported_at`, `indexer_name`, `download_client_name`, `replace_existing`, `episode_download_records`, `movie_download_records`) SELECT `id`, `create_time`, `update_time`, `title`, `quality`, `size`, `status`, `torrent_hash`, `release_group`, `save_path`, `import_attempts`, `failure_reason`, `imported_at`, `indexer_name`, `download_client_name`, `replace_existing`, `episode_download_records`, `movie_download_records` FROM `download_records`;
-- drop "download_records" table after copying rows
DROP TABLE `download_records`;
-- rename temporary table "new_download_records" to "download_records"
ALTER TABLE `new_download_records` RENAME TO `download_records`;
-- create "new_media_files" table
CREATE TABLE `new_media_files` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `path` text NOT NULL, `size` integer NOT NULL, `quality` text NULL, `format` text NULL, `release_group` text NULL, `source` text NOT NULL DEFAULT ('auto'), `last_seen_at` datetime NULL, `episode_media_files` integer NULL, `movie_media_files` integer NULL, CONSTRAINT `media_files_episodes_media_files` FOREIGN KEY (`episode_media_files`) REFERENCES `episodes` (`id`) ON DELETE CASCADE, CONSTRAINT `media_files_movies_media_files` FOREIGN KEY (`movie_media_files`) REFERENCES `movies` (`id`) ON DELETE CASCADE);
-- copy rows from old table "media_files" to new temporary table "new_media_files"
INSERT INTO `new_media_files` (`id`, `create_time`, `update_time`, `path`, `size`, `quality`, `format`, `release_group`, `source`, `last_seen_at`, `episode_media_files`, `movie_media_files`) SELECT `id`, `create_time`, `update_time`, `path`, `size`, `quality`, `format`, `release_group`, `source`, `last_seen_at`, `episode_media_files`, `movie_media_files` FROM `media_files`;
-- drop "media_files" table after copying rows
DROP TABLE `media_files`;
-- rename temporary table "new_media_files" to "media_files"
ALTER TABLE `new_media_files` RENAME TO `media_files`;
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
