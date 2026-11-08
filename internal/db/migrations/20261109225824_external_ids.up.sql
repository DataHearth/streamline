-- add column "tmdb_id" to table: "tv_shows"
ALTER TABLE `tv_shows` ADD COLUMN `tmdb_id` integer NULL;
-- add column "wikidata_id" to table: "tv_shows"
ALTER TABLE `tv_shows` ADD COLUMN `wikidata_id` text NULL;
-- add column "wikidata_id" to table: "movies"
ALTER TABLE `movies` ADD COLUMN `wikidata_id` text NULL;
