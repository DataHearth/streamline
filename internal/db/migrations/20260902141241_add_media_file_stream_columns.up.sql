-- add column "audio_tracks" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `audio_tracks` integer NULL;
-- add column "audio_langs" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `audio_langs` text NULL;
-- add column "sub_langs" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `sub_langs` text NULL;
