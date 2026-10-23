-- reverse: add column "sub_langs" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `sub_langs`;
-- reverse: add column "audio_langs" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `audio_langs`;
-- reverse: add column "audio_tracks" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `audio_tracks`;
