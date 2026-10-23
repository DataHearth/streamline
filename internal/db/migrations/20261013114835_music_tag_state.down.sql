-- Hand-written. Atlas's generated reverse of a SQLite table rebuild drops the
-- `new_*` scratch table the rebuild already renamed away, and fails on "no
-- such table". The added columns carry no foreign key, so they drop in place.
DROP INDEX `mediafile_tags_state`;
ALTER TABLE `media_files` DROP COLUMN `tags_state`;
ALTER TABLE `media_files` DROP COLUMN `tags_hash`;
ALTER TABLE `media_files` DROP COLUMN `tagged_at`;
ALTER TABLE `media_files` DROP COLUMN `tags_mtime`;
ALTER TABLE `media_files` DROP COLUMN `tags_size`;
ALTER TABLE `media_files` DROP COLUMN `tags_checked_at`;
ALTER TABLE `media_files` DROP COLUMN `tag_failures`;
ALTER TABLE `media_files` DROP COLUMN `tags_error`;
ALTER TABLE `tracks` DROP COLUMN `release_track_mbid`;
