-- Re-adds the column the up dropped and refills it from the join table. Atlas's
-- generated reverse of a SQLite table rebuild drops the `new_download_records`
-- scratch table the rebuild already renamed away, so it fails on "no such
-- table".
ALTER TABLE `download_records` ADD COLUMN `wanted_episodes` json NULL;
UPDATE `download_records` SET `wanted_episodes` = (SELECT json_group_array(`episode_id`) FROM `download_record_episodes` WHERE `download_record_id` = `download_records`.`id`) WHERE EXISTS (SELECT 1 FROM `download_record_episodes` WHERE `download_record_id` = `download_records`.`id`);
