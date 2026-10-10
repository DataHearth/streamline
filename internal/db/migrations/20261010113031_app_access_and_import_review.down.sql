-- Hand-written: Atlas reverses a SQLite table rebuild by dropping the new_* temp table, which no longer exists on the way down.
PRAGMA foreign_keys = off;
-- rebuild "import_scan_albums" to its previous shape
CREATE TABLE `new_import_scan_albums` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `folder_path` text NOT NULL, `tagged_artist` text NULL, `tagged_album` text NULL, `classification` text NOT NULL DEFAULT ('unmatched'), `release_group_mbid` text NULL, `artist_mbid` text NULL, `candidates` json NULL, `existing_album_id` integer NULL, `file_count` integer NOT NULL DEFAULT (0), `decision` text NOT NULL DEFAULT ('pending'), `decision_release_group_mbid` text NULL, `outcome` text NOT NULL DEFAULT ('pending'), `outcome_message` text NULL, `created_album_id` integer NULL, `import_scan_albums` integer NOT NULL, CONSTRAINT `import_scan_albums_import_scans_albums` FOREIGN KEY (`import_scan_albums`) REFERENCES `import_scans` (`id`) ON DELETE CASCADE);
INSERT INTO `new_import_scan_albums` (`id`, `create_time`, `update_time`, `folder_path`, `tagged_artist`, `tagged_album`, `classification`, `release_group_mbid`, `artist_mbid`, `candidates`, `existing_album_id`, `file_count`, `decision`, `decision_release_group_mbid`, `outcome`, `outcome_message`, `created_album_id`, `import_scan_albums`) SELECT `id`, `create_time`, `update_time`, `folder_path`, `tagged_artist`, `tagged_album`, `classification`, `release_group_mbid`, `artist_mbid`, `candidates`, `existing_album_id`, `file_count`, `decision`, `decision_release_group_mbid`, `outcome`, `outcome_message`, `created_album_id`, `import_scan_albums` FROM `import_scan_albums`;
DROP TABLE `import_scan_albums`;
ALTER TABLE `new_import_scan_albums` RENAME TO `import_scan_albums`;
CREATE INDEX `importscanalbum_classification` ON `import_scan_albums` (`classification`);
CREATE INDEX `importscanalbum_decision` ON `import_scan_albums` (`decision`);
CREATE INDEX `importscanalbum_import_scan_albums` ON `import_scan_albums` (`import_scan_albums`);
CREATE INDEX `importscanalbum_folder_path` ON `import_scan_albums` (`folder_path`);
-- rebuild "import_scan_books" to its previous shape
CREATE TABLE `new_import_scan_books` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `file_paths` json NOT NULL, `slot` text NOT NULL, `parsed_title` text NULL, `parsed_author` text NULL, `parsed_isbn` text NULL, `classification` text NOT NULL DEFAULT ('unmatched'), `book_hardcover_id` integer NULL, `candidates` json NULL, `existing_book_id` integer NULL, `decision` text NOT NULL DEFAULT ('pending'), `decision_book_hardcover_id` integer NULL, `outcome` text NOT NULL DEFAULT ('pending'), `outcome_message` text NULL, `created_book_id` integer NULL, `import_scan_books` integer NOT NULL, CONSTRAINT `import_scan_books_import_scans_books` FOREIGN KEY (`import_scan_books`) REFERENCES `import_scans` (`id`) ON DELETE CASCADE);
INSERT INTO `new_import_scan_books` (`id`, `create_time`, `update_time`, `file_paths`, `slot`, `parsed_title`, `parsed_author`, `parsed_isbn`, `classification`, `book_hardcover_id`, `candidates`, `existing_book_id`, `decision`, `decision_book_hardcover_id`, `outcome`, `outcome_message`, `created_book_id`, `import_scan_books`) SELECT `id`, `create_time`, `update_time`, `file_paths`, `slot`, `parsed_title`, `parsed_author`, `parsed_isbn`, `classification`, `book_hardcover_id`, `candidates`, `existing_book_id`, `decision`, `decision_book_hardcover_id`, `outcome`, `outcome_message`, `created_book_id`, `import_scan_books` FROM `import_scan_books`;
DROP TABLE `import_scan_books`;
ALTER TABLE `new_import_scan_books` RENAME TO `import_scan_books`;
CREATE INDEX `importscanbook_classification` ON `import_scan_books` (`classification`);
CREATE INDEX `importscanbook_decision` ON `import_scan_books` (`decision`);
CREATE INDEX `importscanbook_import_scan_books` ON `import_scan_books` (`import_scan_books`);
ALTER TABLE `requests` DROP COLUMN `requested_as`;
ALTER TABLE `requests` DROP COLUMN `artist_name`;
ALTER TABLE `requests` DROP COLUMN `artist_mbid`;
ALTER TABLE `users` DROP COLUMN `opds_last_client`;
ALTER TABLE `users` DROP COLUMN `opds_last_used_at`;
ALTER TABLE `users` DROP COLUMN `opds_created_at`;
ALTER TABLE `users` DROP COLUMN `subsonic_last_client`;
ALTER TABLE `users` DROP COLUMN `subsonic_last_used_at`;
ALTER TABLE `users` DROP COLUMN `subsonic_created_at`;
ALTER TABLE `import_scans` DROP COLUMN `failure_code`;
PRAGMA foreign_keys = on;
