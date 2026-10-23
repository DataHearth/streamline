-- add column "transcoded_at" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `transcoded_at` datetime NULL;
-- add column "size_before" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `size_before` integer NULL;
-- create "transcode_jobs" table
CREATE TABLE `transcode_jobs` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `status` text NOT NULL DEFAULT ('queued'), `attempts` integer NOT NULL DEFAULT (0), `error` text NULL, `size_before` integer NULL, `size_after` integer NULL, `started_at` datetime NULL, `finished_at` datetime NULL, `media_file_transcode_jobs` integer NOT NULL, CONSTRAINT `transcode_jobs_media_files_transcode_jobs` FOREIGN KEY (`media_file_transcode_jobs`) REFERENCES `media_files` (`id`) ON DELETE NO ACTION);
-- create index "transcodejob_status" to table: "transcode_jobs"
CREATE INDEX `transcodejob_status` ON `transcode_jobs` (`status`);
-- create index "transcodejob_media_file_transcode_jobs" to table: "transcode_jobs"
CREATE INDEX `transcodejob_media_file_transcode_jobs` ON `transcode_jobs` (`media_file_transcode_jobs`);
