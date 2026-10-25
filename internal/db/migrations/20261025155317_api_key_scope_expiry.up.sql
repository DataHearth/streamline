-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_api_keys" table
CREATE TABLE `new_api_keys` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `name` text NOT NULL, `key_hash` text NOT NULL, `last_used_at` datetime NULL, `scope` text NOT NULL DEFAULT ('full'), `expires_at` datetime NULL, `user_api_keys` integer NOT NULL, CONSTRAINT `api_keys_users_api_keys` FOREIGN KEY (`user_api_keys`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- copy rows from old table "api_keys" to new temporary table "new_api_keys"
INSERT INTO `new_api_keys` (`id`, `create_time`, `update_time`, `name`, `key_hash`, `last_used_at`, `user_api_keys`) SELECT `id`, `create_time`, `update_time`, `name`, `key_hash`, `last_used_at`, `user_api_keys` FROM `api_keys`;
-- drop "api_keys" table after copying rows
DROP TABLE `api_keys`;
-- rename temporary table "new_api_keys" to "api_keys"
ALTER TABLE `new_api_keys` RENAME TO `api_keys`;
-- create index "apikey_key_hash" to table: "api_keys"
CREATE UNIQUE INDEX `apikey_key_hash` ON `api_keys` (`key_hash`);
-- create index "apikey_user_api_keys" to table: "api_keys"
CREATE INDEX `apikey_user_api_keys` ON `api_keys` (`user_api_keys`);
-- create index "apikey_expires_at" to table: "api_keys"
CREATE INDEX `apikey_expires_at` ON `api_keys` (`expires_at`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
