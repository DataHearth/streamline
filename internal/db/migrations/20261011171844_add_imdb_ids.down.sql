-- reverse: add column "imdb_id" to table: "movies"
ALTER TABLE `movies` DROP COLUMN `imdb_id`;
-- reverse: add column "imdb_id" to table: "tv_shows"
ALTER TABLE `tv_shows` DROP COLUMN `imdb_id`;
