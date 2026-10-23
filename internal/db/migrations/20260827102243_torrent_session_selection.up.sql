-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_torrent_sessions" table
CREATE TABLE `new_torrent_sessions` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `info_hash` text NOT NULL, `name` text NULL, `save_path` text NOT NULL, `source_magnet` text NULL, `source_torrent` blob NULL, `paused` bool NOT NULL DEFAULT (false), `completed_at` datetime NULL, `seed_stopped` bool NOT NULL DEFAULT (false), `uploaded` integer NOT NULL DEFAULT (0), `wanted_files` json NULL, `selection_mode` text NOT NULL DEFAULT ('all'));
-- copy rows from old table "torrent_sessions" to new temporary table "new_torrent_sessions"
INSERT INTO `new_torrent_sessions` (`id`, `create_time`, `update_time`, `info_hash`, `name`, `save_path`, `source_magnet`, `source_torrent`, `paused`, `completed_at`, `seed_stopped`, `uploaded`) SELECT `id`, `create_time`, `update_time`, `info_hash`, `name`, `save_path`, `source_magnet`, `source_torrent`, `paused`, `completed_at`, `seed_stopped`, `uploaded` FROM `torrent_sessions`;
-- drop "torrent_sessions" table after copying rows
DROP TABLE `torrent_sessions`;
-- rename temporary table "new_torrent_sessions" to "torrent_sessions"
ALTER TABLE `new_torrent_sessions` RENAME TO `torrent_sessions`;
-- create index "torrent_sessions_info_hash_key" to table: "torrent_sessions"
CREATE UNIQUE INDEX `torrent_sessions_info_hash_key` ON `torrent_sessions` (`info_hash`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
