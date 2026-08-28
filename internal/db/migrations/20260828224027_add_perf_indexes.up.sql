-- create index "apikey_key_hash" to table: "api_keys"
CREATE UNIQUE INDEX `apikey_key_hash` ON `api_keys` (`key_hash`);
-- create index "apikey_user_api_keys" to table: "api_keys"
CREATE INDEX `apikey_user_api_keys` ON `api_keys` (`user_api_keys`);
-- create index "episode_status" to table: "episodes"
CREATE INDEX `episode_status` ON `episodes` (`status`);
-- create index "importscanfile_import_scan_files" to table: "import_scan_files"
CREATE INDEX `importscanfile_import_scan_files` ON `import_scan_files` (`import_scan_files`);
-- create index "importscanfile_source_path" to table: "import_scan_files"
CREATE INDEX `importscanfile_source_path` ON `import_scan_files` (`source_path`);
-- create index "importscanshow_import_scan_shows" to table: "import_scan_shows"
CREATE INDEX `importscanshow_import_scan_shows` ON `import_scan_shows` (`import_scan_shows`);
-- create index "importscanshow_folder_path" to table: "import_scan_shows"
CREATE INDEX `importscanshow_folder_path` ON `import_scan_shows` (`folder_path`);
-- create index "oidcidentity_user_oidc_identities" to table: "oidc_identities"
CREATE INDEX `oidcidentity_user_oidc_identities` ON `oidc_identities` (`user_oidc_identities`);
-- create index "request_user_requests" to table: "requests"
CREATE INDEX `request_user_requests` ON `requests` (`user_requests`);
-- create index "request_request_approved_by" to table: "requests"
CREATE INDEX `request_request_approved_by` ON `requests` (`request_approved_by`);
-- create index "session_user_sessions" to table: "sessions"
CREATE INDEX `session_user_sessions` ON `sessions` (`user_sessions`);
-- create index "tvshow_create_time" to table: "tv_shows"
CREATE INDEX `tvshow_create_time` ON `tv_shows` (`create_time`);
-- create index "tvshow_series_status" to table: "tv_shows"
CREATE INDEX `tvshow_series_status` ON `tv_shows` (`series_status`);
-- create index "mediafile_probed_at" to table: "media_files"
CREATE INDEX `mediafile_probed_at` ON `media_files` (`probed_at`) WHERE probed_at IS NULL;
-- create index "invite_invite_created_by" to table: "invites"
CREATE INDEX `invite_invite_created_by` ON `invites` (`invite_created_by`);
-- create index "invite_invite_used_by" to table: "invites"
CREATE INDEX `invite_invite_used_by` ON `invites` (`invite_used_by`);
-- create index "movie_status" to table: "movies"
CREATE INDEX `movie_status` ON `movies` (`status`);
-- create index "movie_create_time" to table: "movies"
CREATE INDEX `movie_create_time` ON `movies` (`create_time`);
-- create index "downloadrecord_status" to table: "download_records"
CREATE INDEX `downloadrecord_status` ON `download_records` (`status`);
-- create index "downloadrecord_torrent_hash" to table: "download_records"
CREATE INDEX `downloadrecord_torrent_hash` ON `download_records` (`torrent_hash`);
-- create index "downloadrecord_update_time_id" to table: "download_records"
CREATE INDEX `downloadrecord_update_time_id` ON `download_records` (`update_time`, `id`);
-- create index "downloadrecord_movie_download_records" to table: "download_records"
CREATE INDEX `downloadrecord_movie_download_records` ON `download_records` (`movie_download_records`);
-- create index "downloadrecord_episode_download_records" to table: "download_records"
CREATE INDEX `downloadrecord_episode_download_records` ON `download_records` (`episode_download_records`);
