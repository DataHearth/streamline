-- reverse: add column "parsed_codec" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `parsed_codec`;
-- reverse: add column "parsed_resolution" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `parsed_resolution`;
-- reverse: add column "parsed_source" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `parsed_source`;
