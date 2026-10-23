-- add column "first_aired" to table: "tv_shows"
ALTER TABLE `tv_shows` ADD COLUMN `first_aired` datetime NULL;
-- add column "release_date" to table: "movies"
ALTER TABLE `movies` ADD COLUMN `release_date` datetime NULL;
