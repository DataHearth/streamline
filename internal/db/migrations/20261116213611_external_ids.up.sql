-- create "external_ids" table
CREATE TABLE `external_ids` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `source` text NOT NULL, `value` text NOT NULL, `movie_external_ids` integer NULL, `tv_show_external_ids` integer NULL, CONSTRAINT `external_ids_movies_external_ids` FOREIGN KEY (`movie_external_ids`) REFERENCES `movies` (`id`) ON DELETE CASCADE, CONSTRAINT `external_ids_tv_shows_external_ids` FOREIGN KEY (`tv_show_external_ids`) REFERENCES `tv_shows` (`id`) ON DELETE CASCADE);
-- create index "externalid_source_movie_external_ids" to table: "external_ids"
CREATE UNIQUE INDEX `externalid_source_movie_external_ids` ON `external_ids` (`source`, `movie_external_ids`);
-- create index "externalid_source_tv_show_external_ids" to table: "external_ids"
CREATE UNIQUE INDEX `externalid_source_tv_show_external_ids` ON `external_ids` (`source`, `tv_show_external_ids`);
-- create index "externalid_movie_external_ids" to table: "external_ids"
CREATE INDEX `externalid_movie_external_ids` ON `external_ids` (`movie_external_ids`);
-- create index "externalid_tv_show_external_ids" to table: "external_ids"
CREATE INDEX `externalid_tv_show_external_ids` ON `external_ids` (`tv_show_external_ids`);
-- create index "externalid_source_value" to table: "external_ids"
CREATE INDEX `externalid_source_value` ON `external_ids` (`source`, `value`);
