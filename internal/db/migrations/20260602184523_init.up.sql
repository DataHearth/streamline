-- create "api_keys" table
CREATE TABLE `api_keys` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `name` text NOT NULL, `key_hash` text NOT NULL, `last_used_at` datetime NULL, `user_api_keys` integer NOT NULL, CONSTRAINT `api_keys_users_api_keys` FOREIGN KEY (`user_api_keys`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create "download_clients" table
CREATE TABLE `download_clients` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `name` text NOT NULL, `client_type` text NOT NULL, `host` text NOT NULL, `port` integer NOT NULL, `auth_method` text NOT NULL DEFAULT ('password'), `username` text NULL, `password` text NULL, `api_key` text NULL, `use_ssl` bool NOT NULL DEFAULT (false), `priority` integer NOT NULL DEFAULT (0), `enabled` bool NOT NULL DEFAULT (true));
-- create index "download_clients_name_key" to table: "download_clients"
CREATE UNIQUE INDEX `download_clients_name_key` ON `download_clients` (`name`);
-- create "download_records" table
CREATE TABLE `download_records` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `title` text NOT NULL, `quality` text NULL, `size` integer NULL, `status` text NOT NULL DEFAULT ('downloading'), `torrent_hash` text NULL, `release_group` text NULL, `save_path` text NULL, `import_attempts` integer NOT NULL DEFAULT (0), `failure_reason` text NULL, `imported_at` datetime NULL, `download_record_indexer` integer NULL, `download_record_download_client` integer NULL, `episode_download_records` integer NULL, `movie_download_records` integer NULL, CONSTRAINT `download_records_indexers_indexer` FOREIGN KEY (`download_record_indexer`) REFERENCES `indexers` (`id`) ON DELETE SET NULL, CONSTRAINT `download_records_download_clients_download_client` FOREIGN KEY (`download_record_download_client`) REFERENCES `download_clients` (`id`) ON DELETE SET NULL, CONSTRAINT `download_records_episodes_download_records` FOREIGN KEY (`episode_download_records`) REFERENCES `episodes` (`id`) ON DELETE SET NULL, CONSTRAINT `download_records_movies_download_records` FOREIGN KEY (`movie_download_records`) REFERENCES `movies` (`id`) ON DELETE SET NULL);
-- create "episodes" table
CREATE TABLE `episodes` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `number` integer NOT NULL, `title` text NULL, `air_date` datetime NULL, `status` text NOT NULL DEFAULT ('wanted'), `season_episodes` integer NOT NULL, CONSTRAINT `episodes_seasons_episodes` FOREIGN KEY (`season_episodes`) REFERENCES `seasons` (`id`) ON DELETE NO ACTION);
-- create "import_scans" table
CREATE TABLE `import_scans` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `source_path` text NOT NULL, `mode` text NOT NULL, `import_mode` text NULL, `status` text NOT NULL DEFAULT ('running'), `total_count` integer NOT NULL DEFAULT (0), `processed_count` integer NOT NULL DEFAULT (0), `commit_success_count` integer NOT NULL DEFAULT (0), `commit_failed_count` integer NOT NULL DEFAULT (0), `failure_reason` text NULL, `scanned_at` datetime NULL, `committed_at` datetime NULL);
-- create index "importscan_status" to table: "import_scans"
CREATE INDEX `importscan_status` ON `import_scans` (`status`);
-- create "import_scan_files" table
CREATE TABLE `import_scan_files` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `source_path` text NOT NULL, `size` integer NOT NULL, `parsed_title` text NULL, `parsed_year` integer NULL, `parsed_quality` text NULL, `parsed_release_group` text NULL, `classification` text NOT NULL DEFAULT ('unmatched'), `candidates` json NULL, `tmdb_id` integer NULL, `existing_movie_id` integer NULL, `decision` text NOT NULL DEFAULT ('pending'), `decision_tmdb_id` integer NULL, `outcome` text NOT NULL DEFAULT ('pending'), `outcome_message` text NULL, `created_movie_id` integer NULL, `import_scan_files` integer NOT NULL, CONSTRAINT `import_scan_files_import_scans_files` FOREIGN KEY (`import_scan_files`) REFERENCES `import_scans` (`id`) ON DELETE CASCADE);
-- create index "importscanfile_classification" to table: "import_scan_files"
CREATE INDEX `importscanfile_classification` ON `import_scan_files` (`classification`);
-- create index "importscanfile_decision" to table: "import_scan_files"
CREATE INDEX `importscanfile_decision` ON `import_scan_files` (`decision`);
-- create "indexers" table
CREATE TABLE `indexers` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `name` text NOT NULL, `host` text NOT NULL, `port` integer NOT NULL, `path` text NULL DEFAULT (''), `use_ssl` bool NOT NULL DEFAULT (false), `api_key` text NOT NULL, `protocol` text NOT NULL, `priority` integer NOT NULL DEFAULT (0), `enabled` bool NOT NULL DEFAULT (true));
-- create index "indexers_name_key" to table: "indexers"
CREATE UNIQUE INDEX `indexers_name_key` ON `indexers` (`name`);
-- create "invites" table
CREATE TABLE `invites` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `token_hash` text NOT NULL, `email` text NULL, `role` text NOT NULL DEFAULT ('member'), `expires_at` datetime NOT NULL, `used_at` datetime NULL, `invite_created_by` integer NOT NULL, `invite_used_by` integer NULL, CONSTRAINT `invites_users_created_by` FOREIGN KEY (`invite_created_by`) REFERENCES `users` (`id`) ON DELETE NO ACTION, CONSTRAINT `invites_users_used_by` FOREIGN KEY (`invite_used_by`) REFERENCES `users` (`id`) ON DELETE SET NULL);
-- create index "invites_token_hash_key" to table: "invites"
CREATE UNIQUE INDEX `invites_token_hash_key` ON `invites` (`token_hash`);
-- create "media_files" table
CREATE TABLE `media_files` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `path` text NOT NULL, `size` integer NOT NULL, `quality` text NULL, `format` text NULL, `release_group` text NULL, `source` text NOT NULL DEFAULT ('auto'), `last_seen_at` datetime NULL, `episode_media_files` integer NULL, `movie_media_files` integer NULL, CONSTRAINT `media_files_episodes_media_files` FOREIGN KEY (`episode_media_files`) REFERENCES `episodes` (`id`) ON DELETE SET NULL, CONSTRAINT `media_files_movies_media_files` FOREIGN KEY (`movie_media_files`) REFERENCES `movies` (`id`) ON DELETE SET NULL);
-- create "media_servers" table
CREATE TABLE `media_servers` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `name` text NOT NULL, `server_type` text NOT NULL, `host` text NOT NULL, `api_key` text NOT NULL, `enabled` bool NOT NULL DEFAULT (true), `library_section` text NULL);
-- create index "media_servers_name_key" to table: "media_servers"
CREATE UNIQUE INDEX `media_servers_name_key` ON `media_servers` (`name`);
-- create "movies" table
CREATE TABLE `movies` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `title` text NOT NULL, `original_title` text NOT NULL, `year` integer NOT NULL, `overview` text NULL, `runtime` integer NULL DEFAULT (0), `status` text NOT NULL DEFAULT ('wanted'), `monitored` bool NOT NULL DEFAULT (true), `tmdb_id` integer NOT NULL, `last_search_at` datetime NULL, `digital_release_date` datetime NULL, `grab_failures` integer NOT NULL DEFAULT (0), `failure_reason` text NULL, `quality_profile_movies` integer NULL, CONSTRAINT `movies_quality_profiles_movies` FOREIGN KEY (`quality_profile_movies`) REFERENCES `quality_profiles` (`id`) ON DELETE SET NULL);
-- create index "movies_tmdb_id_key" to table: "movies"
CREATE UNIQUE INDEX `movies_tmdb_id_key` ON `movies` (`tmdb_id`);
-- create index "movie_digital_release_date" to table: "movies"
CREATE INDEX `movie_digital_release_date` ON `movies` (`digital_release_date`);
-- create "movie_events" table
CREATE TABLE `movie_events` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `type` text NOT NULL, `payload` json NULL, `movie_events` integer NOT NULL, CONSTRAINT `movie_events_movies_events` FOREIGN KEY (`movie_events`) REFERENCES `movies` (`id`) ON DELETE CASCADE);
-- create index "movieevent_create_time" to table: "movie_events"
CREATE INDEX `movieevent_create_time` ON `movie_events` (`create_time`);
-- create index "movieevent_type_create_time" to table: "movie_events"
CREATE INDEX `movieevent_type_create_time` ON `movie_events` (`type`, `create_time`);
-- create index "movieevent_create_time_movie_events" to table: "movie_events"
CREATE INDEX `movieevent_create_time_movie_events` ON `movie_events` (`create_time`, `movie_events`);
-- create "oidc_identities" table
CREATE TABLE `oidc_identities` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `provider` text NOT NULL, `subject` text NOT NULL, `email` text NULL, `user_oidc_identities` integer NOT NULL, CONSTRAINT `oidc_identities_users_oidc_identities` FOREIGN KEY (`user_oidc_identities`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create index "oidcidentity_provider_subject" to table: "oidc_identities"
CREATE UNIQUE INDEX `oidcidentity_provider_subject` ON `oidc_identities` (`provider`, `subject`);
-- create "quality_profiles" table
CREATE TABLE `quality_profiles` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `name` text NOT NULL, `preferred_resolution` text NOT NULL, `min_resolution` text NOT NULL DEFAULT ('720p'), `upgrade_allowed` bool NOT NULL DEFAULT (true));
-- create index "quality_profiles_name_key" to table: "quality_profiles"
CREATE UNIQUE INDEX `quality_profiles_name_key` ON `quality_profiles` (`name`);
-- create "requests" table
CREATE TABLE `requests` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `media_type` text NOT NULL, `media_id` integer NOT NULL, `title` text NOT NULL, `status` text NOT NULL DEFAULT ('pending'), `request_approved_by` integer NULL, `user_requests` integer NOT NULL, CONSTRAINT `requests_users_approved_by` FOREIGN KEY (`request_approved_by`) REFERENCES `users` (`id`) ON DELETE SET NULL, CONSTRAINT `requests_users_requests` FOREIGN KEY (`user_requests`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create "scheduled_jobs" table
CREATE TABLE `scheduled_jobs` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `name` text NOT NULL, `paused` bool NOT NULL DEFAULT (false), `last_started_at` datetime NULL, `last_finished_at` datetime NULL, `last_status` text NOT NULL DEFAULT ('never'), `last_error` text NULL, `last_duration_ms` integer NOT NULL DEFAULT (0));
-- create index "scheduled_jobs_name_key" to table: "scheduled_jobs"
CREATE UNIQUE INDEX `scheduled_jobs_name_key` ON `scheduled_jobs` (`name`);
-- create "seasons" table
CREATE TABLE `seasons` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `number` integer NOT NULL, `name` text NULL, `tv_show_seasons` integer NOT NULL, CONSTRAINT `seasons_tv_shows_seasons` FOREIGN KEY (`tv_show_seasons`) REFERENCES `tv_shows` (`id`) ON DELETE NO ACTION);
-- create "sessions" table
CREATE TABLE `sessions` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `jti` text NOT NULL, `expires_at` datetime NOT NULL, `revoked_at` datetime NULL, `last_seen_at` datetime NULL, `ip` text NULL, `user_agent` text NULL, `user_sessions` integer NOT NULL, CONSTRAINT `sessions_users_sessions` FOREIGN KEY (`user_sessions`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create index "sessions_jti_key" to table: "sessions"
CREATE UNIQUE INDEX `sessions_jti_key` ON `sessions` (`jti`);
-- create "tv_shows" table
CREATE TABLE `tv_shows` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `title` text NOT NULL, `year` integer NOT NULL, `overview` text NULL, `status` text NOT NULL DEFAULT ('wanted'), `tvdb_id` integer NOT NULL, `quality_profile_tv_shows` integer NULL, CONSTRAINT `tv_shows_quality_profiles_tv_shows` FOREIGN KEY (`quality_profile_tv_shows`) REFERENCES `quality_profiles` (`id`) ON DELETE SET NULL);
-- create index "tv_shows_tvdb_id_key" to table: "tv_shows"
CREATE UNIQUE INDEX `tv_shows_tvdb_id_key` ON `tv_shows` (`tvdb_id`);
-- create "users" table
CREATE TABLE `users` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `email` text NOT NULL, `password_hash` text NULL, `role` text NOT NULL DEFAULT ('member'), `auth_method` text NOT NULL DEFAULT ('local'), `display_name` text NULL, `failed_login_count` integer NOT NULL DEFAULT (0), `last_failed_login_at` datetime NULL, `locked_until` datetime NULL);
-- create index "users_email_key" to table: "users"
CREATE UNIQUE INDEX `users_email_key` ON `users` (`email`);
