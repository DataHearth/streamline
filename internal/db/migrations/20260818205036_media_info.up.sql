-- add column "container" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `container` text NULL;
-- add column "duration_seconds" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `duration_seconds` integer NULL;
-- add column "video_codec" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `video_codec` text NULL;
-- add column "width" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `width` integer NULL;
-- add column "height" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `height` integer NULL;
-- add column "audio_codec" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `audio_codec` text NULL;
-- add column "audio_channels" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `audio_channels` integer NULL;
-- add column "bitrate" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `bitrate` integer NULL;
-- add column "probed_at" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `probed_at` datetime NULL;
