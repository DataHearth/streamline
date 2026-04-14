-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_import_scans" table
CREATE TABLE `new_import_scans` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `source_path` text NOT NULL, `kind` text NOT NULL DEFAULT ('movie'), `mode` text NOT NULL, `import_mode` text NULL, `status` text NOT NULL DEFAULT ('running'), `total_count` integer NOT NULL DEFAULT (0), `processed_count` integer NOT NULL DEFAULT (0), `commit_success_count` integer NOT NULL DEFAULT (0), `commit_failed_count` integer NOT NULL DEFAULT (0), `failure_reason` text NULL, `scanned_at` datetime NULL, `committed_at` datetime NULL);
-- copy rows from old table "import_scans" to new temporary table "new_import_scans"
INSERT INTO `new_import_scans` (`id`, `create_time`, `update_time`, `source_path`, `mode`, `import_mode`, `status`, `total_count`, `processed_count`, `commit_success_count`, `commit_failed_count`, `failure_reason`, `scanned_at`, `committed_at`) SELECT `id`, `create_time`, `update_time`, `source_path`, `mode`, `import_mode`, `status`, `total_count`, `processed_count`, `commit_success_count`, `commit_failed_count`, `failure_reason`, `scanned_at`, `committed_at` FROM `import_scans`;
-- drop "import_scans" table after copying rows
DROP TABLE `import_scans`;
-- rename temporary table "new_import_scans" to "import_scans"
ALTER TABLE `new_import_scans` RENAME TO `import_scans`;
-- create index "importscan_status" to table: "import_scans"
CREATE INDEX `importscan_status` ON `import_scans` (`status`);
-- create index "importscan_kind" to table: "import_scans"
CREATE INDEX `importscan_kind` ON `import_scans` (`kind`);
-- create "import_scan_shows" table
CREATE TABLE `import_scan_shows` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `folder_path` text NOT NULL, `parsed_title` text NULL, `parsed_year` integer NULL, `classification` text NOT NULL DEFAULT ('unmatched'), `tvdb_id` integer NULL, `candidates` json NULL, `existing_tvshow_id` integer NULL, `file_count` integer NOT NULL DEFAULT (0), `decision` text NOT NULL DEFAULT ('pending'), `decision_tvdb_id` integer NULL, `outcome` text NOT NULL DEFAULT ('pending'), `outcome_message` text NULL, `created_tvshow_id` integer NULL, `import_scan_shows` integer NOT NULL, CONSTRAINT `import_scan_shows_import_scans_shows` FOREIGN KEY (`import_scan_shows`) REFERENCES `import_scans` (`id`) ON DELETE CASCADE);
-- create index "importscanshow_classification" to table: "import_scan_shows"
CREATE INDEX `importscanshow_classification` ON `import_scan_shows` (`classification`);
-- create index "importscanshow_decision" to table: "import_scan_shows"
CREATE INDEX `importscanshow_decision` ON `import_scan_shows` (`decision`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
