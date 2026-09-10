-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_tv_shows" table
CREATE TABLE `new_tv_shows` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `title` text NOT NULL, `original_title` text NULL, `year` integer NOT NULL, `first_aired` datetime NULL, `overview` text NULL, `series_status` text NOT NULL DEFAULT ('continuing'), `type` text NOT NULL DEFAULT ('standard'), `monitored` bool NOT NULL DEFAULT (true), `tvdb_id` integer NOT NULL, `poster_path` text NULL, `network` text NULL, `creator` text NULL, `runtime` integer NULL DEFAULT (0), `rating` real NULL DEFAULT (0), `genres` json NULL, `last_refreshed_at` datetime NULL, `quality_profile` text NULL);
-- copy rows from old table "tv_shows" to new temporary table "new_tv_shows"
INSERT INTO `new_tv_shows` (`id`, `create_time`, `update_time`, `title`, `original_title`, `year`, `first_aired`, `overview`, `series_status`, `type`, `monitored`, `tvdb_id`, `poster_path`, `network`, `creator`, `runtime`, `rating`, `genres`, `last_refreshed_at`, `quality_profile`) SELECT `id`, `create_time`, `update_time`, `title`, `original_title`, `year`, `first_aired`, `overview`, `series_status`, `type`, `monitored`, `tvdb_id`, `poster_path`, `network`, `creator`, `runtime`, `rating`, `genres`, `last_refreshed_at`, `quality_profile` FROM `tv_shows`;
-- drop "tv_shows" table after copying rows
DROP TABLE `tv_shows`;
-- rename temporary table "new_tv_shows" to "tv_shows"
ALTER TABLE `new_tv_shows` RENAME TO `tv_shows`;
-- create index "tv_shows_tvdb_id_key" to table: "tv_shows"
CREATE UNIQUE INDEX `tv_shows_tvdb_id_key` ON `tv_shows` (`tvdb_id`);
-- create index "tvshow_create_time" to table: "tv_shows"
CREATE INDEX `tvshow_create_time` ON `tv_shows` (`create_time`);
-- create index "tvshow_series_status" to table: "tv_shows"
CREATE INDEX `tvshow_series_status` ON `tv_shows` (`series_status`);
-- create "new_movies" table
CREATE TABLE `new_movies` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `title` text NOT NULL, `original_title` text NOT NULL, `year` integer NOT NULL, `overview` text NULL, `runtime` integer NULL DEFAULT (0), `status` text NOT NULL DEFAULT ('wanted'), `monitored` bool NOT NULL DEFAULT (true), `tmdb_id` integer NOT NULL, `last_search_at` datetime NULL, `release_date` datetime NULL, `digital_release_date` datetime NULL, `grab_failures` integer NOT NULL DEFAULT (0), `failure_reason` text NULL, `quality_profile` text NULL, `rating` real NULL DEFAULT (0), `genres` json NULL, `last_refreshed_at` datetime NULL);
-- copy rows from old table "movies" to new temporary table "new_movies"
INSERT INTO `new_movies` (`id`, `create_time`, `update_time`, `title`, `original_title`, `year`, `overview`, `runtime`, `status`, `monitored`, `tmdb_id`, `last_search_at`, `release_date`, `digital_release_date`, `grab_failures`, `failure_reason`, `quality_profile`, `rating`, `genres`, `last_refreshed_at`) SELECT `id`, `create_time`, `update_time`, `title`, `original_title`, `year`, `overview`, `runtime`, `status`, `monitored`, `tmdb_id`, `last_search_at`, `release_date`, `digital_release_date`, `grab_failures`, `failure_reason`, `quality_profile`, `rating`, `genres`, `last_refreshed_at` FROM `movies`;
-- drop "movies" table after copying rows
DROP TABLE `movies`;
-- rename temporary table "new_movies" to "movies"
ALTER TABLE `new_movies` RENAME TO `movies`;
-- create index "movies_tmdb_id_key" to table: "movies"
CREATE UNIQUE INDEX `movies_tmdb_id_key` ON `movies` (`tmdb_id`);
-- create index "movie_digital_release_date" to table: "movies"
CREATE INDEX `movie_digital_release_date` ON `movies` (`digital_release_date`);
-- create index "movie_status" to table: "movies"
CREATE INDEX `movie_status` ON `movies` (`status`);
-- create index "movie_create_time" to table: "movies"
CREATE INDEX `movie_create_time` ON `movies` (`create_time`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
