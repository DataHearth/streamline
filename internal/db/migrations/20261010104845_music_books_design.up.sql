-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_albums" table
CREATE TABLE `new_albums` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `mbid` text NOT NULL, `release_mbid` text NULL, `barcode` text NULL, `title` text NOT NULL, `type` text NOT NULL DEFAULT ('album'), `release_date` datetime NULL, `monitored` bool NOT NULL DEFAULT (true), `grab_failures` integer NOT NULL DEFAULT (0), `last_search_at` datetime NULL, `status` text NOT NULL DEFAULT ('wanted'), `label` text NULL, `catalog_number` text NULL, `country` text NULL, `media` text NULL, `studio` text NULL, `metadata_fetched_at` datetime NULL, `credits_fetched_at` datetime NULL, `artist_albums` integer NOT NULL, CONSTRAINT `albums_artists_albums` FOREIGN KEY (`artist_albums`) REFERENCES `artists` (`id`) ON DELETE CASCADE);
-- copy rows from old table "albums" to new temporary table "new_albums"
INSERT INTO `new_albums` (`id`, `create_time`, `update_time`, `mbid`, `release_mbid`, `barcode`, `title`, `type`, `release_date`, `monitored`, `grab_failures`, `last_search_at`, `status`, `artist_albums`) SELECT `id`, `create_time`, `update_time`, `mbid`, `release_mbid`, `barcode`, `title`, `type`, `release_date`, `monitored`, `grab_failures`, `last_search_at`, `status`, `artist_albums` FROM `albums`;
-- drop "albums" table after copying rows
DROP TABLE `albums`;
-- rename temporary table "new_albums" to "albums"
ALTER TABLE `new_albums` RENAME TO `albums`;
-- create index "albums_mbid_key" to table: "albums"
CREATE UNIQUE INDEX `albums_mbid_key` ON `albums` (`mbid`);
-- create index "album_artist_albums" to table: "albums"
CREATE INDEX `album_artist_albums` ON `albums` (`artist_albums`);
-- create index "album_status" to table: "albums"
CREATE INDEX `album_status` ON `albums` (`status`);
-- create index "album_metadata_fetched_at" to table: "albums"
CREATE INDEX `album_metadata_fetched_at` ON `albums` (`metadata_fetched_at`);
-- create "new_artists" table
CREATE TABLE `new_artists` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `mbid` text NOT NULL, `name` text NOT NULL, `sort_name` text NULL, `overview` text NULL, `overview_source` text NULL, `overview_fr` text NULL, `overview_source_fr` text NULL, `monitor` text NOT NULL DEFAULT ('all'), `type` text NULL, `origin` text NULL, `since` integer NULL, `genre` text NULL, `deezer_id` integer NULL, `wikidata_id` text NULL, `path` text NULL, `quality_profile` text NULL, `last_refreshed_at` datetime NULL, `details_fetched_at` datetime NULL);
-- copy rows from old table "artists" to new temporary table "new_artists"
INSERT INTO `new_artists` (`id`, `create_time`, `update_time`, `mbid`, `name`, `sort_name`, `overview`, `path`, `quality_profile`, `last_refreshed_at`) SELECT `id`, `create_time`, `update_time`, `mbid`, `name`, `sort_name`, `overview`, `path`, `quality_profile`, `last_refreshed_at` FROM `artists`;
-- drop "artists" table after copying rows
DROP TABLE `artists`;
-- rename temporary table "new_artists" to "artists"
ALTER TABLE `new_artists` RENAME TO `artists`;
-- create index "artists_mbid_key" to table: "artists"
CREATE UNIQUE INDEX `artists_mbid_key` ON `artists` (`mbid`);
-- create index "artist_create_time" to table: "artists"
CREATE INDEX `artist_create_time` ON `artists` (`create_time`);
-- create "new_tracks" table
CREATE TABLE `new_tracks` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `mbid` text NULL, `title` text NOT NULL, `disc` integer NOT NULL DEFAULT (1), `position` integer NOT NULL, `duration` integer NULL DEFAULT (0), `bonus` bool NOT NULL DEFAULT (false), `album_tracks` integer NOT NULL, CONSTRAINT `tracks_albums_tracks` FOREIGN KEY (`album_tracks`) REFERENCES `albums` (`id`) ON DELETE CASCADE);
-- copy rows from old table "tracks" to new temporary table "new_tracks"
INSERT INTO `new_tracks` (`id`, `create_time`, `update_time`, `mbid`, `title`, `disc`, `position`, `duration`, `album_tracks`) SELECT `id`, `create_time`, `update_time`, `mbid`, `title`, `disc`, `position`, `duration`, `album_tracks` FROM `tracks`;
-- drop "tracks" table after copying rows
DROP TABLE `tracks`;
-- rename temporary table "new_tracks" to "tracks"
ALTER TABLE `new_tracks` RENAME TO `tracks`;
-- create index "track_album_tracks" to table: "tracks"
CREATE INDEX `track_album_tracks` ON `tracks` (`album_tracks`);
-- create "new_download_records" table
CREATE TABLE `new_download_records` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `title` text NOT NULL, `quality` text NULL, `size` integer NULL, `status` text NOT NULL DEFAULT ('downloading'), `torrent_hash` text NULL, `release_group` text NULL, `save_path` text NULL, `import_attempts` integer NOT NULL DEFAULT (0), `failure_reason` text NULL, `imported_at` datetime NULL, `indexer_name` text NULL, `download_client_name` text NULL, `replace_mode` text NOT NULL DEFAULT ('none'), `hold_reasons` json NULL, `verification_bypassed` bool NOT NULL DEFAULT (false), `selected_files` json NULL, `selected_bytes` integer NULL, `selection_state` text NOT NULL DEFAULT ('skipped'), `book_kind` text NULL, `album_download_records` integer NULL, `artist_download_records` integer NULL, `book_download_records` integer NULL, `episode_download_records` integer NULL, `movie_download_records` integer NULL, CONSTRAINT `download_records_albums_download_records` FOREIGN KEY (`album_download_records`) REFERENCES `albums` (`id`) ON DELETE CASCADE, CONSTRAINT `download_records_artists_download_records` FOREIGN KEY (`artist_download_records`) REFERENCES `artists` (`id`) ON DELETE CASCADE, CONSTRAINT `download_records_books_download_records` FOREIGN KEY (`book_download_records`) REFERENCES `books` (`id`) ON DELETE CASCADE, CONSTRAINT `download_records_episodes_anchored_download_records` FOREIGN KEY (`episode_download_records`) REFERENCES `episodes` (`id`) ON DELETE CASCADE, CONSTRAINT `download_records_movies_download_records` FOREIGN KEY (`movie_download_records`) REFERENCES `movies` (`id`) ON DELETE CASCADE);
-- copy rows from old table "download_records" to new temporary table "new_download_records"
INSERT INTO `new_download_records` (`id`, `create_time`, `update_time`, `title`, `quality`, `size`, `status`, `torrent_hash`, `release_group`, `save_path`, `import_attempts`, `failure_reason`, `imported_at`, `indexer_name`, `download_client_name`, `replace_mode`, `hold_reasons`, `verification_bypassed`, `selected_files`, `selected_bytes`, `selection_state`, `book_kind`, `album_download_records`, `book_download_records`, `episode_download_records`, `movie_download_records`) SELECT `id`, `create_time`, `update_time`, `title`, `quality`, `size`, `status`, `torrent_hash`, `release_group`, `save_path`, `import_attempts`, `failure_reason`, `imported_at`, `indexer_name`, `download_client_name`, `replace_mode`, `hold_reasons`, `verification_bypassed`, `selected_files`, `selected_bytes`, `selection_state`, `book_kind`, `album_download_records`, `book_download_records`, `episode_download_records`, `movie_download_records` FROM `download_records`;
-- drop "download_records" table after copying rows
DROP TABLE `download_records`;
-- rename temporary table "new_download_records" to "download_records"
ALTER TABLE `new_download_records` RENAME TO `download_records`;
-- create index "downloadrecord_selection_state" to table: "download_records"
CREATE INDEX `downloadrecord_selection_state` ON `download_records` (`selection_state`);
-- create index "downloadrecord_status" to table: "download_records"
CREATE INDEX `downloadrecord_status` ON `download_records` (`status`);
-- create index "downloadrecord_torrent_hash" to table: "download_records"
CREATE INDEX `downloadrecord_torrent_hash` ON `download_records` (`torrent_hash`);
-- create index "downloadrecord_update_time_id" to table: "download_records"
CREATE INDEX `downloadrecord_update_time_id` ON `download_records` (`update_time`, `id`);
-- create index "downloadrecord_movie_download_records" to table: "download_records"
CREATE INDEX `downloadrecord_movie_download_records` ON `download_records` (`movie_download_records`);
-- create index "downloadrecord_episode_download_records" to table: "download_records"
CREATE INDEX `downloadrecord_episode_download_records` ON `download_records` (`episode_download_records`);
-- create index "downloadrecord_artist_download_records" to table: "download_records"
CREATE INDEX `downloadrecord_artist_download_records` ON `download_records` (`artist_download_records`);
-- create "new_authors" table
CREATE TABLE `new_authors` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `hardcover_id` integer NOT NULL, `name` text NOT NULL, `sort_name` text NULL, `image_source` text NULL);
-- copy rows from old table "authors" to new temporary table "new_authors"
INSERT INTO `new_authors` (`id`, `create_time`, `update_time`, `hardcover_id`, `name`, `sort_name`) SELECT `id`, `create_time`, `update_time`, `hardcover_id`, `name`, `sort_name` FROM `authors`;
-- drop "authors" table after copying rows
DROP TABLE `authors`;
-- rename temporary table "new_authors" to "authors"
ALTER TABLE `new_authors` RENAME TO `authors`;
-- create index "authors_hardcover_id_key" to table: "authors"
CREATE UNIQUE INDEX `authors_hardcover_id_key` ON `authors` (`hardcover_id`);
-- create index "author_name" to table: "authors"
CREATE INDEX `author_name` ON `authors` (`name`);
-- create "new_books" table
CREATE TABLE `new_books` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `hardcover_id` integer NOT NULL, `title` text NOT NULL, `original_title` text NULL, `sort_title` text NULL, `author_name` text NULL, `kind` text NOT NULL DEFAULT ('novel'), `genre` text NULL, `rating_tenths` integer NULL, `release_year` integer NULL, `release_date` datetime NULL, `overview` text NULL, `preferred_language` text NOT NULL DEFAULT ('en'), `quality_profile` text NULL, `series_position` real NULL, `ebook_monitored` bool NOT NULL DEFAULT (false), `ebook_status` text NOT NULL DEFAULT ('skipped'), `ebook_grab_failures` integer NOT NULL DEFAULT (0), `ebook_last_search_at` datetime NULL, `ebook_replacing_language` text NULL, `audiobook_monitored` bool NOT NULL DEFAULT (false), `audiobook_status` text NOT NULL DEFAULT ('skipped'), `audiobook_grab_failures` integer NOT NULL DEFAULT (0), `audiobook_last_search_at` datetime NULL, `audiobook_replacing_language` text NULL, `last_refreshed_at` datetime NULL, `book_ebook_edition` integer NULL, `book_audiobook_edition` integer NULL, `book_series_volumes` integer NULL, CONSTRAINT `books_book_editions_ebook_edition` FOREIGN KEY (`book_ebook_edition`) REFERENCES `book_editions` (`id`) ON DELETE SET NULL, CONSTRAINT `books_book_editions_audiobook_edition` FOREIGN KEY (`book_audiobook_edition`) REFERENCES `book_editions` (`id`) ON DELETE SET NULL, CONSTRAINT `books_book_series_volumes` FOREIGN KEY (`book_series_volumes`) REFERENCES `book_series` (`id`) ON DELETE CASCADE);
-- copy rows from old table "books" to new temporary table "new_books"
INSERT INTO `new_books` (`id`, `create_time`, `update_time`, `hardcover_id`, `title`, `sort_title`, `release_date`, `overview`, `series_position`, `ebook_monitored`, `ebook_status`, `ebook_grab_failures`, `ebook_last_search_at`, `audiobook_monitored`, `audiobook_status`, `audiobook_grab_failures`, `audiobook_last_search_at`) SELECT `id`, `create_time`, `update_time`, `hardcover_id`, `title`, `sort_title`, `release_date`, `overview`, `series_position`, `ebook_monitored`, `ebook_status`, `ebook_grab_failures`, `ebook_last_search_at`, `audiobook_monitored`, `audiobook_status`, `audiobook_grab_failures`, `audiobook_last_search_at` FROM `books`;
-- drop "books" table after copying rows
DROP TABLE `books`;
-- rename temporary table "new_books" to "books"
ALTER TABLE `new_books` RENAME TO `books`;
-- create index "books_hardcover_id_key" to table: "books"
CREATE UNIQUE INDEX `books_hardcover_id_key` ON `books` (`hardcover_id`);
-- create index "book_book_series_volumes" to table: "books"
CREATE INDEX `book_book_series_volumes` ON `books` (`book_series_volumes`);
-- create index "book_series_position_book_series_volumes" to table: "books"
CREATE INDEX `book_series_position_book_series_volumes` ON `books` (`series_position`, `book_series_volumes`);
-- create index "book_kind" to table: "books"
CREATE INDEX `book_kind` ON `books` (`kind`);
-- create index "book_author_name" to table: "books"
CREATE INDEX `book_author_name` ON `books` (`author_name`);
-- create "new_requests" table
CREATE TABLE `new_requests` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `media_type` text NOT NULL, `media_id` integer NULL DEFAULT (0), `media_mbid` text NULL, `title` text NOT NULL, `status` text NOT NULL DEFAULT ('pending'), `reason` text NULL, `quality_profile` text NULL, `request_approved_by` integer NULL, `user_requests` integer NOT NULL, CONSTRAINT `requests_users_approved_by` FOREIGN KEY (`request_approved_by`) REFERENCES `users` (`id`) ON DELETE SET NULL, CONSTRAINT `requests_users_requests` FOREIGN KEY (`user_requests`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- copy rows from old table "requests" to new temporary table "new_requests"
INSERT INTO `new_requests` (`id`, `create_time`, `update_time`, `media_type`, `media_id`, `media_mbid`, `title`, `status`, `reason`, `quality_profile`, `request_approved_by`, `user_requests`) SELECT `id`, `create_time`, `update_time`, `media_type`, `media_id`, `media_mbid`, `title`, `status`, `reason`, `quality_profile`, `request_approved_by`, `user_requests` FROM `requests`;
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
-- create "new_import_scan_books" table
CREATE TABLE `new_import_scan_books` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `file_paths` json NOT NULL, `slot` text NOT NULL, `parsed_title` text NULL, `parsed_author` text NULL, `parsed_isbn` text NULL, `classification` text NOT NULL DEFAULT ('unmatched'), `book_hardcover_id` integer NULL, `candidates` json NULL, `existing_book_id` integer NULL, `decision` text NOT NULL DEFAULT ('pending'), `decision_book_hardcover_id` integer NULL, `outcome` text NOT NULL DEFAULT ('pending'), `outcome_message` text NULL, `created_book_id` integer NULL, `import_scan_books` integer NOT NULL, CONSTRAINT `import_scan_books_import_scans_books` FOREIGN KEY (`import_scan_books`) REFERENCES `import_scans` (`id`) ON DELETE CASCADE);
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
-- create "artist_members" table
CREATE TABLE `artist_members` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `name` text NOT NULL, `mbid` text NULL, `instruments` text NULL, `from_year` integer NULL, `to_year` integer NULL, `ordinal` integer NOT NULL, `artist_members` integer NOT NULL, CONSTRAINT `artist_members_artists_members` FOREIGN KEY (`artist_members`) REFERENCES `artists` (`id`) ON DELETE CASCADE);
-- create index "artistmember_artist_members" to table: "artist_members"
CREATE INDEX `artistmember_artist_members` ON `artist_members` (`artist_members`);
-- create "book_contributions" table
CREATE TABLE `book_contributions` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `role` text NOT NULL, `language` text NULL, `order` integer NOT NULL DEFAULT (0), `author_contributions` integer NOT NULL, `book_contributions` integer NULL, `book_series_contributions` integer NULL, CONSTRAINT `book_contributions_authors_contributions` FOREIGN KEY (`author_contributions`) REFERENCES `authors` (`id`) ON DELETE CASCADE, CONSTRAINT `book_contributions_books_contributions` FOREIGN KEY (`book_contributions`) REFERENCES `books` (`id`) ON DELETE CASCADE, CONSTRAINT `book_contributions_book_series_contributions` FOREIGN KEY (`book_series_contributions`) REFERENCES `book_series` (`id`) ON DELETE CASCADE);
-- create index "bookcontribution_author_contributions" to table: "book_contributions"
CREATE INDEX `bookcontribution_author_contributions` ON `book_contributions` (`author_contributions`);
-- create index "bookcontribution_book_contributions" to table: "book_contributions"
CREATE INDEX `bookcontribution_book_contributions` ON `book_contributions` (`book_contributions`);
-- create index "bookcontribution_book_series_contributions" to table: "book_contributions"
CREATE INDEX `bookcontribution_book_series_contributions` ON `book_contributions` (`book_series_contributions`);
-- create "book_editions" table
CREATE TABLE `book_editions` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `hardcover_edition_id` integer NOT NULL, `language` text NOT NULL, `title` text NOT NULL, `publisher` text NOT NULL DEFAULT (''), `year` integer NOT NULL DEFAULT (0), `format` text NOT NULL, `original` bool NOT NULL DEFAULT (false), `pages` integer NULL, `duration_seconds` integer NULL, `narrator` text NULL, `translator` text NULL, `isbn_13` text NULL, `asin` text NULL, `popularity` integer NOT NULL DEFAULT (0), `book_editions` integer NOT NULL, CONSTRAINT `book_editions_books_editions` FOREIGN KEY (`book_editions`) REFERENCES `books` (`id`) ON DELETE CASCADE);
-- create index "bookedition_hardcover_edition_id_book_editions" to table: "book_editions"
CREATE UNIQUE INDEX `bookedition_hardcover_edition_id_book_editions` ON `book_editions` (`hardcover_edition_id`, `book_editions`);
-- create index "bookedition_language_format_publisher_book_editions" to table: "book_editions"
CREATE INDEX `bookedition_language_format_publisher_book_editions` ON `book_editions` (`language`, `format`, `publisher`, `book_editions`);
-- create index "bookedition_isbn_13" to table: "book_editions"
CREATE INDEX `bookedition_isbn_13` ON `book_editions` (`isbn_13`);
-- create "book_series" table
CREATE TABLE `book_series` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `hardcover_id` integer NOT NULL, `title` text NOT NULL, `original_title` text NULL, `sort_title` text NULL, `overview` text NULL, `author_name` text NULL, `kind` text NOT NULL DEFAULT ('novel'), `rating_tenths` integer NULL, `ongoing` bool NOT NULL DEFAULT (false), `since` integer NULL, `monitor` text NOT NULL DEFAULT ('all'), `quality_profile` text NULL, `edition_language` text NULL, `edition_publisher` text NULL, `last_refreshed_at` datetime NULL);
-- create index "book_series_hardcover_id_key" to table: "book_series"
CREATE UNIQUE INDEX `book_series_hardcover_id_key` ON `book_series` (`hardcover_id`);
-- create "music_credits" table
CREATE TABLE `music_credits` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `kind` text NOT NULL, `role` text NULL, `name` text NOT NULL, `mbid` text NULL, `instruments` text NULL, `guest` bool NOT NULL DEFAULT (false), `ordinal` integer NOT NULL, `album_credits` integer NULL, `track_credits` integer NULL, CONSTRAINT `music_credits_albums_credits` FOREIGN KEY (`album_credits`) REFERENCES `albums` (`id`) ON DELETE CASCADE, CONSTRAINT `music_credits_tracks_credits` FOREIGN KEY (`track_credits`) REFERENCES `tracks` (`id`) ON DELETE CASCADE);
-- create index "musiccredit_album_credits" to table: "music_credits"
CREATE INDEX `musiccredit_album_credits` ON `music_credits` (`album_credits`);
-- create index "musiccredit_track_credits" to table: "music_credits"
CREATE INDEX `musiccredit_track_credits` ON `music_credits` (`track_credits`);
-- create index "musiccredit_mbid" to table: "music_credits"
CREATE INDEX `musiccredit_mbid` ON `music_credits` (`mbid`);
-- create "download_record_albums" table
CREATE TABLE `download_record_albums` (`download_record_id` integer NOT NULL, `album_id` integer NOT NULL, PRIMARY KEY (`download_record_id`, `album_id`), CONSTRAINT `download_record_albums_download_record_id` FOREIGN KEY (`download_record_id`) REFERENCES `download_records` (`id`) ON DELETE CASCADE, CONSTRAINT `download_record_albums_album_id` FOREIGN KEY (`album_id`) REFERENCES `albums` (`id`) ON DELETE CASCADE);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
