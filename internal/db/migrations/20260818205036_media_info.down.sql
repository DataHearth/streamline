-- reverse: add column "probed_at" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `probed_at`;
-- reverse: add column "bitrate" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `bitrate`;
-- reverse: add column "audio_channels" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `audio_channels`;
-- reverse: add column "audio_codec" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `audio_codec`;
-- reverse: add column "height" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `height`;
-- reverse: add column "width" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `width`;
-- reverse: add column "video_codec" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `video_codec`;
-- reverse: add column "duration_seconds" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `duration_seconds`;
-- reverse: add column "container" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `container`;
