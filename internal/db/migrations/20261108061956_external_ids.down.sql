-- reverse: create index "externalid_source_value" to table: "external_ids"
DROP INDEX `externalid_source_value`;
-- reverse: create index "externalid_tv_show_external_ids" to table: "external_ids"
DROP INDEX `externalid_tv_show_external_ids`;
-- reverse: create index "externalid_movie_external_ids" to table: "external_ids"
DROP INDEX `externalid_movie_external_ids`;
-- reverse: create index "externalid_source_tv_show_external_ids" to table: "external_ids"
DROP INDEX `externalid_source_tv_show_external_ids`;
-- reverse: create index "externalid_source_movie_external_ids" to table: "external_ids"
DROP INDEX `externalid_source_movie_external_ids`;
-- reverse: create "external_ids" table
DROP TABLE `external_ids`;
