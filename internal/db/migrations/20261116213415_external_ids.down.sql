-- reverse: add column "wikidata_id" to table: "movies"
ALTER TABLE `movies` DROP COLUMN `wikidata_id`;
-- reverse: add column "wikidata_id" to table: "tv_shows"
ALTER TABLE `tv_shows` DROP COLUMN `wikidata_id`;
-- reverse: add column "tmdb_id" to table: "tv_shows"
ALTER TABLE `tv_shows` DROP COLUMN `tmdb_id`;
