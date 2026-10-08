-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_requests" table
CREATE TABLE `new_requests` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `media_type` text NOT NULL, `media_id` integer NULL DEFAULT (0), `media_mbid` text NULL, `book_kind` text NULL, `title` text NOT NULL, `status` text NOT NULL DEFAULT ('pending'), `reason` text NULL, `quality_profile` text NULL, `request_approved_by` integer NULL, `user_requests` integer NOT NULL, CONSTRAINT `requests_users_approved_by` FOREIGN KEY (`request_approved_by`) REFERENCES `users` (`id`) ON DELETE SET NULL, CONSTRAINT `requests_users_requests` FOREIGN KEY (`user_requests`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- copy rows from old table "requests" to new temporary table "new_requests"
INSERT INTO `new_requests` (`id`, `create_time`, `update_time`, `media_type`, `media_id`, `title`, `status`, `reason`, `quality_profile`, `request_approved_by`, `user_requests`) SELECT `id`, `create_time`, `update_time`, `media_type`, `media_id`, `title`, `status`, `reason`, `quality_profile`, `request_approved_by`, `user_requests` FROM `requests`;
-- drop "requests" table after copying rows
DROP TABLE `requests`;
-- rename temporary table "new_requests" to "requests"
ALTER TABLE `new_requests` RENAME TO `requests`;
-- create index "request_media_type_media_id" to table: "requests"
CREATE UNIQUE INDEX `request_media_type_media_id` ON `requests` (`media_type`, `media_id`) WHERE status IN ('pending', 'approved', 'available') AND media_id <> 0;
-- create index "request_media_type_media_mbid" to table: "requests"
CREATE UNIQUE INDEX `request_media_type_media_mbid` ON `requests` (`media_type`, `media_mbid`) WHERE status IN ('pending', 'approved', 'available') AND media_mbid <> '';
-- create index "request_user_requests" to table: "requests"
CREATE INDEX `request_user_requests` ON `requests` (`user_requests`);
-- create index "request_request_approved_by" to table: "requests"
CREATE INDEX `request_request_approved_by` ON `requests` (`request_approved_by`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
