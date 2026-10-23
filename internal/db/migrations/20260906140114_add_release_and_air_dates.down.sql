-- reverse: add column "release_date" to table: "movies"
ALTER TABLE `movies` DROP COLUMN `release_date`;
-- reverse: add column "first_aired" to table: "tv_shows"
ALTER TABLE `tv_shows` DROP COLUMN `first_aired`;
