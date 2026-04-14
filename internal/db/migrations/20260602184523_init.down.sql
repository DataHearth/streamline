-- reverse: create index "users_email_key" to table: "users"
DROP INDEX `users_email_key`;
-- reverse: create "users" table
DROP TABLE `users`;
-- reverse: create index "tv_shows_tvdb_id_key" to table: "tv_shows"
DROP INDEX `tv_shows_tvdb_id_key`;
-- reverse: create "tv_shows" table
DROP TABLE `tv_shows`;
-- reverse: create index "sessions_jti_key" to table: "sessions"
DROP INDEX `sessions_jti_key`;
-- reverse: create "sessions" table
DROP TABLE `sessions`;
-- reverse: create "seasons" table
DROP TABLE `seasons`;
-- reverse: create index "scheduled_jobs_name_key" to table: "scheduled_jobs"
DROP INDEX `scheduled_jobs_name_key`;
-- reverse: create "scheduled_jobs" table
DROP TABLE `scheduled_jobs`;
-- reverse: create "requests" table
DROP TABLE `requests`;
-- reverse: create index "quality_profiles_name_key" to table: "quality_profiles"
DROP INDEX `quality_profiles_name_key`;
-- reverse: create "quality_profiles" table
DROP TABLE `quality_profiles`;
-- reverse: create index "oidcidentity_provider_subject" to table: "oidc_identities"
DROP INDEX `oidcidentity_provider_subject`;
-- reverse: create "oidc_identities" table
DROP TABLE `oidc_identities`;
-- reverse: create index "movieevent_create_time_movie_events" to table: "movie_events"
DROP INDEX `movieevent_create_time_movie_events`;
-- reverse: create index "movieevent_type_create_time" to table: "movie_events"
DROP INDEX `movieevent_type_create_time`;
-- reverse: create index "movieevent_create_time" to table: "movie_events"
DROP INDEX `movieevent_create_time`;
-- reverse: create "movie_events" table
DROP TABLE `movie_events`;
-- reverse: create index "movie_digital_release_date" to table: "movies"
DROP INDEX `movie_digital_release_date`;
-- reverse: create index "movies_tmdb_id_key" to table: "movies"
DROP INDEX `movies_tmdb_id_key`;
-- reverse: create "movies" table
DROP TABLE `movies`;
-- reverse: create index "media_servers_name_key" to table: "media_servers"
DROP INDEX `media_servers_name_key`;
-- reverse: create "media_servers" table
DROP TABLE `media_servers`;
-- reverse: create "media_files" table
DROP TABLE `media_files`;
-- reverse: create index "invites_token_hash_key" to table: "invites"
DROP INDEX `invites_token_hash_key`;
-- reverse: create "invites" table
DROP TABLE `invites`;
-- reverse: create index "indexers_name_key" to table: "indexers"
DROP INDEX `indexers_name_key`;
-- reverse: create "indexers" table
DROP TABLE `indexers`;
-- reverse: create index "importscanfile_decision" to table: "import_scan_files"
DROP INDEX `importscanfile_decision`;
-- reverse: create index "importscanfile_classification" to table: "import_scan_files"
DROP INDEX `importscanfile_classification`;
-- reverse: create "import_scan_files" table
DROP TABLE `import_scan_files`;
-- reverse: create index "importscan_status" to table: "import_scans"
DROP INDEX `importscan_status`;
-- reverse: create "import_scans" table
DROP TABLE `import_scans`;
-- reverse: create "episodes" table
DROP TABLE `episodes`;
-- reverse: create "download_records" table
DROP TABLE `download_records`;
-- reverse: create index "download_clients_name_key" to table: "download_clients"
DROP INDEX `download_clients_name_key`;
-- reverse: create "download_clients" table
DROP TABLE `download_clients`;
-- reverse: create "api_keys" table
DROP TABLE `api_keys`;
