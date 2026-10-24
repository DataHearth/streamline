-- Hand-written: Atlas reverses a SQLite table rebuild by dropping the new_* temp table, which no longer exists on the way down.
DROP INDEX `downloadrecord_rejected`;
ALTER TABLE `download_records` DROP COLUMN `rejected`;
