-- Hand-written. Atlas's generated reverse of a SQLite table rebuild drops the
-- `new_*` scratch table the rebuild already renamed away, and fails on "no
-- such table". The added column carries no foreign key, so it drops in place.
ALTER TABLE `albums` DROP COLUMN `tags_managed`;
