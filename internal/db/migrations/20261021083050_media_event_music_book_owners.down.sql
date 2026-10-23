-- Hand-written: atlas reverses a SQLite table rebuild by dropping the
-- temporary table, which no longer exists once the up has run. This rebuilds
-- media_events back to its movie/episode/series-only shape instead. Rows owned
-- by an artist, album, book or book series have no owner in that shape and
-- are dropped, as events.Record would refuse them.
-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_media_events" table
CREATE TABLE `new_media_events` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `type` text NOT NULL, `payload` json NULL, `episode_events` integer NULL, `movie_events` integer NULL, `tv_show_events` integer NULL, CONSTRAINT `media_events_episodes_events` FOREIGN KEY (`episode_events`) REFERENCES `episodes` (`id`) ON DELETE CASCADE, CONSTRAINT `media_events_movies_events` FOREIGN KEY (`movie_events`) REFERENCES `movies` (`id`) ON DELETE CASCADE, CONSTRAINT `media_events_tv_shows_events` FOREIGN KEY (`tv_show_events`) REFERENCES `tv_shows` (`id`) ON DELETE CASCADE);
-- copy the movie, episode and series rows across
INSERT INTO `new_media_events` (`id`, `create_time`, `update_time`, `type`, `payload`, `episode_events`, `movie_events`, `tv_show_events`) SELECT `id`, `create_time`, `update_time`, `type`, `payload`, `episode_events`, `movie_events`, `tv_show_events` FROM `media_events` WHERE `episode_events` IS NOT NULL OR `movie_events` IS NOT NULL OR `tv_show_events` IS NOT NULL;
-- drop "media_events" table after copying rows
DROP TABLE `media_events`;
-- rename temporary table "new_media_events" to "media_events"
ALTER TABLE `new_media_events` RENAME TO `media_events`;
-- create index "mediaevent_create_time" to table: "media_events"
CREATE INDEX `mediaevent_create_time` ON `media_events` (`create_time`);
-- create index "mediaevent_type_create_time" to table: "media_events"
CREATE INDEX `mediaevent_type_create_time` ON `media_events` (`type`, `create_time`);
-- create index "mediaevent_create_time_movie_events" to table: "media_events"
CREATE INDEX `mediaevent_create_time_movie_events` ON `media_events` (`create_time`, `movie_events`);
-- create index "mediaevent_create_time_episode_events" to table: "media_events"
CREATE INDEX `mediaevent_create_time_episode_events` ON `media_events` (`create_time`, `episode_events`);
-- create index "mediaevent_create_time_tv_show_events" to table: "media_events"
CREATE INDEX `mediaevent_create_time_tv_show_events` ON `media_events` (`create_time`, `tv_show_events`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
