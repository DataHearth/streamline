-- add column "folder_assignments" to table: "download_records"
ALTER TABLE `download_records` ADD COLUMN `folder_assignments` json NULL;
