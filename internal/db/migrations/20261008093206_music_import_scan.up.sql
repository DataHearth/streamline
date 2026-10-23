-- create "import_scan_albums" table
CREATE TABLE `import_scan_albums` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `folder_path` text NOT NULL, `tagged_artist` text NULL, `tagged_album` text NULL, `classification` text NOT NULL DEFAULT ('unmatched'), `release_group_mbid` text NULL, `artist_mbid` text NULL, `candidates` json NULL, `existing_album_id` integer NULL, `file_count` integer NOT NULL DEFAULT (0), `decision` text NOT NULL DEFAULT ('pending'), `decision_release_group_mbid` text NULL, `outcome` text NOT NULL DEFAULT ('pending'), `outcome_message` text NULL, `created_album_id` integer NULL, `import_scan_albums` integer NOT NULL, CONSTRAINT `import_scan_albums_import_scans_albums` FOREIGN KEY (`import_scan_albums`) REFERENCES `import_scans` (`id`) ON DELETE CASCADE);
-- create index "importscanalbum_classification" to table: "import_scan_albums"
CREATE INDEX `importscanalbum_classification` ON `import_scan_albums` (`classification`);
-- create index "importscanalbum_decision" to table: "import_scan_albums"
CREATE INDEX `importscanalbum_decision` ON `import_scan_albums` (`decision`);
-- create index "importscanalbum_import_scan_albums" to table: "import_scan_albums"
CREATE INDEX `importscanalbum_import_scan_albums` ON `import_scan_albums` (`import_scan_albums`);
-- create index "importscanalbum_folder_path" to table: "import_scan_albums"
CREATE INDEX `importscanalbum_folder_path` ON `import_scan_albums` (`folder_path`);
