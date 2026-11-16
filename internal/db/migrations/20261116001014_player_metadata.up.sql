-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_credits" table
CREATE TABLE `new_credits` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `character` text NULL, `order` integer NULL DEFAULT (0), `job` text NULL, `department` text NULL, `episode_credits` integer NULL, `movie_credits` integer NULL, `person_credits` integer NOT NULL, `tv_show_credits` integer NULL, CONSTRAINT `credits_episodes_credits` FOREIGN KEY (`episode_credits`) REFERENCES `episodes` (`id`) ON DELETE CASCADE, CONSTRAINT `credits_movies_credits` FOREIGN KEY (`movie_credits`) REFERENCES `movies` (`id`) ON DELETE CASCADE, CONSTRAINT `credits_persons_credits` FOREIGN KEY (`person_credits`) REFERENCES `persons` (`id`) ON DELETE CASCADE, CONSTRAINT `credits_tv_shows_credits` FOREIGN KEY (`tv_show_credits`) REFERENCES `tv_shows` (`id`) ON DELETE CASCADE);
-- copy rows from old table "credits" to new temporary table "new_credits"
INSERT INTO `new_credits` (`id`, `create_time`, `update_time`, `character`, `order`, `movie_credits`, `person_credits`, `tv_show_credits`) SELECT `id`, `create_time`, `update_time`, `character`, `order`, `movie_credits`, `person_credits`, `tv_show_credits` FROM `credits`;
-- drop "credits" table after copying rows
DROP TABLE `credits`;
-- rename temporary table "new_credits" to "credits"
ALTER TABLE `new_credits` RENAME TO `credits`;
-- create index "credit_person_credits" to table: "credits"
CREATE INDEX `credit_person_credits` ON `credits` (`person_credits`);
-- create index "credit_movie_credits" to table: "credits"
CREATE INDEX `credit_movie_credits` ON `credits` (`movie_credits`);
-- create index "credit_tv_show_credits" to table: "credits"
CREATE INDEX `credit_tv_show_credits` ON `credits` (`tv_show_credits`);
-- create index "credit_episode_credits" to table: "credits"
CREATE INDEX `credit_episode_credits` ON `credits` (`episode_credits`);
-- add column "certification" to table: "tv_shows"
ALTER TABLE `tv_shows` ADD COLUMN `certification` text NULL;
-- add column "certification" to table: "movies"
ALTER TABLE `movies` ADD COLUMN `certification` text NULL;
-- create "new_episodes" table
CREATE TABLE `new_episodes` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `number` integer NOT NULL, `title` text NULL, `overview` text NULL, `air_date` datetime NULL, `monitored` bool NOT NULL DEFAULT (true), `monitor_pinned` bool NOT NULL DEFAULT (false), `absolute_number` integer NULL DEFAULT (0), `grab_failures` integer NOT NULL DEFAULT (0), `last_search_at` datetime NULL, `tvdb_id` integer NULL DEFAULT (0), `credits_fetched_at` datetime NULL, `status` text NOT NULL DEFAULT ('wanted'), `season_episodes` integer NOT NULL, CONSTRAINT `episodes_seasons_episodes` FOREIGN KEY (`season_episodes`) REFERENCES `seasons` (`id`) ON DELETE CASCADE);
-- copy rows from old table "episodes" to new temporary table "new_episodes"
INSERT INTO `new_episodes` (`id`, `create_time`, `update_time`, `number`, `title`, `overview`, `air_date`, `monitored`, `monitor_pinned`, `absolute_number`, `grab_failures`, `last_search_at`, `status`, `season_episodes`) SELECT `id`, `create_time`, `update_time`, `number`, `title`, `overview`, `air_date`, `monitored`, `monitor_pinned`, `absolute_number`, `grab_failures`, `last_search_at`, `status`, `season_episodes` FROM `episodes`;
-- drop "episodes" table after copying rows
DROP TABLE `episodes`;
-- rename temporary table "new_episodes" to "episodes"
ALTER TABLE `new_episodes` RENAME TO `episodes`;
-- create index "episode_season_episodes" to table: "episodes"
CREATE INDEX `episode_season_episodes` ON `episodes` (`season_episodes`);
-- create index "episode_status" to table: "episodes"
CREATE INDEX `episode_status` ON `episodes` (`status`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
