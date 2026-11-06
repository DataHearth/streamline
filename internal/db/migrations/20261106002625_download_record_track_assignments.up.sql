-- add column "track_assignments" to table: "download_records"
ALTER TABLE `download_records` ADD COLUMN `track_assignments` json NULL;
