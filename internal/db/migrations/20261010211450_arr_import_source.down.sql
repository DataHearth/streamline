-- Rebuilds the three tables to their previous shape. Atlas's generated reverse
-- of a SQLite table rebuild drops the `new_*` scratch tables the rebuild
-- already renamed away, so it fails on "no such table".
--
-- A Radarr/Sonarr scan has no representation in the old schema — a title-only
-- row would commit as a file at an empty path — so those scans are dropped
-- rather than carried down as filesystem scans.
PRAGMA foreign_keys = off;
DELETE FROM `import_scan_files` WHERE `import_scan_files` IN (SELECT `id` FROM `import_scans` WHERE `source` != 'filesystem');
DELETE FROM `import_scan_shows` WHERE `import_scan_shows` IN (SELECT `id` FROM `import_scans` WHERE `source` != 'filesystem');
DELETE FROM `import_scans` WHERE `source` != 'filesystem';
CREATE TABLE `old_import_scans` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `source_path` text NOT NULL, `kind` text NOT NULL DEFAULT ('movie'), `mode` text NOT NULL, `import_mode` text NULL, `status` text NOT NULL DEFAULT ('running'), `total_count` integer NOT NULL DEFAULT (0), `processed_count` integer NOT NULL DEFAULT (0), `commit_success_count` integer NOT NULL DEFAULT (0), `commit_failed_count` integer NOT NULL DEFAULT (0), `failure_reason` text NULL, `scanned_at` datetime NULL, `committed_at` datetime NULL, `failure_code` text NULL);
INSERT INTO `old_import_scans` (`id`, `create_time`, `update_time`, `source_path`, `kind`, `mode`, `import_mode`, `status`, `total_count`, `processed_count`, `commit_success_count`, `commit_failed_count`, `failure_reason`, `failure_code`, `scanned_at`, `committed_at`) SELECT `id`, `create_time`, `update_time`, COALESCE(`source_path`, ''), `kind`, `mode`, `import_mode`, `status`, `total_count`, `processed_count`, `commit_success_count`, `commit_failed_count`, `failure_reason`, `failure_code`, `scanned_at`, `committed_at` FROM `import_scans`;
DROP TABLE `import_scans`;
ALTER TABLE `old_import_scans` RENAME TO `import_scans`;
CREATE INDEX `importscan_status` ON `import_scans` (`status`);
CREATE INDEX `importscan_kind` ON `import_scans` (`kind`);
CREATE TABLE `old_import_scan_files` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `source_path` text NOT NULL, `size` integer NOT NULL, `parsed_title` text NULL, `parsed_year` integer NULL, `parsed_quality` text NULL, `parsed_release_group` text NULL, `classification` text NOT NULL DEFAULT ('unmatched'), `candidates` json NULL, `tmdb_id` integer NULL, `existing_movie_id` integer NULL, `decision` text NOT NULL DEFAULT ('pending'), `decision_tmdb_id` integer NULL, `outcome` text NOT NULL DEFAULT ('pending'), `outcome_message` text NULL, `created_movie_id` integer NULL, `import_scan_files` integer NOT NULL, CONSTRAINT `import_scan_files_import_scans_files` FOREIGN KEY (`import_scan_files`) REFERENCES `import_scans` (`id`) ON DELETE CASCADE);
INSERT INTO `old_import_scan_files` (`id`, `create_time`, `update_time`, `source_path`, `size`, `parsed_title`, `parsed_year`, `parsed_quality`, `parsed_release_group`, `classification`, `candidates`, `tmdb_id`, `existing_movie_id`, `decision`, `decision_tmdb_id`, `outcome`, `outcome_message`, `created_movie_id`, `import_scan_files`) SELECT `id`, `create_time`, `update_time`, COALESCE(`source_path`, ''), `size`, `parsed_title`, `parsed_year`, `parsed_quality`, `parsed_release_group`, `classification`, `candidates`, `tmdb_id`, `existing_movie_id`, `decision`, `decision_tmdb_id`, `outcome`, `outcome_message`, `created_movie_id`, `import_scan_files` FROM `import_scan_files`;
DROP TABLE `import_scan_files`;
ALTER TABLE `old_import_scan_files` RENAME TO `import_scan_files`;
CREATE INDEX `importscanfile_classification` ON `import_scan_files` (`classification`);
CREATE INDEX `importscanfile_decision` ON `import_scan_files` (`decision`);
CREATE INDEX `importscanfile_import_scan_files` ON `import_scan_files` (`import_scan_files`);
CREATE INDEX `importscanfile_source_path` ON `import_scan_files` (`source_path`);
CREATE TABLE `old_import_scan_shows` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `folder_path` text NOT NULL, `parsed_title` text NULL, `parsed_year` integer NULL, `classification` text NOT NULL DEFAULT ('unmatched'), `tvdb_id` integer NULL, `candidates` json NULL, `existing_tvshow_id` integer NULL, `file_count` integer NOT NULL DEFAULT (0), `decision` text NOT NULL DEFAULT ('pending'), `decision_tvdb_id` integer NULL, `outcome` text NOT NULL DEFAULT ('pending'), `outcome_message` text NULL, `created_tvshow_id` integer NULL, `import_scan_shows` integer NOT NULL, CONSTRAINT `import_scan_shows_import_scans_shows` FOREIGN KEY (`import_scan_shows`) REFERENCES `import_scans` (`id`) ON DELETE CASCADE);
INSERT INTO `old_import_scan_shows` (`id`, `create_time`, `update_time`, `folder_path`, `parsed_title`, `parsed_year`, `classification`, `tvdb_id`, `candidates`, `existing_tvshow_id`, `file_count`, `decision`, `decision_tvdb_id`, `outcome`, `outcome_message`, `created_tvshow_id`, `import_scan_shows`) SELECT `id`, `create_time`, `update_time`, `folder_path`, `parsed_title`, `parsed_year`, `classification`, `tvdb_id`, `candidates`, `existing_tvshow_id`, `file_count`, `decision`, `decision_tvdb_id`, `outcome`, `outcome_message`, `created_tvshow_id`, `import_scan_shows` FROM `import_scan_shows`;
DROP TABLE `import_scan_shows`;
ALTER TABLE `old_import_scan_shows` RENAME TO `import_scan_shows`;
CREATE INDEX `importscanshow_classification` ON `import_scan_shows` (`classification`);
CREATE INDEX `importscanshow_decision` ON `import_scan_shows` (`decision`);
CREATE INDEX `importscanshow_import_scan_shows` ON `import_scan_shows` (`import_scan_shows`);
CREATE INDEX `importscanshow_folder_path` ON `import_scan_shows` (`folder_path`);
PRAGMA foreign_keys = on;
