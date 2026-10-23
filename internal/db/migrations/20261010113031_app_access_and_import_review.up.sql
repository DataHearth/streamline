-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- add column "failure_code" to table: "import_scans"
ALTER TABLE `import_scans` ADD COLUMN `failure_code` text NULL;
-- add column "subsonic_created_at" to table: "users"
ALTER TABLE `users` ADD COLUMN `subsonic_created_at` datetime NULL;
-- add column "subsonic_last_used_at" to table: "users"
ALTER TABLE `users` ADD COLUMN `subsonic_last_used_at` datetime NULL;
-- add column "subsonic_last_client" to table: "users"
ALTER TABLE `users` ADD COLUMN `subsonic_last_client` text NULL;
-- add column "opds_created_at" to table: "users"
ALTER TABLE `users` ADD COLUMN `opds_created_at` datetime NULL;
-- add column "opds_last_used_at" to table: "users"
ALTER TABLE `users` ADD COLUMN `opds_last_used_at` datetime NULL;
-- add column "opds_last_client" to table: "users"
ALTER TABLE `users` ADD COLUMN `opds_last_client` text NULL;
-- create "new_import_scan_albums" table
CREATE TABLE `new_import_scan_albums` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `folder_path` text NOT NULL, `tagged_artist` text NULL, `tagged_album` text NULL, `classification` text NOT NULL DEFAULT ('unmatched'), `release_group_mbid` text NULL, `artist_mbid` text NULL, `candidates` json NULL, `existing_album_id` integer NULL, `file_count` integer NOT NULL DEFAULT (0), `tagged_year` integer NULL, `format` text NULL, `size` integer NOT NULL DEFAULT (0), `decision` text NOT NULL DEFAULT ('pending'), `decision_release_group_mbid` text NULL, `outcome` text NOT NULL DEFAULT ('pending'), `outcome_message` text NULL, `created_album_id` integer NULL, `import_scan_albums` integer NOT NULL, CONSTRAINT `import_scan_albums_import_scans_albums` FOREIGN KEY (`import_scan_albums`) REFERENCES `import_scans` (`id`) ON DELETE CASCADE);
-- copy rows from old table "import_scan_albums" to new temporary table "new_import_scan_albums"
INSERT INTO `new_import_scan_albums` (`id`, `create_time`, `update_time`, `folder_path`, `tagged_artist`, `tagged_album`, `classification`, `release_group_mbid`, `artist_mbid`, `candidates`, `existing_album_id`, `file_count`, `decision`, `decision_release_group_mbid`, `outcome`, `outcome_message`, `created_album_id`, `import_scan_albums`) SELECT `id`, `create_time`, `update_time`, `folder_path`, `tagged_artist`, `tagged_album`, `classification`, `release_group_mbid`, `artist_mbid`, `candidates`, `existing_album_id`, `file_count`, `decision`, `decision_release_group_mbid`, `outcome`, `outcome_message`, `created_album_id`, `import_scan_albums` FROM `import_scan_albums`;
-- drop "import_scan_albums" table after copying rows
DROP TABLE `import_scan_albums`;
-- rename temporary table "new_import_scan_albums" to "import_scan_albums"
ALTER TABLE `new_import_scan_albums` RENAME TO `import_scan_albums`;
-- create index "importscanalbum_classification" to table: "import_scan_albums"
CREATE INDEX `importscanalbum_classification` ON `import_scan_albums` (`classification`);
-- create index "importscanalbum_decision" to table: "import_scan_albums"
CREATE INDEX `importscanalbum_decision` ON `import_scan_albums` (`decision`);
-- create index "importscanalbum_import_scan_albums" to table: "import_scan_albums"
CREATE INDEX `importscanalbum_import_scan_albums` ON `import_scan_albums` (`import_scan_albums`);
-- create index "importscanalbum_folder_path" to table: "import_scan_albums"
CREATE INDEX `importscanalbum_folder_path` ON `import_scan_albums` (`folder_path`);
-- add column "artist_mbid" to table: "requests"
ALTER TABLE `requests` ADD COLUMN `artist_mbid` text NULL;
-- add column "artist_name" to table: "requests"
ALTER TABLE `requests` ADD COLUMN `artist_name` text NULL;
-- add column "requested_as" to table: "requests"
ALTER TABLE `requests` ADD COLUMN `requested_as` text NULL;
-- create "new_import_scan_books" table
CREATE TABLE `new_import_scan_books` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `file_paths` json NOT NULL, `slot` text NOT NULL, `parsed_title` text NULL, `parsed_author` text NULL, `parsed_isbn` text NULL, `parsed_year` integer NULL, `size` integer NOT NULL DEFAULT (0), `classification` text NOT NULL DEFAULT ('unmatched'), `book_hardcover_id` integer NULL, `candidates` json NULL, `existing_book_id` integer NULL, `decision` text NOT NULL DEFAULT ('pending'), `decision_book_hardcover_id` integer NULL, `outcome` text NOT NULL DEFAULT ('pending'), `outcome_message` text NULL, `created_book_id` integer NULL, `import_scan_books` integer NOT NULL, CONSTRAINT `import_scan_books_import_scans_books` FOREIGN KEY (`import_scan_books`) REFERENCES `import_scans` (`id`) ON DELETE CASCADE);
-- copy rows from old table "import_scan_books" to new temporary table "new_import_scan_books"
INSERT INTO `new_import_scan_books` (`id`, `create_time`, `update_time`, `file_paths`, `slot`, `parsed_title`, `parsed_author`, `parsed_isbn`, `classification`, `book_hardcover_id`, `candidates`, `existing_book_id`, `decision`, `decision_book_hardcover_id`, `outcome`, `outcome_message`, `created_book_id`, `import_scan_books`) SELECT `id`, `create_time`, `update_time`, `file_paths`, `slot`, `parsed_title`, `parsed_author`, `parsed_isbn`, `classification`, `book_hardcover_id`, `candidates`, `existing_book_id`, `decision`, `decision_book_hardcover_id`, `outcome`, `outcome_message`, `created_book_id`, `import_scan_books` FROM `import_scan_books`;
-- drop "import_scan_books" table after copying rows
DROP TABLE `import_scan_books`;
-- rename temporary table "new_import_scan_books" to "import_scan_books"
ALTER TABLE `new_import_scan_books` RENAME TO `import_scan_books`;
-- create index "importscanbook_classification" to table: "import_scan_books"
CREATE INDEX `importscanbook_classification` ON `import_scan_books` (`classification`);
-- create index "importscanbook_decision" to table: "import_scan_books"
CREATE INDEX `importscanbook_decision` ON `import_scan_books` (`decision`);
-- create index "importscanbook_import_scan_books" to table: "import_scan_books"
CREATE INDEX `importscanbook_import_scan_books` ON `import_scan_books` (`import_scan_books`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
-- OPDS tokens are now compared as SHA-256 digests, so a token stored in plaintext
-- before this migration can never authenticate again. Clear it rather than show
-- the user an enabled credential that no reader can use.
UPDATE `users` SET `opds_token` = NULL WHERE `opds_token` IS NOT NULL;
