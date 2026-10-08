-- Hand-written. Atlas's generated reverse of a SQLite table rebuild drops the
-- `new_requests` scratch table the rebuild already renamed away (fails on "no
-- such table") and never restores media_id's NOT NULL, so the table is rebuilt
-- back to its previous shape. Music and book rows have no place in it.
PRAGMA foreign_keys = off;
DELETE FROM `requests` WHERE `media_type` NOT IN ('movie', 'tvshow');
DROP INDEX `request_request_approved_by`;
DROP INDEX `request_user_requests`;
DROP INDEX `request_media_type_media_mbid`;
DROP INDEX `request_media_type_media_id`;
CREATE TABLE `new_requests` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `media_type` text NOT NULL, `media_id` integer NOT NULL, `title` text NOT NULL, `status` text NOT NULL DEFAULT ('pending'), `reason` text NULL, `request_approved_by` integer NULL, `user_requests` integer NOT NULL, `quality_profile` text NULL, CONSTRAINT `requests_users_approved_by` FOREIGN KEY (`request_approved_by`) REFERENCES `users` (`id`) ON DELETE SET NULL, CONSTRAINT `requests_users_requests` FOREIGN KEY (`user_requests`) REFERENCES `users` (`id`) ON DELETE CASCADE);
INSERT INTO `new_requests` (`id`, `create_time`, `update_time`, `media_type`, `media_id`, `title`, `status`, `reason`, `request_approved_by`, `user_requests`, `quality_profile`) SELECT `id`, `create_time`, `update_time`, `media_type`, `media_id`, `title`, `status`, `reason`, `request_approved_by`, `user_requests`, `quality_profile` FROM `requests`;
DROP TABLE `requests`;
ALTER TABLE `new_requests` RENAME TO `requests`;
CREATE UNIQUE INDEX `request_media_type_media_id` ON `requests` (`media_type`, `media_id`) WHERE status IN ('pending', 'approved', 'available');
CREATE INDEX `request_user_requests` ON `requests` (`user_requests`);
CREATE INDEX `request_request_approved_by` ON `requests` (`request_approved_by`);
PRAGMA foreign_keys = on;
