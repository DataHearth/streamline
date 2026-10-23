-- reverse: add column "tag_overrides" to table: "albums"
ALTER TABLE `albums` DROP COLUMN `tag_overrides`;
-- reverse: add column "tag_overrides" to table: "tracks"
ALTER TABLE `tracks` DROP COLUMN `tag_overrides`;
