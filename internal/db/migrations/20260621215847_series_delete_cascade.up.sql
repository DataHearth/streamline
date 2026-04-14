-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_media_files" table
CREATE TABLE `new_media_files` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `path` text NOT NULL, `size` integer NOT NULL, `quality` text NULL, `format` text NULL, `release_group` text NULL, `source` text NOT NULL DEFAULT ('auto'), `last_seen_at` datetime NULL, `episode_media_files` integer NULL, `movie_media_files` integer NULL, CONSTRAINT `media_files_episodes_media_files` FOREIGN KEY (`episode_media_files`) REFERENCES `episodes` (`id`) ON DELETE CASCADE, CONSTRAINT `media_files_movies_media_files` FOREIGN KEY (`movie_media_files`) REFERENCES `movies` (`id`) ON DELETE SET NULL);
-- copy rows from old table "media_files" to new temporary table "new_media_files"
INSERT INTO `new_media_files` (`id`, `create_time`, `update_time`, `path`, `size`, `quality`, `format`, `release_group`, `source`, `last_seen_at`, `episode_media_files`, `movie_media_files`) SELECT `id`, `create_time`, `update_time`, `path`, `size`, `quality`, `format`, `release_group`, `source`, `last_seen_at`, `episode_media_files`, `movie_media_files` FROM `media_files`;
-- drop "media_files" table after copying rows
DROP TABLE `media_files`;
-- rename temporary table "new_media_files" to "media_files"
ALTER TABLE `new_media_files` RENAME TO `media_files`;
-- create "new_download_records" table
CREATE TABLE `new_download_records` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `title` text NOT NULL, `quality` text NULL, `size` integer NULL, `status` text NOT NULL DEFAULT ('downloading'), `torrent_hash` text NULL, `release_group` text NULL, `save_path` text NULL, `import_attempts` integer NOT NULL DEFAULT (0), `failure_reason` text NULL, `imported_at` datetime NULL, `indexer_name` text NULL, `download_client_name` text NULL, `episode_download_records` integer NULL, `movie_download_records` integer NULL, CONSTRAINT `download_records_episodes_download_records` FOREIGN KEY (`episode_download_records`) REFERENCES `episodes` (`id`) ON DELETE CASCADE, CONSTRAINT `download_records_movies_download_records` FOREIGN KEY (`movie_download_records`) REFERENCES `movies` (`id`) ON DELETE SET NULL);
-- copy rows from old table "download_records" to new temporary table "new_download_records"
INSERT INTO `new_download_records` (`id`, `create_time`, `update_time`, `title`, `quality`, `size`, `status`, `torrent_hash`, `release_group`, `save_path`, `import_attempts`, `failure_reason`, `imported_at`, `indexer_name`, `download_client_name`, `episode_download_records`, `movie_download_records`) SELECT `id`, `create_time`, `update_time`, `title`, `quality`, `size`, `status`, `torrent_hash`, `release_group`, `save_path`, `import_attempts`, `failure_reason`, `imported_at`, `indexer_name`, `download_client_name`, `episode_download_records`, `movie_download_records` FROM `download_records`;
-- drop "download_records" table after copying rows
DROP TABLE `download_records`;
-- rename temporary table "new_download_records" to "download_records"
ALTER TABLE `new_download_records` RENAME TO `download_records`;
-- create "new_episodes" table
CREATE TABLE `new_episodes` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `number` integer NOT NULL, `title` text NULL, `air_date` datetime NULL, `monitored` bool NOT NULL DEFAULT (true), `absolute_number` integer NULL DEFAULT (0), `grab_failures` integer NOT NULL DEFAULT (0), `last_search_at` datetime NULL, `status` text NOT NULL DEFAULT ('wanted'), `season_episodes` integer NOT NULL, CONSTRAINT `episodes_seasons_episodes` FOREIGN KEY (`season_episodes`) REFERENCES `seasons` (`id`) ON DELETE CASCADE);
-- copy rows from old table "episodes" to new temporary table "new_episodes"
INSERT INTO `new_episodes` (`id`, `create_time`, `update_time`, `number`, `title`, `air_date`, `monitored`, `absolute_number`, `grab_failures`, `last_search_at`, `status`, `season_episodes`) SELECT `id`, `create_time`, `update_time`, `number`, `title`, `air_date`, `monitored`, `absolute_number`, `grab_failures`, `last_search_at`, `status`, `season_episodes` FROM `episodes`;
-- drop "episodes" table after copying rows
DROP TABLE `episodes`;
-- rename temporary table "new_episodes" to "episodes"
ALTER TABLE `new_episodes` RENAME TO `episodes`;
-- create "new_seasons" table
CREATE TABLE `new_seasons` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `number` integer NOT NULL, `name` text NULL, `monitored` bool NOT NULL DEFAULT (true), `tv_show_seasons` integer NOT NULL, CONSTRAINT `seasons_tv_shows_seasons` FOREIGN KEY (`tv_show_seasons`) REFERENCES `tv_shows` (`id`) ON DELETE CASCADE);
-- copy rows from old table "seasons" to new temporary table "new_seasons"
INSERT INTO `new_seasons` (`id`, `create_time`, `update_time`, `number`, `name`, `monitored`, `tv_show_seasons`) SELECT `id`, `create_time`, `update_time`, `number`, `name`, `monitored`, `tv_show_seasons` FROM `seasons`;
-- drop "seasons" table after copying rows
DROP TABLE `seasons`;
-- rename temporary table "new_seasons" to "seasons"
ALTER TABLE `new_seasons` RENAME TO `seasons`;
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
