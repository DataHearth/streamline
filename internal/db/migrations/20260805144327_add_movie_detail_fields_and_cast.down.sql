-- reverse: add column "cast" to table: "tv_shows"
ALTER TABLE `tv_shows` DROP COLUMN `cast`;
-- reverse: create index "movie_digital_release_date" to table: "movies"
DROP INDEX `movie_digital_release_date`;
-- reverse: create index "movies_tmdb_id_key" to table: "movies"
DROP INDEX `movies_tmdb_id_key`;
-- reverse: create "new_movies" table
DROP TABLE `new_movies`;
