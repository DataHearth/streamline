-- add column "parsed_source" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `parsed_source` text NULL;
-- add column "parsed_resolution" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `parsed_resolution` text NULL;
-- add column "parsed_codec" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `parsed_codec` text NULL;
