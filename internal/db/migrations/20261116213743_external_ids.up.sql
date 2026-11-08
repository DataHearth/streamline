-- create "external_ids" table
CREATE TABLE `external_ids` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `source` text NOT NULL, `value` text NOT NULL, `album_id` integer NULL, `artist_id` integer NULL, `movie_id` integer NULL, `tv_show_id` integer NULL, CONSTRAINT `external_ids_albums_external_ids` FOREIGN KEY (`album_id`) REFERENCES `albums` (`id`) ON DELETE CASCADE, CONSTRAINT `external_ids_artists_external_ids` FOREIGN KEY (`artist_id`) REFERENCES `artists` (`id`) ON DELETE CASCADE, CONSTRAINT `external_ids_movies_external_ids` FOREIGN KEY (`movie_id`) REFERENCES `movies` (`id`) ON DELETE CASCADE, CONSTRAINT `external_ids_tv_shows_external_ids` FOREIGN KEY (`tv_show_id`) REFERENCES `tv_shows` (`id`) ON DELETE CASCADE);
-- create index "externalid_movie_id_source" to table: "external_ids"
CREATE UNIQUE INDEX `externalid_movie_id_source` ON `external_ids` (`movie_id`, `source`);
-- create index "externalid_tv_show_id_source" to table: "external_ids"
CREATE UNIQUE INDEX `externalid_tv_show_id_source` ON `external_ids` (`tv_show_id`, `source`);
-- create index "externalid_artist_id_source" to table: "external_ids"
CREATE UNIQUE INDEX `externalid_artist_id_source` ON `external_ids` (`artist_id`, `source`);
-- create index "externalid_album_id_source" to table: "external_ids"
CREATE UNIQUE INDEX `externalid_album_id_source` ON `external_ids` (`album_id`, `source`);
-- create index "externalid_source_value" to table: "external_ids"
CREATE INDEX `externalid_source_value` ON `external_ids` (`source`, `value`);
