-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_invites" table
CREATE TABLE `new_invites` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `token_hash` text NOT NULL, `email` text NULL, `role` text NOT NULL DEFAULT ('member'), `expires_at` datetime NOT NULL, `used_at` datetime NULL, `invite_created_by` integer NOT NULL, `invite_used_by` integer NULL, CONSTRAINT `invites_users_created_by` FOREIGN KEY (`invite_created_by`) REFERENCES `users` (`id`) ON DELETE CASCADE, CONSTRAINT `invites_users_used_by` FOREIGN KEY (`invite_used_by`) REFERENCES `users` (`id`) ON DELETE SET NULL);
-- copy rows from old table "invites" to new temporary table "new_invites"
INSERT INTO `new_invites` (`id`, `create_time`, `update_time`, `token_hash`, `email`, `role`, `expires_at`, `used_at`, `invite_created_by`, `invite_used_by`) SELECT `id`, `create_time`, `update_time`, `token_hash`, `email`, `role`, `expires_at`, `used_at`, `invite_created_by`, `invite_used_by` FROM `invites`;
-- drop "invites" table after copying rows
DROP TABLE `invites`;
-- rename temporary table "new_invites" to "invites"
ALTER TABLE `new_invites` RENAME TO `invites`;
-- create index "invites_token_hash_key" to table: "invites"
CREATE UNIQUE INDEX `invites_token_hash_key` ON `invites` (`token_hash`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
