-- add column "config_hash" to table: "indexer_sessions"
ALTER TABLE `indexer_sessions` ADD COLUMN `config_hash` text NULL;
