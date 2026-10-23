-- add column "original_date" to table: "albums"
ALTER TABLE `albums` ADD COLUMN `original_date` text NULL;
-- add column "edition_date" to table: "albums"
ALTER TABLE `albums` ADD COLUMN `edition_date` text NULL;
-- add column "genres" to table: "albums"
ALTER TABLE `albums` ADD COLUMN `genres` json NULL;
