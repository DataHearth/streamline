-- reverse: create index "transcodejob_media_file_transcode_jobs" to table: "transcode_jobs"
DROP INDEX `transcodejob_media_file_transcode_jobs`;
-- reverse: create index "transcodejob_status" to table: "transcode_jobs"
DROP INDEX `transcodejob_status`;
-- reverse: create "transcode_jobs" table
DROP TABLE `transcode_jobs`;
-- reverse: add column "size_before" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `size_before`;
-- reverse: add column "transcoded_at" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `transcoded_at`;
