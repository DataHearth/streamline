-- reverse: create index "externalid_source_value" to table: "external_ids"
DROP INDEX `externalid_source_value`;
-- reverse: create index "externalid_album_id_source" to table: "external_ids"
DROP INDEX `externalid_album_id_source`;
-- reverse: create index "externalid_artist_id_source" to table: "external_ids"
DROP INDEX `externalid_artist_id_source`;
-- reverse: create index "externalid_tv_show_id_source" to table: "external_ids"
DROP INDEX `externalid_tv_show_id_source`;
-- reverse: create index "externalid_movie_id_source" to table: "external_ids"
DROP INDEX `externalid_movie_id_source`;
-- reverse: create "external_ids" table
DROP TABLE `external_ids`;
