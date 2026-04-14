-- reverse: create index "tv_shows_tvdb_id_key" to table: "tv_shows"
DROP INDEX `tv_shows_tvdb_id_key`;
-- reverse: create "new_tv_shows" table
DROP TABLE `new_tv_shows`;
-- reverse: create index "movie_digital_release_date" to table: "movies"
DROP INDEX `movie_digital_release_date`;
-- reverse: create index "movies_tmdb_id_key" to table: "movies"
DROP INDEX `movies_tmdb_id_key`;
-- reverse: create "new_movies" table
DROP TABLE `new_movies`;
-- reverse: create "new_download_records" table
DROP TABLE `new_download_records`;
