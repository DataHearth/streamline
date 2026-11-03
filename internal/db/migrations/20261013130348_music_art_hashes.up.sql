-- add column "art_sidecar_hash" to table: "artists"
ALTER TABLE `artists` ADD COLUMN `art_sidecar_hash` text NULL;
-- add column "tags_art_hash" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `tags_art_hash` text NULL;
-- add column "art_sidecar_hash" to table: "albums"
ALTER TABLE `albums` ADD COLUMN `art_sidecar_hash` text NULL;
