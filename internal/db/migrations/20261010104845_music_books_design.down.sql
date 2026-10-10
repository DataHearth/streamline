-- Hand-written: Atlas reverses a SQLite table rebuild by dropping the new_* temp table, which no longer exists on the way down.
PRAGMA foreign_keys = off;
-- rebuild "books" to its previous shape
CREATE TABLE `new_books` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `hardcover_id` integer NOT NULL, `title` text NOT NULL, `sort_title` text NULL, `release_date` datetime NULL, `overview` text NULL, `series_name` text NULL, `series_position` text NULL, `ebook_monitored` bool NOT NULL DEFAULT (false), `ebook_status` text NOT NULL DEFAULT ('skipped'), `ebook_grab_failures` integer NOT NULL DEFAULT (0), `ebook_last_search_at` datetime NULL, `audiobook_monitored` bool NOT NULL DEFAULT (false), `audiobook_status` text NOT NULL DEFAULT ('skipped'), `audiobook_grab_failures` integer NOT NULL DEFAULT (0), `audiobook_last_search_at` datetime NULL, `author_books` integer NOT NULL, CONSTRAINT `books_authors_books` FOREIGN KEY (`author_books`) REFERENCES `authors` (`id`) ON DELETE CASCADE);
INSERT INTO `new_books` (`id`, `create_time`, `update_time`, `hardcover_id`, `title`, `sort_title`, `release_date`, `overview`, `series_position`, `ebook_monitored`, `ebook_status`, `ebook_grab_failures`, `ebook_last_search_at`, `audiobook_monitored`, `audiobook_status`, `audiobook_grab_failures`, `audiobook_last_search_at`, `author_books`) SELECT `id`, `create_time`, `update_time`, `hardcover_id`, `title`, `sort_title`, `release_date`, `overview`, `series_position`, `ebook_monitored`, `ebook_status`, `ebook_grab_failures`, `ebook_last_search_at`, `audiobook_monitored`, `audiobook_status`, `audiobook_grab_failures`, `audiobook_last_search_at`, (SELECT `author_contributions` FROM `book_contributions` WHERE `book_contributions` = `books`.`id` AND `role` = 'author' ORDER BY `order`, `id` LIMIT 1) FROM `books` WHERE (SELECT `author_contributions` FROM `book_contributions` WHERE `book_contributions` = `books`.`id` AND `role` = 'author' ORDER BY `order`, `id` LIMIT 1) IS NOT NULL;
DROP TABLE `books`;
ALTER TABLE `new_books` RENAME TO `books`;
CREATE UNIQUE INDEX `books_hardcover_id_key` ON `books` (`hardcover_id`);
CREATE INDEX `book_author_books` ON `books` (`author_books`);
-- rebuild "authors" to its previous shape
CREATE TABLE `new_authors` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `hardcover_id` integer NOT NULL, `name` text NOT NULL, `sort_name` text NULL, `overview` text NULL, `monitored` bool NOT NULL DEFAULT (true), `folder` text NULL, `monitor_policy` text NOT NULL DEFAULT ('all'), `want_kinds` text NOT NULL DEFAULT ('ebook'), `ebook_quality_profile` text NULL, `audiobook_quality_profile` text NULL, `last_refreshed_at` datetime NULL);
INSERT INTO `new_authors` (`id`, `create_time`, `update_time`, `hardcover_id`, `name`, `sort_name`) SELECT `id`, `create_time`, `update_time`, `hardcover_id`, `name`, `sort_name` FROM `authors`;
DROP TABLE `authors`;
ALTER TABLE `new_authors` RENAME TO `authors`;
CREATE UNIQUE INDEX `authors_hardcover_id_key` ON `authors` (`hardcover_id`);
-- rebuild "albums" to its previous shape
CREATE TABLE `new_albums` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `mbid` text NOT NULL, `release_mbid` text NULL, `title` text NOT NULL, `type` text NOT NULL DEFAULT ('album'), `release_date` datetime NULL, `monitored` bool NOT NULL DEFAULT (true), `grab_failures` integer NOT NULL DEFAULT (0), `last_search_at` datetime NULL, `status` text NOT NULL DEFAULT ('wanted'), `artist_albums` integer NOT NULL, `barcode` text NULL, CONSTRAINT `albums_artists_albums` FOREIGN KEY (`artist_albums`) REFERENCES `artists` (`id`) ON DELETE CASCADE);
INSERT INTO `new_albums` (`id`, `create_time`, `update_time`, `mbid`, `release_mbid`, `title`, `type`, `release_date`, `monitored`, `grab_failures`, `last_search_at`, `status`, `artist_albums`, `barcode`) SELECT `id`, `create_time`, `update_time`, `mbid`, `release_mbid`, `title`, `type`, `release_date`, `monitored`, `grab_failures`, `last_search_at`, `status`, `artist_albums`, `barcode` FROM `albums`;
DROP TABLE `albums`;
ALTER TABLE `new_albums` RENAME TO `albums`;
CREATE UNIQUE INDEX `albums_mbid_key` ON `albums` (`mbid`);
CREATE INDEX `album_artist_albums` ON `albums` (`artist_albums`);
CREATE INDEX `album_status` ON `albums` (`status`);
-- rebuild "artists" to its previous shape
CREATE TABLE `new_artists` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `mbid` text NOT NULL, `name` text NOT NULL, `sort_name` text NULL, `overview` text NULL, `monitored` bool NOT NULL DEFAULT (true), `path` text NULL, `quality_profile` text NULL, `last_refreshed_at` datetime NULL);
INSERT INTO `new_artists` (`id`, `create_time`, `update_time`, `mbid`, `name`, `sort_name`, `overview`, `monitored`, `path`, `quality_profile`, `last_refreshed_at`) SELECT `id`, `create_time`, `update_time`, `mbid`, `name`, `sort_name`, `overview`, (`monitor` <> 'none'), `path`, `quality_profile`, `last_refreshed_at` FROM `artists`;
DROP TABLE `artists`;
ALTER TABLE `new_artists` RENAME TO `artists`;
CREATE UNIQUE INDEX `artists_mbid_key` ON `artists` (`mbid`);
-- rebuild "tracks" to its previous shape
CREATE TABLE `new_tracks` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `mbid` text NULL, `title` text NOT NULL, `disc` integer NOT NULL DEFAULT (1), `position` integer NOT NULL, `duration` integer NULL DEFAULT (0), `album_tracks` integer NOT NULL, CONSTRAINT `tracks_albums_tracks` FOREIGN KEY (`album_tracks`) REFERENCES `albums` (`id`) ON DELETE CASCADE);
INSERT INTO `new_tracks` (`id`, `create_time`, `update_time`, `mbid`, `title`, `disc`, `position`, `duration`, `album_tracks`) SELECT `id`, `create_time`, `update_time`, `mbid`, `title`, `disc`, `position`, `duration`, `album_tracks` FROM `tracks`;
DROP TABLE `tracks`;
ALTER TABLE `new_tracks` RENAME TO `tracks`;
CREATE INDEX `track_album_tracks` ON `tracks` (`album_tracks`);
-- rebuild "download_records" to its previous shape
CREATE TABLE `new_download_records` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `title` text NOT NULL, `quality` text NULL, `size` integer NULL, `status` text NOT NULL DEFAULT ('downloading'), `torrent_hash` text NULL, `release_group` text NULL, `save_path` text NULL, `import_attempts` integer NOT NULL DEFAULT (0), `failure_reason` text NULL, `imported_at` datetime NULL, `indexer_name` text NULL, `download_client_name` text NULL, `replace_mode` text NOT NULL DEFAULT ('none'), `hold_reasons` json NULL, `verification_bypassed` bool NOT NULL DEFAULT (false), `selected_files` json NULL, `selected_bytes` integer NULL, `selection_state` text NOT NULL DEFAULT ('skipped'), `album_download_records` integer NULL, `book_download_records` integer NULL, `episode_download_records` integer NULL, `movie_download_records` integer NULL, `book_kind` text NULL, CONSTRAINT `download_records_albums_download_records` FOREIGN KEY (`album_download_records`) REFERENCES `albums` (`id`) ON DELETE CASCADE, CONSTRAINT `download_records_books_download_records` FOREIGN KEY (`book_download_records`) REFERENCES `books` (`id`) ON DELETE CASCADE, CONSTRAINT `download_records_episodes_anchored_download_records` FOREIGN KEY (`episode_download_records`) REFERENCES `episodes` (`id`) ON DELETE CASCADE, CONSTRAINT `download_records_movies_download_records` FOREIGN KEY (`movie_download_records`) REFERENCES `movies` (`id`) ON DELETE CASCADE);
INSERT INTO `new_download_records` (`id`, `create_time`, `update_time`, `title`, `quality`, `size`, `status`, `torrent_hash`, `release_group`, `save_path`, `import_attempts`, `failure_reason`, `imported_at`, `indexer_name`, `download_client_name`, `replace_mode`, `hold_reasons`, `verification_bypassed`, `selected_files`, `selected_bytes`, `selection_state`, `album_download_records`, `book_download_records`, `episode_download_records`, `movie_download_records`, `book_kind`) SELECT `id`, `create_time`, `update_time`, `title`, `quality`, `size`, `status`, `torrent_hash`, `release_group`, `save_path`, `import_attempts`, `failure_reason`, `imported_at`, `indexer_name`, `download_client_name`, `replace_mode`, `hold_reasons`, `verification_bypassed`, `selected_files`, `selected_bytes`, `selection_state`, `album_download_records`, `book_download_records`, `episode_download_records`, `movie_download_records`, `book_kind` FROM `download_records`;
DROP TABLE `download_records`;
ALTER TABLE `new_download_records` RENAME TO `download_records`;
CREATE INDEX `downloadrecord_selection_state` ON `download_records` (`selection_state`);
CREATE INDEX `downloadrecord_status` ON `download_records` (`status`);
CREATE INDEX `downloadrecord_torrent_hash` ON `download_records` (`torrent_hash`);
CREATE INDEX `downloadrecord_update_time_id` ON `download_records` (`update_time`, `id`);
CREATE INDEX `downloadrecord_movie_download_records` ON `download_records` (`movie_download_records`);
CREATE INDEX `downloadrecord_episode_download_records` ON `download_records` (`episode_download_records`);
-- rebuild "requests" to its previous shape
CREATE TABLE `new_requests` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `media_type` text NOT NULL, `media_id` integer NULL DEFAULT (0), `media_mbid` text NULL, `book_kind` text NULL, `title` text NOT NULL, `status` text NOT NULL DEFAULT ('pending'), `reason` text NULL, `quality_profile` text NULL, `request_approved_by` integer NULL, `user_requests` integer NOT NULL, CONSTRAINT `requests_users_approved_by` FOREIGN KEY (`request_approved_by`) REFERENCES `users` (`id`) ON DELETE SET NULL, CONSTRAINT `requests_users_requests` FOREIGN KEY (`user_requests`) REFERENCES `users` (`id`) ON DELETE CASCADE);
INSERT INTO `new_requests` (`id`, `create_time`, `update_time`, `media_type`, `media_id`, `media_mbid`, `title`, `status`, `reason`, `quality_profile`, `request_approved_by`, `user_requests`) SELECT `id`, `create_time`, `update_time`, `media_type`, `media_id`, `media_mbid`, `title`, `status`, `reason`, `quality_profile`, `request_approved_by`, `user_requests` FROM `requests`;
DROP TABLE `requests`;
ALTER TABLE `new_requests` RENAME TO `requests`;
CREATE UNIQUE INDEX `request_media_type_media_id` ON `requests` (`media_type`, `media_id`) WHERE status IN ('pending', 'approved', 'available') AND media_id <> 0;
CREATE UNIQUE INDEX `request_media_type_media_mbid` ON `requests` (`media_type`, `media_mbid`) WHERE status IN ('pending', 'approved', 'available') AND media_mbid <> '';
CREATE INDEX `request_user_requests` ON `requests` (`user_requests`);
CREATE INDEX `request_request_approved_by` ON `requests` (`request_approved_by`);
-- rebuild "import_scan_books" to its previous shape
CREATE TABLE `new_import_scan_books` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `file_paths` json NOT NULL, `slot` text NOT NULL, `parsed_title` text NULL, `parsed_author` text NULL, `parsed_isbn` text NULL, `classification` text NOT NULL DEFAULT ('unmatched'), `book_hardcover_id` integer NULL, `author_hardcover_id` integer NULL, `candidates` json NULL, `existing_book_id` integer NULL, `decision` text NOT NULL DEFAULT ('pending'), `decision_book_hardcover_id` integer NULL, `outcome` text NOT NULL DEFAULT ('pending'), `outcome_message` text NULL, `created_book_id` integer NULL, `import_scan_books` integer NOT NULL, CONSTRAINT `import_scan_books_import_scans_books` FOREIGN KEY (`import_scan_books`) REFERENCES `import_scans` (`id`) ON DELETE CASCADE);
INSERT INTO `new_import_scan_books` (`id`, `create_time`, `update_time`, `file_paths`, `slot`, `parsed_title`, `parsed_author`, `parsed_isbn`, `classification`, `book_hardcover_id`, `candidates`, `existing_book_id`, `decision`, `decision_book_hardcover_id`, `outcome`, `outcome_message`, `created_book_id`, `import_scan_books`) SELECT `id`, `create_time`, `update_time`, `file_paths`, `slot`, `parsed_title`, `parsed_author`, `parsed_isbn`, `classification`, `book_hardcover_id`, `candidates`, `existing_book_id`, `decision`, `decision_book_hardcover_id`, `outcome`, `outcome_message`, `created_book_id`, `import_scan_books` FROM `import_scan_books`;
DROP TABLE `import_scan_books`;
ALTER TABLE `new_import_scan_books` RENAME TO `import_scan_books`;
CREATE INDEX `importscanbook_classification` ON `import_scan_books` (`classification`);
CREATE INDEX `importscanbook_decision` ON `import_scan_books` (`decision`);
CREATE INDEX `importscanbook_import_scan_books` ON `import_scan_books` (`import_scan_books`);
DROP TABLE `download_record_albums`;
DROP TABLE `music_credits`;
DROP TABLE `book_contributions`;
DROP TABLE `book_editions`;
DROP TABLE `book_series`;
DROP TABLE `artist_members`;
PRAGMA foreign_keys = on;
