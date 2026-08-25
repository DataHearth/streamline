-- add column "missing_since" to table: "media_files"
ALTER TABLE `media_files` ADD COLUMN `missing_since` datetime NULL;
-- create "media_events" table
CREATE TABLE `media_events` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `type` text NOT NULL, `payload` json NULL, `episode_events` integer NULL, `movie_events` integer NULL, `tv_show_events` integer NULL, CONSTRAINT `media_events_episodes_events` FOREIGN KEY (`episode_events`) REFERENCES `episodes` (`id`) ON DELETE CASCADE, CONSTRAINT `media_events_movies_events` FOREIGN KEY (`movie_events`) REFERENCES `movies` (`id`) ON DELETE CASCADE, CONSTRAINT `media_events_tv_shows_events` FOREIGN KEY (`tv_show_events`) REFERENCES `tv_shows` (`id`) ON DELETE CASCADE);
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
