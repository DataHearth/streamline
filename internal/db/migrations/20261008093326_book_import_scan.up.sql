-- create "import_scan_books" table
CREATE TABLE `import_scan_books` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `file_paths` json NOT NULL, `slot` text NOT NULL, `parsed_title` text NULL, `parsed_author` text NULL, `parsed_isbn` text NULL, `classification` text NOT NULL DEFAULT ('unmatched'), `book_hardcover_id` integer NULL, `author_hardcover_id` integer NULL, `candidates` json NULL, `existing_book_id` integer NULL, `decision` text NOT NULL DEFAULT ('pending'), `decision_book_hardcover_id` integer NULL, `outcome` text NOT NULL DEFAULT ('pending'), `outcome_message` text NULL, `created_book_id` integer NULL, `import_scan_books` integer NOT NULL, CONSTRAINT `import_scan_books_import_scans_books` FOREIGN KEY (`import_scan_books`) REFERENCES `import_scans` (`id`) ON DELETE CASCADE);
-- create index "importscanbook_classification" to table: "import_scan_books"
CREATE INDEX `importscanbook_classification` ON `import_scan_books` (`classification`);
-- create index "importscanbook_decision" to table: "import_scan_books"
CREATE INDEX `importscanbook_decision` ON `import_scan_books` (`decision`);
-- create index "importscanbook_import_scan_books" to table: "import_scan_books"
CREATE INDEX `importscanbook_import_scan_books` ON `import_scan_books` (`import_scan_books`);
