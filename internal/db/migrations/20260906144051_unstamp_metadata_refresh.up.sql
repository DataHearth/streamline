-- The release_date / first_aired columns added by
-- 20260906140114_add_release_and_air_dates are provider-sourced, and nothing
-- backfills them: a row is only written on create and on metadata refresh.
-- The refresh skips anything last refreshed inside 24h, so every row already
-- in a library carries a stamp that says "already up to date" while holding a
-- NULL date — on a real install that left both fields empty everywhere, with
-- the daily job reporting `refreshed: 0` because it had no candidates.
--
-- Unstamping is the same move 20260902141241 makes for probed_at: a schema
-- change is exactly what invalidates an "already recorded" marker. The next
-- metadata-refresh tick then treats the library as stale and fills the two
-- columns in, which is one provider call per title — the same sweep the daily
-- job already performs whenever the library goes stale together, brought
-- forward by one tick rather than added to.
UPDATE `movies` SET `last_refreshed_at` = NULL;
UPDATE `tv_shows` SET `last_refreshed_at` = NULL;
