-- reverse: add column "config_hash" to table: "indexer_sessions"
ALTER TABLE `indexer_sessions` DROP COLUMN `config_hash`;
