-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_transcode_jobs" table
CREATE TABLE `new_transcode_jobs` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `status` text NOT NULL DEFAULT ('queued'), `attempts` integer NOT NULL DEFAULT (0), `error` text NULL, `size_before` integer NULL, `size_after` integer NULL, `started_at` datetime NULL, `finished_at` datetime NULL, `media_file_transcode_jobs` integer NOT NULL, CONSTRAINT `transcode_jobs_media_files_transcode_jobs` FOREIGN KEY (`media_file_transcode_jobs`) REFERENCES `media_files` (`id`) ON DELETE CASCADE);
-- copy rows from old table "transcode_jobs" to new temporary table "new_transcode_jobs"
INSERT INTO `new_transcode_jobs` (`id`, `create_time`, `update_time`, `status`, `attempts`, `error`, `size_before`, `size_after`, `started_at`, `finished_at`, `media_file_transcode_jobs`) SELECT `id`, `create_time`, `update_time`, `status`, `attempts`, `error`, `size_before`, `size_after`, `started_at`, `finished_at`, `media_file_transcode_jobs` FROM `transcode_jobs`;
-- drop "transcode_jobs" table after copying rows
DROP TABLE `transcode_jobs`;
-- rename temporary table "new_transcode_jobs" to "transcode_jobs"
ALTER TABLE `new_transcode_jobs` RENAME TO `transcode_jobs`;
-- create index "transcodejob_status" to table: "transcode_jobs"
CREATE INDEX `transcodejob_status` ON `transcode_jobs` (`status`);
-- create index "transcodejob_media_file_transcode_jobs" to table: "transcode_jobs"
CREATE INDEX `transcodejob_media_file_transcode_jobs` ON `transcode_jobs` (`media_file_transcode_jobs`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
