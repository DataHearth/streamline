-- create "torrent_data_removals" table
CREATE TABLE `torrent_data_removals` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `info_hash` text NOT NULL, `name` text NOT NULL, `dir` text NOT NULL, `info` blob NOT NULL);
-- create index "torrent_data_removals_info_hash_key" to table: "torrent_data_removals"
CREATE UNIQUE INDEX `torrent_data_removals_info_hash_key` ON `torrent_data_removals` (`info_hash`);
