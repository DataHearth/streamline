-- create index "mediafile_book_kind" to table: "media_files"
CREATE INDEX `mediafile_book_kind` ON `media_files` (`book_kind`) WHERE book_kind IS NOT NULL;
