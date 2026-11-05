-- reverse: create index "downloadrecord_book_download_records" to table: "download_records"
DROP INDEX `downloadrecord_book_download_records`;
-- reverse: create index "downloadrecord_track_download_records" to table: "download_records"
DROP INDEX `downloadrecord_track_download_records`;
-- reverse: create index "downloadrecord_album_download_records" to table: "download_records"
DROP INDEX `downloadrecord_album_download_records`;
-- reverse: drop index "downloadrecord_rejected" from table: "download_records"
CREATE INDEX `downloadrecord_rejected` ON `download_records` (`rejected`);
-- reverse: create index "musicannotation_track_id" to table: "music_annotations"
DROP INDEX `musicannotation_track_id`;
-- reverse: create index "musicannotation_album_id" to table: "music_annotations"
DROP INDEX `musicannotation_album_id`;
-- reverse: create index "musicannotation_artist_id" to table: "music_annotations"
DROP INDEX `musicannotation_artist_id`;
-- reverse: create index "bookmark_track_id" to table: "bookmarks"
DROP INDEX `bookmark_track_id`;
-- reverse: drop index "musiccredit_mbid" from table: "music_credits"
CREATE INDEX `musiccredit_mbid` ON `music_credits` (`mbid`);
-- reverse: create index "bookedition_book_editions" to table: "book_editions"
DROP INDEX `bookedition_book_editions`;
-- reverse: drop index "bookedition_isbn_13" from table: "book_editions"
CREATE INDEX `bookedition_isbn_13` ON `book_editions` (`isbn_13`);
-- reverse: create index "book_book_audiobook_edition" to table: "books"
DROP INDEX `book_book_audiobook_edition`;
-- reverse: create index "book_book_ebook_edition" to table: "books"
DROP INDEX `book_book_ebook_edition`;
-- reverse: drop index "book_author_name" from table: "books"
CREATE INDEX `book_author_name` ON `books` (`author_name`);
-- reverse: drop index "author_name" from table: "authors"
CREATE INDEX `author_name` ON `authors` (`name`);
-- reverse: drop index "person_name" from table: "persons"
CREATE INDEX `person_name` ON `persons` (`name`);
