-- Carry the existing activity history across the movie_events -> media_events
-- rename. The schema diff above creates the new table but cannot know the old
-- one is its predecessor, so without this an upgrade silently empties the
-- activity feed. Column names line up: media_events.movie_events is the same
-- FK the old table used.
INSERT INTO `media_events` (`id`, `create_time`, `update_time`, `type`, `payload`, `movie_events`)
SELECT `id`, `create_time`, `update_time`, `type`, `payload`, `movie_events` FROM `movie_events`;
DROP TABLE `movie_events`;
