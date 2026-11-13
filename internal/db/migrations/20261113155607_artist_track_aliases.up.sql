-- add column "aliases" to table: "tracks"
ALTER TABLE `tracks` ADD COLUMN `aliases` json NULL;
-- add column "aliases" to table: "artists"
ALTER TABLE `artists` ADD COLUMN `aliases` json NULL;
