-- reverse: create index "person_name" to table: "persons"
DROP INDEX `person_name`;
-- reverse: create index "person_tvdb_id" to table: "persons"
DROP INDEX `person_tvdb_id`;
-- reverse: create index "person_tmdb_id" to table: "persons"
DROP INDEX `person_tmdb_id`;
-- reverse: create "persons" table
DROP TABLE `persons`;
-- reverse: create index "credit_tv_show_credits" to table: "credits"
DROP INDEX `credit_tv_show_credits`;
-- reverse: create index "credit_movie_credits" to table: "credits"
DROP INDEX `credit_movie_credits`;
-- reverse: create index "credit_person_credits" to table: "credits"
DROP INDEX `credit_person_credits`;
-- reverse: create "credits" table
DROP TABLE `credits`;
