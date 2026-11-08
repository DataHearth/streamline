-- add column "imdb_id" to table: "tv_shows"
ALTER TABLE `tv_shows` ADD COLUMN `imdb_id` text NULL;
-- add column "imdb_id" to table: "movies"
ALTER TABLE `movies` ADD COLUMN `imdb_id` text NULL;
