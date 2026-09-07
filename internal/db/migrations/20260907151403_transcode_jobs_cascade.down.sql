-- reverse: create index "transcodejob_media_file_transcode_jobs" to table: "transcode_jobs"
DROP INDEX `transcodejob_media_file_transcode_jobs`;
-- reverse: create index "transcodejob_status" to table: "transcode_jobs"
DROP INDEX `transcodejob_status`;
-- reverse: create "new_transcode_jobs" table
DROP TABLE `new_transcode_jobs`;
