-- reverse: add column "art_sidecar_hash" to table: "albums"
ALTER TABLE `albums` DROP COLUMN `art_sidecar_hash`;
-- reverse: add column "tags_art_hash" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `tags_art_hash`;
-- reverse: add column "art_sidecar_hash" to table: "artists"
ALTER TABLE `artists` DROP COLUMN `art_sidecar_hash`;
