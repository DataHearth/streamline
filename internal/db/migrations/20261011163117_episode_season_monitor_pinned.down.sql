-- Hand-written: Atlas reverses a SQLite table rebuild by dropping the new_* temp table, which no longer exists on the way down.
ALTER TABLE `episodes` DROP COLUMN `monitor_pinned`;
ALTER TABLE `seasons` DROP COLUMN `monitor_pinned`;
