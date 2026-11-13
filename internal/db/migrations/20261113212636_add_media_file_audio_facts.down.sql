-- reverse: add column "sample_rate" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `sample_rate`;
-- reverse: add column "bit_depth" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `bit_depth`;
