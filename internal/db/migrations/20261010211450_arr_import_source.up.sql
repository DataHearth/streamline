-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_import_scans" table
CREATE TABLE `new_import_scans` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `source_path` text NULL, `source` text NOT NULL DEFAULT ('filesystem'), `source_url` text NULL, `mappings` json NULL, `kind` text NOT NULL DEFAULT ('movie'), `mode` text NOT NULL, `import_mode` text NULL, `status` text NOT NULL DEFAULT ('running'), `total_count` integer NOT NULL DEFAULT (0), `processed_count` integer NOT NULL DEFAULT (0), `commit_success_count` integer NOT NULL DEFAULT (0), `commit_failed_count` integer NOT NULL DEFAULT (0), `failure_reason` text NULL, `failure_code` text NULL, `scanned_at` datetime NULL, `committed_at` datetime NULL);
-- copy rows from old table "import_scans" to new temporary table "new_import_scans"
INSERT INTO `new_import_scans` (`id`, `create_time`, `update_time`, `source_path`, `kind`, `mode`, `import_mode`, `status`, `total_count`, `processed_count`, `commit_success_count`, `commit_failed_count`, `failure_reason`, `failure_code`, `scanned_at`, `committed_at`) SELECT `id`, `create_time`, `update_time`, `source_path`, `kind`, `mode`, `import_mode`, `status`, `total_count`, `processed_count`, `commit_success_count`, `commit_failed_count`, `failure_reason`, `failure_code`, `scanned_at`, `committed_at` FROM `import_scans`;
-- drop "import_scans" table after copying rows
DROP TABLE `import_scans`;
-- rename temporary table "new_import_scans" to "import_scans"
ALTER TABLE `new_import_scans` RENAME TO `import_scans`;
-- create index "importscan_status" to table: "import_scans"
CREATE INDEX `importscan_status` ON `import_scans` (`status`);
-- create index "importscan_kind" to table: "import_scans"
CREATE INDEX `importscan_kind` ON `import_scans` (`kind`);
-- create index "importscan_source" to table: "import_scans"
CREATE INDEX `importscan_source` ON `import_scans` (`source`);
-- create "new_import_scan_files" table
CREATE TABLE `new_import_scan_files` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `source_path` text NULL, `size` integer NOT NULL, `quality_profile` text NULL, `monitored` bool NOT NULL DEFAULT (true), `parsed_title` text NULL, `parsed_year` integer NULL, `parsed_quality` text NULL, `parsed_release_group` text NULL, `classification` text NOT NULL DEFAULT ('unmatched'), `candidates` json NULL, `tmdb_id` integer NULL, `existing_movie_id` integer NULL, `decision` text NOT NULL DEFAULT ('pending'), `decision_tmdb_id` integer NULL, `outcome` text NOT NULL DEFAULT ('pending'), `outcome_message` text NULL, `created_movie_id` integer NULL, `import_scan_files` integer NOT NULL, CONSTRAINT `import_scan_files_import_scans_files` FOREIGN KEY (`import_scan_files`) REFERENCES `import_scans` (`id`) ON DELETE CASCADE);
-- copy rows from old table "import_scan_files" to new temporary table "new_import_scan_files"
INSERT INTO `new_import_scan_files` (`id`, `create_time`, `update_time`, `source_path`, `size`, `parsed_title`, `parsed_year`, `parsed_quality`, `parsed_release_group`, `classification`, `candidates`, `tmdb_id`, `existing_movie_id`, `decision`, `decision_tmdb_id`, `outcome`, `outcome_message`, `created_movie_id`, `import_scan_files`) SELECT `id`, `create_time`, `update_time`, `source_path`, `size`, `parsed_title`, `parsed_year`, `parsed_quality`, `parsed_release_group`, `classification`, `candidates`, `tmdb_id`, `existing_movie_id`, `decision`, `decision_tmdb_id`, `outcome`, `outcome_message`, `created_movie_id`, `import_scan_files` FROM `import_scan_files`;
-- drop "import_scan_files" table after copying rows
DROP TABLE `import_scan_files`;
-- rename temporary table "new_import_scan_files" to "import_scan_files"
ALTER TABLE `new_import_scan_files` RENAME TO `import_scan_files`;
-- create index "importscanfile_classification" to table: "import_scan_files"
CREATE INDEX `importscanfile_classification` ON `import_scan_files` (`classification`);
-- create index "importscanfile_decision" to table: "import_scan_files"
CREATE INDEX `importscanfile_decision` ON `import_scan_files` (`decision`);
-- create index "importscanfile_import_scan_files" to table: "import_scan_files"
CREATE INDEX `importscanfile_import_scan_files` ON `import_scan_files` (`import_scan_files`);
-- create index "importscanfile_source_path" to table: "import_scan_files"
CREATE INDEX `importscanfile_source_path` ON `import_scan_files` (`source_path`);
-- create "new_import_scan_shows" table
CREATE TABLE `new_import_scan_shows` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `folder_path` text NOT NULL, `parsed_title` text NULL, `parsed_year` integer NULL, `classification` text NOT NULL DEFAULT ('unmatched'), `tvdb_id` integer NULL, `candidates` json NULL, `existing_tvshow_id` integer NULL, `file_count` integer NOT NULL DEFAULT (0), `quality_profile` text NULL, `monitored` bool NOT NULL DEFAULT (true), `series_type` text NULL, `monitoring` json NULL, `source_files` json NULL, `decision` text NOT NULL DEFAULT ('pending'), `decision_tvdb_id` integer NULL, `outcome` text NOT NULL DEFAULT ('pending'), `outcome_message` text NULL, `created_tvshow_id` integer NULL, `import_scan_shows` integer NOT NULL, CONSTRAINT `import_scan_shows_import_scans_shows` FOREIGN KEY (`import_scan_shows`) REFERENCES `import_scans` (`id`) ON DELETE CASCADE);
-- copy rows from old table "import_scan_shows" to new temporary table "new_import_scan_shows"
INSERT INTO `new_import_scan_shows` (`id`, `create_time`, `update_time`, `folder_path`, `parsed_title`, `parsed_year`, `classification`, `tvdb_id`, `candidates`, `existing_tvshow_id`, `file_count`, `decision`, `decision_tvdb_id`, `outcome`, `outcome_message`, `created_tvshow_id`, `import_scan_shows`) SELECT `id`, `create_time`, `update_time`, `folder_path`, `parsed_title`, `parsed_year`, `classification`, `tvdb_id`, `candidates`, `existing_tvshow_id`, `file_count`, `decision`, `decision_tvdb_id`, `outcome`, `outcome_message`, `created_tvshow_id`, `import_scan_shows` FROM `import_scan_shows`;
-- drop "import_scan_shows" table after copying rows
DROP TABLE `import_scan_shows`;
-- rename temporary table "new_import_scan_shows" to "import_scan_shows"
ALTER TABLE `new_import_scan_shows` RENAME TO `import_scan_shows`;
-- create index "importscanshow_classification" to table: "import_scan_shows"
CREATE INDEX `importscanshow_classification` ON `import_scan_shows` (`classification`);
-- create index "importscanshow_decision" to table: "import_scan_shows"
CREATE INDEX `importscanshow_decision` ON `import_scan_shows` (`decision`);
-- create index "importscanshow_import_scan_shows" to table: "import_scan_shows"
CREATE INDEX `importscanshow_import_scan_shows` ON `import_scan_shows` (`import_scan_shows`);
-- create index "importscanshow_folder_path" to table: "import_scan_shows"
CREATE INDEX `importscanshow_folder_path` ON `import_scan_shows` (`folder_path`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
