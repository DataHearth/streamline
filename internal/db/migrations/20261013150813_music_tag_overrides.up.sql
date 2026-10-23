-- add column "tag_overrides" to table: "tracks"
ALTER TABLE `tracks` ADD COLUMN `tag_overrides` json NULL;
-- add column "tag_overrides" to table: "albums"
ALTER TABLE `albums` ADD COLUMN `tag_overrides` json NULL;
