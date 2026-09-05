-- add column "audio_tracks" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `audio_tracks` integer NULL;
-- add column "audio_langs" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `audio_langs` text NULL;
-- add column "sub_langs" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `sub_langs` text NULL;

-- Every probe taken before these columns existed is now incomplete: the
-- reader takes a stamped probed_at as "every probe column recorded", so the
-- three above would read as a confident empty on every old row. Unstamp them
-- so the media-probe backfill fills the new columns.
UPDATE `media_files` SET `probed_at` = NULL;
