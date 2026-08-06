-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_movies" table
CREATE TABLE `new_movies` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `title` text NOT NULL, `original_title` text NOT NULL, `year` integer NOT NULL, `overview` text NULL, `runtime` integer NULL DEFAULT (0), `status` text NOT NULL DEFAULT ('wanted'), `monitored` bool NOT NULL DEFAULT (true), `tmdb_id` integer NOT NULL, `last_search_at` datetime NULL, `digital_release_date` datetime NULL, `grab_failures` integer NOT NULL DEFAULT (0), `failure_reason` text NULL, `quality_profile` text NULL, `rating` real NULL DEFAULT (0), `genres` json NULL, `cast` json NULL);
-- copy rows from old table "movies" to new temporary table "new_movies"
INSERT INTO `new_movies` (`id`, `create_time`, `update_time`, `title`, `original_title`, `year`, `overview`, `runtime`, `status`, `monitored`, `tmdb_id`, `last_search_at`, `digital_release_date`, `grab_failures`, `failure_reason`, `quality_profile`) SELECT `id`, `create_time`, `update_time`, `title`, `original_title`, `year`, `overview`, `runtime`, `status`, `monitored`, `tmdb_id`, `last_search_at`, `digital_release_date`, `grab_failures`, `failure_reason`, `quality_profile` FROM `movies`;
-- drop "movies" table after copying rows
DROP TABLE `movies`;
-- rename temporary table "new_movies" to "movies"
ALTER TABLE `new_movies` RENAME TO `movies`;
-- create index "movies_tmdb_id_key" to table: "movies"
CREATE UNIQUE INDEX `movies_tmdb_id_key` ON `movies` (`tmdb_id`);
-- create index "movie_digital_release_date" to table: "movies"
CREATE INDEX `movie_digital_release_date` ON `movies` (`digital_release_date`);
-- add column "cast" to table: "tv_shows"
ALTER TABLE `tv_shows` ADD COLUMN `cast` json NULL;
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
