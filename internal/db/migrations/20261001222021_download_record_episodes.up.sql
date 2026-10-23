-- create "download_record_episodes" table
CREATE TABLE `download_record_episodes` (`episode_id` integer NOT NULL, `download_record_id` integer NOT NULL, PRIMARY KEY (`episode_id`, `download_record_id`), CONSTRAINT `download_record_episodes_episode_id` FOREIGN KEY (`episode_id`) REFERENCES `episodes` (`id`) ON DELETE CASCADE, CONSTRAINT `download_record_episodes_download_record_id` FOREIGN KEY (`download_record_id`) REFERENCES `download_records` (`id`) ON DELETE CASCADE);
-- backfill: every anchor is a member of its record's episodes
INSERT OR IGNORE INTO `download_record_episodes` (`episode_id`, `download_record_id`) SELECT `episode_download_records`, `id` FROM `download_records` WHERE `episode_download_records` IS NOT NULL;
-- backfill: the grab-time set; wanted_episodes carries no foreign key, so ids of since-deleted episodes are skipped
INSERT OR IGNORE INTO `download_record_episodes` (`episode_id`, `download_record_id`) SELECT j.`value`, r.`id` FROM `download_records` r, json_each(r.`wanted_episodes`) j WHERE r.`wanted_episodes` IS NOT NULL AND EXISTS (SELECT 1 FROM `episodes` e WHERE e.`id` = j.`value`);
