-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_episodes" table
CREATE TABLE `new_episodes` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `number` integer NOT NULL, `title` text NULL, `air_date` datetime NULL, `monitored` bool NOT NULL DEFAULT (true), `absolute_number` integer NULL DEFAULT (0), `grab_failures` integer NOT NULL DEFAULT (0), `last_search_at` datetime NULL, `status` text NOT NULL DEFAULT ('wanted'), `season_episodes` integer NOT NULL, CONSTRAINT `episodes_seasons_episodes` FOREIGN KEY (`season_episodes`) REFERENCES `seasons` (`id`) ON DELETE NO ACTION);
-- copy rows from old table "episodes" to new temporary table "new_episodes"
INSERT INTO `new_episodes` (`id`, `create_time`, `update_time`, `number`, `title`, `air_date`, `status`, `season_episodes`) SELECT `id`, `create_time`, `update_time`, `number`, `title`, `air_date`, `status`, `season_episodes` FROM `episodes`;
-- drop "episodes" table after copying rows
DROP TABLE `episodes`;
-- rename temporary table "new_episodes" to "episodes"
ALTER TABLE `new_episodes` RENAME TO `episodes`;
-- create "new_seasons" table
CREATE TABLE `new_seasons` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `number` integer NOT NULL, `name` text NULL, `monitored` bool NOT NULL DEFAULT (true), `tv_show_seasons` integer NOT NULL, CONSTRAINT `seasons_tv_shows_seasons` FOREIGN KEY (`tv_show_seasons`) REFERENCES `tv_shows` (`id`) ON DELETE NO ACTION);
-- copy rows from old table "seasons" to new temporary table "new_seasons"
INSERT INTO `new_seasons` (`id`, `create_time`, `update_time`, `number`, `name`, `tv_show_seasons`) SELECT `id`, `create_time`, `update_time`, `number`, `name`, `tv_show_seasons` FROM `seasons`;
-- drop "seasons" table after copying rows
DROP TABLE `seasons`;
-- rename temporary table "new_seasons" to "seasons"
ALTER TABLE `new_seasons` RENAME TO `seasons`;
-- create "new_tv_shows" table
CREATE TABLE `new_tv_shows` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `title` text NOT NULL, `year` integer NOT NULL, `overview` text NULL, `series_status` text NOT NULL DEFAULT ('continuing'), `type` text NOT NULL DEFAULT ('standard'), `monitored` bool NOT NULL DEFAULT (true), `tvdb_id` integer NOT NULL, `poster_path` text NULL, `network` text NULL, `creator` text NULL, `runtime` integer NULL DEFAULT (0), `rating` real NULL DEFAULT (0), `genres` json NULL, `last_refreshed_at` datetime NULL, `quality_profile` text NULL);
-- copy rows from old table "tv_shows" to new temporary table "new_tv_shows"
INSERT INTO `new_tv_shows` (`id`, `create_time`, `update_time`, `title`, `year`, `overview`, `tvdb_id`, `quality_profile`) SELECT `id`, `create_time`, `update_time`, `title`, `year`, `overview`, `tvdb_id`, `quality_profile` FROM `tv_shows`;
-- drop "tv_shows" table after copying rows
DROP TABLE `tv_shows`;
-- rename temporary table "new_tv_shows" to "tv_shows"
ALTER TABLE `new_tv_shows` RENAME TO `tv_shows`;
-- create index "tv_shows_tvdb_id_key" to table: "tv_shows"
CREATE UNIQUE INDEX `tv_shows_tvdb_id_key` ON `tv_shows` (`tvdb_id`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
