-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_episodes" table
CREATE TABLE `new_episodes` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `number` integer NOT NULL, `title` text NULL, `overview` text NULL, `air_date` datetime NULL, `monitored` bool NOT NULL DEFAULT (true), `monitor_pinned` bool NOT NULL DEFAULT (false), `absolute_number` integer NULL DEFAULT (0), `grab_failures` integer NOT NULL DEFAULT (0), `last_search_at` datetime NULL, `status` text NOT NULL DEFAULT ('wanted'), `season_episodes` integer NOT NULL, CONSTRAINT `episodes_seasons_episodes` FOREIGN KEY (`season_episodes`) REFERENCES `seasons` (`id`) ON DELETE CASCADE);
-- copy rows from old table "episodes" to new temporary table "new_episodes"
INSERT INTO `new_episodes` (`id`, `create_time`, `update_time`, `number`, `title`, `overview`, `air_date`, `monitored`, `absolute_number`, `grab_failures`, `last_search_at`, `status`, `season_episodes`) SELECT `id`, `create_time`, `update_time`, `number`, `title`, `overview`, `air_date`, `monitored`, `absolute_number`, `grab_failures`, `last_search_at`, `status`, `season_episodes` FROM `episodes`;
-- drop "episodes" table after copying rows
DROP TABLE `episodes`;
-- rename temporary table "new_episodes" to "episodes"
ALTER TABLE `new_episodes` RENAME TO `episodes`;
-- create index "episode_season_episodes" to table: "episodes"
CREATE INDEX `episode_season_episodes` ON `episodes` (`season_episodes`);
-- create index "episode_status" to table: "episodes"
CREATE INDEX `episode_status` ON `episodes` (`status`);
-- create "new_seasons" table
CREATE TABLE `new_seasons` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `number` integer NOT NULL, `name` text NULL, `monitored` bool NOT NULL DEFAULT (true), `monitor_pinned` bool NOT NULL DEFAULT (false), `tv_show_seasons` integer NOT NULL, CONSTRAINT `seasons_tv_shows_seasons` FOREIGN KEY (`tv_show_seasons`) REFERENCES `tv_shows` (`id`) ON DELETE CASCADE);
-- copy rows from old table "seasons" to new temporary table "new_seasons"
INSERT INTO `new_seasons` (`id`, `create_time`, `update_time`, `number`, `name`, `monitored`, `tv_show_seasons`) SELECT `id`, `create_time`, `update_time`, `number`, `name`, `monitored`, `tv_show_seasons` FROM `seasons`;
-- drop "seasons" table after copying rows
DROP TABLE `seasons`;
-- rename temporary table "new_seasons" to "seasons"
ALTER TABLE `new_seasons` RENAME TO `seasons`;
-- create index "season_tv_show_seasons" to table: "seasons"
CREATE INDEX `season_tv_show_seasons` ON `seasons` (`tv_show_seasons`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
