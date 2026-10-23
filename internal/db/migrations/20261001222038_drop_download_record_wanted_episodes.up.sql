-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_download_records" table
CREATE TABLE `new_download_records` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `title` text NOT NULL, `quality` text NULL, `size` integer NULL, `status` text NOT NULL DEFAULT ('downloading'), `torrent_hash` text NULL, `release_group` text NULL, `save_path` text NULL, `import_attempts` integer NOT NULL DEFAULT (0), `failure_reason` text NULL, `imported_at` datetime NULL, `indexer_name` text NULL, `download_client_name` text NULL, `replace_mode` text NOT NULL DEFAULT ('none'), `hold_reasons` json NULL, `verification_bypassed` bool NOT NULL DEFAULT (false), `selected_files` json NULL, `selected_bytes` integer NULL, `selection_state` text NOT NULL DEFAULT ('skipped'), `episode_download_records` integer NULL, `movie_download_records` integer NULL, CONSTRAINT `download_records_episodes_anchored_download_records` FOREIGN KEY (`episode_download_records`) REFERENCES `episodes` (`id`) ON DELETE CASCADE, CONSTRAINT `download_records_movies_download_records` FOREIGN KEY (`movie_download_records`) REFERENCES `movies` (`id`) ON DELETE CASCADE);
-- copy rows from old table "download_records" to new temporary table "new_download_records"
INSERT INTO `new_download_records` (`id`, `create_time`, `update_time`, `title`, `quality`, `size`, `status`, `torrent_hash`, `release_group`, `save_path`, `import_attempts`, `failure_reason`, `imported_at`, `indexer_name`, `download_client_name`, `replace_mode`, `hold_reasons`, `verification_bypassed`, `selected_files`, `selected_bytes`, `selection_state`, `episode_download_records`, `movie_download_records`) SELECT `id`, `create_time`, `update_time`, `title`, `quality`, `size`, `status`, `torrent_hash`, `release_group`, `save_path`, `import_attempts`, `failure_reason`, `imported_at`, `indexer_name`, `download_client_name`, `replace_mode`, `hold_reasons`, `verification_bypassed`, `selected_files`, `selected_bytes`, `selection_state`, `episode_download_records`, `movie_download_records` FROM `download_records`;
-- drop "download_records" table after copying rows
DROP TABLE `download_records`;
-- rename temporary table "new_download_records" to "download_records"
ALTER TABLE `new_download_records` RENAME TO `download_records`;
-- create index "downloadrecord_selection_state" to table: "download_records"
CREATE INDEX `downloadrecord_selection_state` ON `download_records` (`selection_state`);
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
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
