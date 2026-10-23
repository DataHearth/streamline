-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_scheduled_jobs" table
CREATE TABLE `new_scheduled_jobs` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `name` text NOT NULL, `paused` bool NOT NULL DEFAULT (false), `last_finished_at` datetime NULL, `last_status` text NOT NULL DEFAULT ('never'), `last_error` text NULL, `last_duration_ms` integer NOT NULL DEFAULT (0));
-- copy rows from old table "scheduled_jobs" to new temporary table "new_scheduled_jobs"
INSERT INTO `new_scheduled_jobs` (`id`, `name`, `paused`, `last_finished_at`, `last_status`, `last_error`, `last_duration_ms`) SELECT `id`, `name`, `paused`, `last_finished_at`, `last_status`, `last_error`, `last_duration_ms` FROM `scheduled_jobs`;
-- drop "scheduled_jobs" table after copying rows
DROP TABLE `scheduled_jobs`;
-- rename temporary table "new_scheduled_jobs" to "scheduled_jobs"
ALTER TABLE `new_scheduled_jobs` RENAME TO `scheduled_jobs`;
-- create index "scheduled_jobs_name_key" to table: "scheduled_jobs"
CREATE UNIQUE INDEX `scheduled_jobs_name_key` ON `scheduled_jobs` (`name`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
