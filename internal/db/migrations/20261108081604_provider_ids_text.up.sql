-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_import_scan_files" table
CREATE TABLE `new_import_scan_files` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `source_path` text NOT NULL, `size` integer NOT NULL, `parsed_title` text NULL, `parsed_year` integer NULL, `parsed_quality` text NULL, `parsed_release_group` text NULL, `classification` text NOT NULL DEFAULT ('unmatched'), `candidates` json NULL, `tmdb_id` text NULL, `existing_movie_id` integer NULL, `decision` text NOT NULL DEFAULT ('pending'), `decision_tmdb_id` text NULL, `outcome` text NOT NULL DEFAULT ('pending'), `outcome_message` text NULL, `created_movie_id` integer NULL, `import_scan_files` integer NOT NULL, CONSTRAINT `import_scan_files_import_scans_files` FOREIGN KEY (`import_scan_files`) REFERENCES `import_scans` (`id`) ON DELETE CASCADE);
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
CREATE TABLE `new_import_scan_shows` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `folder_path` text NOT NULL, `parsed_title` text NULL, `parsed_year` integer NULL, `classification` text NOT NULL DEFAULT ('unmatched'), `tvdb_id` text NULL, `candidates` json NULL, `existing_tvshow_id` integer NULL, `file_count` integer NOT NULL DEFAULT (0), `decision` text NOT NULL DEFAULT ('pending'), `decision_tvdb_id` text NULL, `outcome` text NOT NULL DEFAULT ('pending'), `outcome_message` text NULL, `created_tvshow_id` integer NULL, `import_scan_shows` integer NOT NULL, CONSTRAINT `import_scan_shows_import_scans_shows` FOREIGN KEY (`import_scan_shows`) REFERENCES `import_scans` (`id`) ON DELETE CASCADE);
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
-- create "new_persons" table
CREATE TABLE `new_persons` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `tmdb_id` text NULL, `tvdb_id` text NULL, `name` text NOT NULL, `profile_url` text NULL, `biography` text NULL, `known_for` text NULL, `birthday` text NULL, `deathday` text NULL, `place_of_birth` text NULL, `imdb_id` text NULL, `instagram_id` text NULL, `twitter_id` text NULL, `details_fetched_at` datetime NULL);
-- copy rows from old table "persons" to new temporary table "new_persons"
INSERT INTO `new_persons` (`id`, `create_time`, `update_time`, `tmdb_id`, `tvdb_id`, `name`, `profile_url`, `biography`, `known_for`, `birthday`, `deathday`, `place_of_birth`, `imdb_id`, `instagram_id`, `twitter_id`, `details_fetched_at`) SELECT `id`, `create_time`, `update_time`, `tmdb_id`, `tvdb_id`, `name`, `profile_url`, `biography`, `known_for`, `birthday`, `deathday`, `place_of_birth`, `imdb_id`, `instagram_id`, `twitter_id`, `details_fetched_at` FROM `persons`;
-- drop "persons" table after copying rows
DROP TABLE `persons`;
-- rename temporary table "new_persons" to "persons"
ALTER TABLE `new_persons` RENAME TO `persons`;
-- create index "person_tmdb_id" to table: "persons"
CREATE INDEX `person_tmdb_id` ON `persons` (`tmdb_id`);
-- create index "person_tvdb_id" to table: "persons"
CREATE INDEX `person_tvdb_id` ON `persons` (`tvdb_id`);
-- create "new_tv_shows" table
CREATE TABLE `new_tv_shows` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `title` text NOT NULL, `original_title` text NULL, `year` integer NOT NULL, `first_aired` datetime NULL, `overview` text NULL, `series_status` text NOT NULL DEFAULT ('continuing'), `type` text NOT NULL DEFAULT ('standard'), `monitored` bool NOT NULL DEFAULT (true), `tvdb_id` text NOT NULL, `poster_path` text NULL, `network` text NULL, `creator` text NULL, `runtime` integer NULL DEFAULT (0), `rating` real NULL DEFAULT (0), `genres` json NULL, `aliases` json NULL, `last_refreshed_at` datetime NULL, `quality_profile` text NULL);
-- copy rows from old table "tv_shows" to new temporary table "new_tv_shows"
INSERT INTO `new_tv_shows` (`id`, `create_time`, `update_time`, `title`, `original_title`, `year`, `first_aired`, `overview`, `series_status`, `type`, `monitored`, `tvdb_id`, `poster_path`, `network`, `creator`, `runtime`, `rating`, `genres`, `aliases`, `last_refreshed_at`, `quality_profile`) SELECT `id`, `create_time`, `update_time`, `title`, `original_title`, `year`, `first_aired`, `overview`, `series_status`, `type`, `monitored`, `tvdb_id`, `poster_path`, `network`, `creator`, `runtime`, `rating`, `genres`, `aliases`, `last_refreshed_at`, `quality_profile` FROM `tv_shows`;
-- drop "tv_shows" table after copying rows
DROP TABLE `tv_shows`;
-- rename temporary table "new_tv_shows" to "tv_shows"
ALTER TABLE `new_tv_shows` RENAME TO `tv_shows`;
-- create index "tv_shows_tvdb_id_key" to table: "tv_shows"
CREATE UNIQUE INDEX `tv_shows_tvdb_id_key` ON `tv_shows` (`tvdb_id`);
-- create index "tvshow_create_time" to table: "tv_shows"
CREATE INDEX `tvshow_create_time` ON `tv_shows` (`create_time`);
-- create index "tvshow_series_status" to table: "tv_shows"
CREATE INDEX `tvshow_series_status` ON `tv_shows` (`series_status`);
-- create "new_movies" table
CREATE TABLE `new_movies` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `title` text NOT NULL, `original_title` text NOT NULL, `year` integer NOT NULL, `overview` text NULL, `runtime` integer NULL DEFAULT (0), `status` text NOT NULL DEFAULT ('wanted'), `monitored` bool NOT NULL DEFAULT (true), `tmdb_id` text NOT NULL, `last_search_at` datetime NULL, `release_date` datetime NULL, `digital_release_date` datetime NULL, `grab_failures` integer NOT NULL DEFAULT (0), `failure_reason` text NULL, `quality_profile` text NULL, `rating` real NULL DEFAULT (0), `genres` json NULL, `aliases` json NULL, `last_refreshed_at` datetime NULL);
-- copy rows from old table "movies" to new temporary table "new_movies"
INSERT INTO `new_movies` (`id`, `create_time`, `update_time`, `title`, `original_title`, `year`, `overview`, `runtime`, `status`, `monitored`, `tmdb_id`, `last_search_at`, `release_date`, `digital_release_date`, `grab_failures`, `failure_reason`, `quality_profile`, `rating`, `genres`, `aliases`, `last_refreshed_at`) SELECT `id`, `create_time`, `update_time`, `title`, `original_title`, `year`, `overview`, `runtime`, `status`, `monitored`, `tmdb_id`, `last_search_at`, `release_date`, `digital_release_date`, `grab_failures`, `failure_reason`, `quality_profile`, `rating`, `genres`, `aliases`, `last_refreshed_at` FROM `movies`;
-- drop "movies" table after copying rows
DROP TABLE `movies`;
-- rename temporary table "new_movies" to "movies"
ALTER TABLE `new_movies` RENAME TO `movies`;
-- create index "movies_tmdb_id_key" to table: "movies"
CREATE UNIQUE INDEX `movies_tmdb_id_key` ON `movies` (`tmdb_id`);
-- create index "movie_digital_release_date" to table: "movies"
CREATE INDEX `movie_digital_release_date` ON `movies` (`digital_release_date`);
-- create index "movie_status" to table: "movies"
CREATE INDEX `movie_status` ON `movies` (`status`);
-- create index "movie_create_time" to table: "movies"
CREATE INDEX `movie_create_time` ON `movies` (`create_time`);
-- create "new_authors" table
CREATE TABLE `new_authors` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `hardcover_id` text NOT NULL, `name` text NOT NULL, `sort_name` text NULL, `image_source` text NULL);
-- copy rows from old table "authors" to new temporary table "new_authors"
INSERT INTO `new_authors` (`id`, `create_time`, `update_time`, `hardcover_id`, `name`, `sort_name`, `image_source`) SELECT `id`, `create_time`, `update_time`, `hardcover_id`, `name`, `sort_name`, `image_source` FROM `authors`;
-- drop "authors" table after copying rows
DROP TABLE `authors`;
-- rename temporary table "new_authors" to "authors"
ALTER TABLE `new_authors` RENAME TO `authors`;
-- create index "authors_hardcover_id_key" to table: "authors"
CREATE UNIQUE INDEX `authors_hardcover_id_key` ON `authors` (`hardcover_id`);
-- create "new_books" table
CREATE TABLE `new_books` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `hardcover_id` text NOT NULL, `title` text NOT NULL, `original_title` text NULL, `sort_title` text NULL, `author_name` text NULL, `kind` text NOT NULL DEFAULT ('novel'), `genre` text NULL, `rating_tenths` integer NULL, `release_year` integer NULL, `release_date` datetime NULL, `overview` text NULL, `preferred_language` text NOT NULL DEFAULT ('en'), `quality_profile` text NULL, `series_position` real NULL, `ebook_monitored` bool NOT NULL DEFAULT (false), `ebook_status` text NOT NULL DEFAULT ('skipped'), `ebook_grab_failures` integer NOT NULL DEFAULT (0), `ebook_last_search_at` datetime NULL, `ebook_replacing_language` text NULL, `audiobook_monitored` bool NOT NULL DEFAULT (false), `audiobook_status` text NOT NULL DEFAULT ('skipped'), `audiobook_grab_failures` integer NOT NULL DEFAULT (0), `audiobook_last_search_at` datetime NULL, `audiobook_replacing_language` text NULL, `last_refreshed_at` datetime NULL, `book_ebook_edition` integer NULL, `book_audiobook_edition` integer NULL, `book_series_volumes` integer NULL, CONSTRAINT `books_book_editions_ebook_edition` FOREIGN KEY (`book_ebook_edition`) REFERENCES `book_editions` (`id`) ON DELETE SET NULL, CONSTRAINT `books_book_editions_audiobook_edition` FOREIGN KEY (`book_audiobook_edition`) REFERENCES `book_editions` (`id`) ON DELETE SET NULL, CONSTRAINT `books_book_series_volumes` FOREIGN KEY (`book_series_volumes`) REFERENCES `book_series` (`id`) ON DELETE CASCADE);
-- copy rows from old table "books" to new temporary table "new_books"
INSERT INTO `new_books` (`id`, `create_time`, `update_time`, `hardcover_id`, `title`, `original_title`, `sort_title`, `author_name`, `kind`, `genre`, `rating_tenths`, `release_year`, `release_date`, `overview`, `preferred_language`, `quality_profile`, `series_position`, `ebook_monitored`, `ebook_status`, `ebook_grab_failures`, `ebook_last_search_at`, `ebook_replacing_language`, `audiobook_monitored`, `audiobook_status`, `audiobook_grab_failures`, `audiobook_last_search_at`, `audiobook_replacing_language`, `last_refreshed_at`, `book_ebook_edition`, `book_audiobook_edition`, `book_series_volumes`) SELECT `id`, `create_time`, `update_time`, `hardcover_id`, `title`, `original_title`, `sort_title`, `author_name`, `kind`, `genre`, `rating_tenths`, `release_year`, `release_date`, `overview`, `preferred_language`, `quality_profile`, `series_position`, `ebook_monitored`, `ebook_status`, `ebook_grab_failures`, `ebook_last_search_at`, `ebook_replacing_language`, `audiobook_monitored`, `audiobook_status`, `audiobook_grab_failures`, `audiobook_last_search_at`, `audiobook_replacing_language`, `last_refreshed_at`, `book_ebook_edition`, `book_audiobook_edition`, `book_series_volumes` FROM `books`;
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
-- create index "book_book_ebook_edition" to table: "books"
CREATE INDEX `book_book_ebook_edition` ON `books` (`book_ebook_edition`);
-- create index "book_book_audiobook_edition" to table: "books"
CREATE INDEX `book_book_audiobook_edition` ON `books` (`book_audiobook_edition`);
-- create index "book_kind" to table: "books"
CREATE INDEX `book_kind` ON `books` (`kind`);
-- create "new_requests" table
CREATE TABLE `new_requests` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `media_type` text NOT NULL, `media_id` text NULL, `media_mbid` text NULL, `artist_mbid` text NULL, `artist_name` text NULL, `requested_as` text NULL, `title` text NOT NULL, `status` text NOT NULL DEFAULT ('pending'), `reason` text NULL, `quality_profile` text NULL, `request_approved_by` integer NULL, `user_requests` integer NOT NULL, CONSTRAINT `requests_users_approved_by` FOREIGN KEY (`request_approved_by`) REFERENCES `users` (`id`) ON DELETE SET NULL, CONSTRAINT `requests_users_requests` FOREIGN KEY (`user_requests`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- copy rows from old table "requests" to new temporary table "new_requests"
INSERT INTO `new_requests` (`id`, `create_time`, `update_time`, `media_type`, `media_id`, `media_mbid`, `artist_mbid`, `artist_name`, `requested_as`, `title`, `status`, `reason`, `quality_profile`, `request_approved_by`, `user_requests`) SELECT `id`, `create_time`, `update_time`, `media_type`, `media_id`, `media_mbid`, `artist_mbid`, `artist_name`, `requested_as`, `title`, `status`, `reason`, `quality_profile`, `request_approved_by`, `user_requests` FROM `requests`;
-- drop "requests" table after copying rows
DROP TABLE `requests`;
-- rename temporary table "new_requests" to "requests"
ALTER TABLE `new_requests` RENAME TO `requests`;
-- create index "request_media_type_media_id" to table: "requests"
CREATE UNIQUE INDEX `request_media_type_media_id` ON `requests` (`media_type`, `media_id`) WHERE status IN ('pending', 'approved', 'available') AND media_id <> '';
-- create index "request_media_type_media_mbid" to table: "requests"
CREATE UNIQUE INDEX `request_media_type_media_mbid` ON `requests` (`media_type`, `media_mbid`) WHERE status IN ('pending', 'approved', 'available') AND media_mbid <> '';
-- create index "request_user_requests" to table: "requests"
CREATE INDEX `request_user_requests` ON `requests` (`user_requests`);
-- create index "request_request_approved_by" to table: "requests"
CREATE INDEX `request_request_approved_by` ON `requests` (`request_approved_by`);
-- create "new_book_editions" table
CREATE TABLE `new_book_editions` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `hardcover_edition_id` text NOT NULL, `language` text NOT NULL, `title` text NOT NULL, `publisher` text NOT NULL DEFAULT (''), `year` integer NOT NULL DEFAULT (0), `format` text NOT NULL, `original` bool NOT NULL DEFAULT (false), `pages` integer NULL, `duration_seconds` integer NULL, `narrator` text NULL, `translator` text NULL, `isbn_13` text NULL, `asin` text NULL, `popularity` integer NOT NULL DEFAULT (0), `book_editions` integer NOT NULL, CONSTRAINT `book_editions_books_editions` FOREIGN KEY (`book_editions`) REFERENCES `books` (`id`) ON DELETE CASCADE);
-- copy rows from old table "book_editions" to new temporary table "new_book_editions"
INSERT INTO `new_book_editions` (`id`, `create_time`, `update_time`, `hardcover_edition_id`, `language`, `title`, `publisher`, `year`, `format`, `original`, `pages`, `duration_seconds`, `narrator`, `translator`, `isbn_13`, `asin`, `popularity`, `book_editions`) SELECT `id`, `create_time`, `update_time`, `hardcover_edition_id`, `language`, `title`, `publisher`, `year`, `format`, `original`, `pages`, `duration_seconds`, `narrator`, `translator`, `isbn_13`, `asin`, `popularity`, `book_editions` FROM `book_editions`;
-- drop "book_editions" table after copying rows
DROP TABLE `book_editions`;
-- rename temporary table "new_book_editions" to "book_editions"
ALTER TABLE `new_book_editions` RENAME TO `book_editions`;
-- create index "bookedition_book_editions" to table: "book_editions"
CREATE INDEX `bookedition_book_editions` ON `book_editions` (`book_editions`);
-- create index "bookedition_hardcover_edition_id_book_editions" to table: "book_editions"
CREATE UNIQUE INDEX `bookedition_hardcover_edition_id_book_editions` ON `book_editions` (`hardcover_edition_id`, `book_editions`);
-- create index "bookedition_language_format_publisher_book_editions" to table: "book_editions"
CREATE INDEX `bookedition_language_format_publisher_book_editions` ON `book_editions` (`language`, `format`, `publisher`, `book_editions`);
-- create "new_book_series" table
CREATE TABLE `new_book_series` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `hardcover_id` text NOT NULL, `title` text NOT NULL, `original_title` text NULL, `sort_title` text NULL, `overview` text NULL, `author_name` text NULL, `kind` text NOT NULL DEFAULT ('novel'), `rating_tenths` integer NULL, `ongoing` bool NOT NULL DEFAULT (false), `since` integer NULL, `monitor` text NOT NULL DEFAULT ('all'), `quality_profile` text NULL, `edition_language` text NULL, `edition_publisher` text NULL, `last_refreshed_at` datetime NULL, `hardcover_slug` text NULL);
-- copy rows from old table "book_series" to new temporary table "new_book_series"
INSERT INTO `new_book_series` (`id`, `create_time`, `update_time`, `hardcover_id`, `title`, `original_title`, `sort_title`, `overview`, `author_name`, `kind`, `rating_tenths`, `ongoing`, `since`, `monitor`, `quality_profile`, `edition_language`, `edition_publisher`, `last_refreshed_at`, `hardcover_slug`) SELECT `id`, `create_time`, `update_time`, `hardcover_id`, `title`, `original_title`, `sort_title`, `overview`, `author_name`, `kind`, `rating_tenths`, `ongoing`, `since`, `monitor`, `quality_profile`, `edition_language`, `edition_publisher`, `last_refreshed_at`, `hardcover_slug` FROM `book_series`;
-- drop "book_series" table after copying rows
DROP TABLE `book_series`;
-- rename temporary table "new_book_series" to "book_series"
ALTER TABLE `new_book_series` RENAME TO `book_series`;
-- create index "book_series_hardcover_id_key" to table: "book_series"
CREATE UNIQUE INDEX `book_series_hardcover_id_key` ON `book_series` (`hardcover_id`);
-- create "new_import_scan_books" table
CREATE TABLE `new_import_scan_books` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `file_paths` json NOT NULL, `slot` text NOT NULL, `parsed_title` text NULL, `parsed_author` text NULL, `parsed_isbn` text NULL, `parsed_year` integer NULL, `size` integer NOT NULL DEFAULT (0), `classification` text NOT NULL DEFAULT ('unmatched'), `book_hardcover_id` text NULL, `candidates` json NULL, `existing_book_id` integer NULL, `decision` text NOT NULL DEFAULT ('pending'), `decision_book_hardcover_id` text NULL, `outcome` text NOT NULL DEFAULT ('pending'), `outcome_message` text NULL, `created_book_id` integer NULL, `import_scan_books` integer NOT NULL, CONSTRAINT `import_scan_books_import_scans_books` FOREIGN KEY (`import_scan_books`) REFERENCES `import_scans` (`id`) ON DELETE CASCADE);
-- copy rows from old table "import_scan_books" to new temporary table "new_import_scan_books"
INSERT INTO `new_import_scan_books` (`id`, `create_time`, `update_time`, `file_paths`, `slot`, `parsed_title`, `parsed_author`, `parsed_isbn`, `parsed_year`, `size`, `classification`, `book_hardcover_id`, `candidates`, `existing_book_id`, `decision`, `decision_book_hardcover_id`, `outcome`, `outcome_message`, `created_book_id`, `import_scan_books`) SELECT `id`, `create_time`, `update_time`, `file_paths`, `slot`, `parsed_title`, `parsed_author`, `parsed_isbn`, `parsed_year`, `size`, `classification`, `book_hardcover_id`, `candidates`, `existing_book_id`, `decision`, `decision_book_hardcover_id`, `outcome`, `outcome_message`, `created_book_id`, `import_scan_books` FROM `import_scan_books`;
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
