-- fold the removed "upcoming" series_status into "continuing"
--
-- Hand-written: sqlite stores an ent enum as plain TEXT with no CHECK
-- constraint, so dropping a value produces no schema diff and migrate:diff
-- generates nothing. The rows are still real, and ent validates the enum on
-- write only — so an "upcoming" row would survive, match neither remaining
-- status filter, and fail validation the next time anything updated it.
UPDATE `tv_shows` SET `series_status` = 'continuing' WHERE `series_status` = 'upcoming';
