-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_download_records" table
CREATE TABLE `new_download_records` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `title` text NOT NULL, `quality` text NULL, `size` integer NULL, `status` text NOT NULL DEFAULT ('downloading'), `torrent_hash` text NULL, `release_group` text NULL, `save_path` text NULL, `import_attempts` integer NOT NULL DEFAULT (0), `failure_reason` text NULL, `imported_at` datetime NULL, `indexer_name` text NULL, `download_client_name` text NULL, `episode_download_records` integer NULL, `movie_download_records` integer NULL, CONSTRAINT `download_records_episodes_download_records` FOREIGN KEY (`episode_download_records`) REFERENCES `episodes` (`id`) ON DELETE SET NULL, CONSTRAINT `download_records_movies_download_records` FOREIGN KEY (`movie_download_records`) REFERENCES `movies` (`id`) ON DELETE SET NULL);
-- copy rows from old table "download_records" to new temporary table "new_download_records"
INSERT INTO `new_download_records` (`id`, `create_time`, `update_time`, `title`, `quality`, `size`, `status`, `torrent_hash`, `release_group`, `save_path`, `import_attempts`, `failure_reason`, `imported_at`, `episode_download_records`, `movie_download_records`) SELECT `id`, `create_time`, `update_time`, `title`, `quality`, `size`, `status`, `torrent_hash`, `release_group`, `save_path`, `import_attempts`, `failure_reason`, `imported_at`, `episode_download_records`, `movie_download_records` FROM `download_records`;
-- drop "download_records" table after copying rows
DROP TABLE `download_records`;
-- rename temporary table "new_download_records" to "download_records"
ALTER TABLE `new_download_records` RENAME TO `download_records`;
-- create "new_movies" table
CREATE TABLE `new_movies` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `title` text NOT NULL, `original_title` text NOT NULL, `year` integer NOT NULL, `overview` text NULL, `runtime` integer NULL DEFAULT (0), `status` text NOT NULL DEFAULT ('wanted'), `monitored` bool NOT NULL DEFAULT (true), `tmdb_id` integer NOT NULL, `last_search_at` datetime NULL, `digital_release_date` datetime NULL, `grab_failures` integer NOT NULL DEFAULT (0), `failure_reason` text NULL, `quality_profile` text NULL);
-- copy rows from old table "movies" to new temporary table "new_movies"
INSERT INTO `new_movies` (`id`, `create_time`, `update_time`, `title`, `original_title`, `year`, `overview`, `runtime`, `status`, `monitored`, `tmdb_id`, `last_search_at`, `digital_release_date`, `grab_failures`, `failure_reason`) SELECT `id`, `create_time`, `update_time`, `title`, `original_title`, `year`, `overview`, `runtime`, `status`, `monitored`, `tmdb_id`, `last_search_at`, `digital_release_date`, `grab_failures`, `failure_reason` FROM `movies`;
-- drop "movies" table after copying rows
DROP TABLE `movies`;
-- rename temporary table "new_movies" to "movies"
ALTER TABLE `new_movies` RENAME TO `movies`;
-- create index "movies_tmdb_id_key" to table: "movies"
CREATE UNIQUE INDEX `movies_tmdb_id_key` ON `movies` (`tmdb_id`);
-- create index "movie_digital_release_date" to table: "movies"
CREATE INDEX `movie_digital_release_date` ON `movies` (`digital_release_date`);
-- create "new_tv_shows" table
CREATE TABLE `new_tv_shows` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `title` text NOT NULL, `year` integer NOT NULL, `overview` text NULL, `status` text NOT NULL DEFAULT ('wanted'), `tvdb_id` integer NOT NULL, `quality_profile` text NULL);
-- copy rows from old table "tv_shows" to new temporary table "new_tv_shows"
INSERT INTO `new_tv_shows` (`id`, `create_time`, `update_time`, `title`, `year`, `overview`, `status`, `tvdb_id`) SELECT `id`, `create_time`, `update_time`, `title`, `year`, `overview`, `status`, `tvdb_id` FROM `tv_shows`;
-- drop "tv_shows" table after copying rows
DROP TABLE `tv_shows`;
-- rename temporary table "new_tv_shows" to "tv_shows"
ALTER TABLE `new_tv_shows` RENAME TO `tv_shows`;
-- create index "tv_shows_tvdb_id_key" to table: "tv_shows"
CREATE UNIQUE INDEX `tv_shows_tvdb_id_key` ON `tv_shows` (`tvdb_id`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
