-- Re-adds the columns the up dropped, empty. Atlas's generated reverse of a
-- SQLite table rebuild drops the `new_movies`/`new_tv_shows` scratch tables
-- the rebuild already renamed away, so it fails on "no such table".
--
-- The cast itself is not copied back: it lives in persons/credits now, and the
-- migration below this one (backfill_person_credits) empties those on its own
-- way down. Whichever model the schema lands on, a metadata refresh refills it.
ALTER TABLE `tv_shows` ADD COLUMN `cast` json NULL;
ALTER TABLE `movies` ADD COLUMN `cast` json NULL;
