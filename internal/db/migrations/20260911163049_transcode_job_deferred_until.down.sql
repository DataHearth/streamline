-- reverse: add column "deferred_until" to table: "transcode_jobs"
ALTER TABLE `transcode_jobs` DROP COLUMN `deferred_until`;
