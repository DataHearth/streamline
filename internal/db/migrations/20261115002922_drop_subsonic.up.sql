-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_users" table
CREATE TABLE `new_users` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `email` text NOT NULL, `password_hash` text NULL, `role` text NOT NULL DEFAULT ('member'), `auth_method` text NOT NULL DEFAULT ('local'), `display_name` text NULL, `device_email` text NULL, `failed_login_count` integer NOT NULL DEFAULT (0), `last_failed_login_at` datetime NULL, `locked_until` datetime NULL);
-- copy rows from old table "users" to new temporary table "new_users"
INSERT INTO `new_users` (`id`, `create_time`, `update_time`, `email`, `password_hash`, `role`, `auth_method`, `display_name`, `device_email`, `failed_login_count`, `last_failed_login_at`, `locked_until`) SELECT `id`, `create_time`, `update_time`, `email`, `password_hash`, `role`, `auth_method`, `display_name`, `device_email`, `failed_login_count`, `last_failed_login_at`, `locked_until` FROM `users`;
-- drop "users" table after copying rows
DROP TABLE `users`;
-- rename temporary table "new_users" to "users"
ALTER TABLE `new_users` RENAME TO `users`;
-- create index "users_email_key" to table: "users"
CREATE UNIQUE INDEX `users_email_key` ON `users` (`email`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- drop "bookmarks" table
DROP TABLE `bookmarks`;
-- drop "music_annotations" table
DROP TABLE `music_annotations`;
-- drop "play_queues" table
DROP TABLE `play_queues`;
-- drop "playlists" table
DROP TABLE `playlists`;
-- drop "radio_stations" table
DROP TABLE `radio_stations`;
-- drop "shares" table
DROP TABLE `shares`;
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
