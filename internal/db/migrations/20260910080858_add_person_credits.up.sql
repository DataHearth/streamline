-- create "credits" table
CREATE TABLE `credits` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `character` text NULL, `order` integer NULL DEFAULT (0), `movie_credits` integer NULL, `person_credits` integer NOT NULL, `tv_show_credits` integer NULL, CONSTRAINT `credits_movies_credits` FOREIGN KEY (`movie_credits`) REFERENCES `movies` (`id`) ON DELETE CASCADE, CONSTRAINT `credits_persons_credits` FOREIGN KEY (`person_credits`) REFERENCES `persons` (`id`) ON DELETE CASCADE, CONSTRAINT `credits_tv_shows_credits` FOREIGN KEY (`tv_show_credits`) REFERENCES `tv_shows` (`id`) ON DELETE CASCADE);
-- create index "credit_person_credits" to table: "credits"
CREATE INDEX `credit_person_credits` ON `credits` (`person_credits`);
-- create index "credit_movie_credits" to table: "credits"
CREATE INDEX `credit_movie_credits` ON `credits` (`movie_credits`);
-- create index "credit_tv_show_credits" to table: "credits"
CREATE INDEX `credit_tv_show_credits` ON `credits` (`tv_show_credits`);
-- create "persons" table
CREATE TABLE `persons` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `tmdb_id` integer NULL DEFAULT (0), `tvdb_id` integer NULL DEFAULT (0), `name` text NOT NULL, `profile_url` text NULL);
-- create index "person_tmdb_id" to table: "persons"
CREATE INDEX `person_tmdb_id` ON `persons` (`tmdb_id`);
-- create index "person_tvdb_id" to table: "persons"
CREATE INDEX `person_tvdb_id` ON `persons` (`tvdb_id`);
-- create index "person_name" to table: "persons"
CREATE INDEX `person_name` ON `persons` (`name`);
