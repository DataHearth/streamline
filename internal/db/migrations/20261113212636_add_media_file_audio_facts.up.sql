-- add column "bit_depth" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `bit_depth` integer NULL;
-- add column "sample_rate" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `sample_rate` integer NULL;
