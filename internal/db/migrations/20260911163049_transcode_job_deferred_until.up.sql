-- add column "deferred_until" to table: "transcode_jobs"
ALTER TABLE `transcode_jobs` ADD COLUMN `deferred_until` datetime NULL;
