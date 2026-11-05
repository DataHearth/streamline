-- drop index "person_name" from table: "persons"
DROP INDEX `person_name`;
-- drop index "author_name" from table: "authors"
DROP INDEX `author_name`;
-- drop index "book_author_name" from table: "books"
DROP INDEX `book_author_name`;
-- create index "book_book_ebook_edition" to table: "books"
CREATE INDEX `book_book_ebook_edition` ON `books` (`book_ebook_edition`);
-- create index "book_book_audiobook_edition" to table: "books"
CREATE INDEX `book_book_audiobook_edition` ON `books` (`book_audiobook_edition`);
-- drop index "bookedition_isbn_13" from table: "book_editions"
DROP INDEX `bookedition_isbn_13`;
-- create index "bookedition_book_editions" to table: "book_editions"
CREATE INDEX `bookedition_book_editions` ON `book_editions` (`book_editions`);
-- drop index "musiccredit_mbid" from table: "music_credits"
DROP INDEX `musiccredit_mbid`;
-- create index "bookmark_track_id" to table: "bookmarks"
CREATE INDEX `bookmark_track_id` ON `bookmarks` (`track_id`);
-- create index "musicannotation_artist_id" to table: "music_annotations"
CREATE INDEX `musicannotation_artist_id` ON `music_annotations` (`artist_id`);
-- create index "musicannotation_album_id" to table: "music_annotations"
CREATE INDEX `musicannotation_album_id` ON `music_annotations` (`album_id`);
-- create index "musicannotation_track_id" to table: "music_annotations"
CREATE INDEX `musicannotation_track_id` ON `music_annotations` (`track_id`);
-- drop index "downloadrecord_rejected" from table: "download_records"
DROP INDEX `downloadrecord_rejected`;
-- create index "downloadrecord_album_download_records" to table: "download_records"
CREATE INDEX `downloadrecord_album_download_records` ON `download_records` (`album_download_records`);
-- create index "downloadrecord_track_download_records" to table: "download_records"
CREATE INDEX `downloadrecord_track_download_records` ON `download_records` (`track_download_records`);
-- create index "downloadrecord_book_download_records" to table: "download_records"
CREATE INDEX `downloadrecord_book_download_records` ON `download_records` (`book_download_records`);
