-- create "torrent_sessions" table
CREATE TABLE `torrent_sessions` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `info_hash` text NOT NULL, `name` text NULL, `save_path` text NOT NULL, `source_magnet` text NULL, `source_torrent` blob NULL, `paused` bool NOT NULL DEFAULT (false), `completed_at` datetime NULL, `seed_stopped` bool NOT NULL DEFAULT (false));
-- create index "torrent_sessions_info_hash_key" to table: "torrent_sessions"
CREATE UNIQUE INDEX `torrent_sessions_info_hash_key` ON `torrent_sessions` (`info_hash`);
