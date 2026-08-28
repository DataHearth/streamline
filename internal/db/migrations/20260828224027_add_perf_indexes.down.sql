-- reverse: create index "downloadrecord_episode_download_records" to table: "download_records"
DROP INDEX `downloadrecord_episode_download_records`;
-- reverse: create index "downloadrecord_movie_download_records" to table: "download_records"
DROP INDEX `downloadrecord_movie_download_records`;
-- reverse: create index "downloadrecord_update_time_id" to table: "download_records"
DROP INDEX `downloadrecord_update_time_id`;
-- reverse: create index "downloadrecord_torrent_hash" to table: "download_records"
DROP INDEX `downloadrecord_torrent_hash`;
-- reverse: create index "downloadrecord_status" to table: "download_records"
DROP INDEX `downloadrecord_status`;
-- reverse: create index "movie_create_time" to table: "movies"
DROP INDEX `movie_create_time`;
-- reverse: create index "movie_status" to table: "movies"
DROP INDEX `movie_status`;
-- reverse: create index "invite_invite_used_by" to table: "invites"
DROP INDEX `invite_invite_used_by`;
-- reverse: create index "invite_invite_created_by" to table: "invites"
DROP INDEX `invite_invite_created_by`;
-- reverse: create index "mediafile_probed_at" to table: "media_files"
DROP INDEX `mediafile_probed_at`;
-- reverse: create index "tvshow_series_status" to table: "tv_shows"
DROP INDEX `tvshow_series_status`;
-- reverse: create index "tvshow_create_time" to table: "tv_shows"
DROP INDEX `tvshow_create_time`;
-- reverse: create index "session_user_sessions" to table: "sessions"
DROP INDEX `session_user_sessions`;
-- reverse: create index "request_request_approved_by" to table: "requests"
DROP INDEX `request_request_approved_by`;
-- reverse: create index "request_user_requests" to table: "requests"
DROP INDEX `request_user_requests`;
-- reverse: create index "oidcidentity_user_oidc_identities" to table: "oidc_identities"
DROP INDEX `oidcidentity_user_oidc_identities`;
-- reverse: create index "importscanshow_folder_path" to table: "import_scan_shows"
DROP INDEX `importscanshow_folder_path`;
-- reverse: create index "importscanshow_import_scan_shows" to table: "import_scan_shows"
DROP INDEX `importscanshow_import_scan_shows`;
-- reverse: create index "importscanfile_source_path" to table: "import_scan_files"
DROP INDEX `importscanfile_source_path`;
-- reverse: create index "importscanfile_import_scan_files" to table: "import_scan_files"
DROP INDEX `importscanfile_import_scan_files`;
-- reverse: create index "episode_status" to table: "episodes"
DROP INDEX `episode_status`;
-- reverse: create index "apikey_user_api_keys" to table: "api_keys"
DROP INDEX `apikey_user_api_keys`;
-- reverse: create index "apikey_key_hash" to table: "api_keys"
DROP INDEX `apikey_key_hash`;
