-- reverse: add column "aliases" to table: "artists"
ALTER TABLE `artists` DROP COLUMN `aliases`;
-- reverse: add column "aliases" to table: "tracks"
ALTER TABLE `tracks` DROP COLUMN `aliases`;
