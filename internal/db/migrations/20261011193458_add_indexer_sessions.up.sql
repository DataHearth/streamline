-- create "indexer_sessions" table
CREATE TABLE `indexer_sessions` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `name` text NOT NULL, `cookies` json NULL, `user_agent` text NULL, `expires_at` datetime NULL, `login_failures` integer NOT NULL DEFAULT (0), `retry_at` datetime NULL, `last_error` text NULL);
-- create index "indexer_sessions_name_key" to table: "indexer_sessions"
CREATE UNIQUE INDEX `indexer_sessions_name_key` ON `indexer_sessions` (`name`);
