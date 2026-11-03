-- disable the enforcement of foreign-keys constraints
PRAGMA foreign_keys = off;
-- create "new_media_events" table
CREATE TABLE `new_media_events` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `type` text NOT NULL, `payload` json NULL, `album_events` integer NULL, `artist_events` integer NULL, `book_events` integer NULL, `book_series_events` integer NULL, `episode_events` integer NULL, `movie_events` integer NULL, `tv_show_events` integer NULL, CONSTRAINT `media_events_albums_events` FOREIGN KEY (`album_events`) REFERENCES `albums` (`id`) ON DELETE CASCADE, CONSTRAINT `media_events_artists_events` FOREIGN KEY (`artist_events`) REFERENCES `artists` (`id`) ON DELETE CASCADE, CONSTRAINT `media_events_books_events` FOREIGN KEY (`book_events`) REFERENCES `books` (`id`) ON DELETE CASCADE, CONSTRAINT `media_events_book_series_events` FOREIGN KEY (`book_series_events`) REFERENCES `book_series` (`id`) ON DELETE CASCADE, CONSTRAINT `media_events_episodes_events` FOREIGN KEY (`episode_events`) REFERENCES `episodes` (`id`) ON DELETE CASCADE, CONSTRAINT `media_events_movies_events` FOREIGN KEY (`movie_events`) REFERENCES `movies` (`id`) ON DELETE CASCADE, CONSTRAINT `media_events_tv_shows_events` FOREIGN KEY (`tv_show_events`) REFERENCES `tv_shows` (`id`) ON DELETE CASCADE);
-- copy rows from old table "media_events" to new temporary table "new_media_events"
INSERT INTO `new_media_events` (`id`, `create_time`, `update_time`, `type`, `payload`, `episode_events`, `movie_events`, `tv_show_events`) SELECT `id`, `create_time`, `update_time`, `type`, `payload`, `episode_events`, `movie_events`, `tv_show_events` FROM `media_events`;
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
-- create index "mediaevent_create_time_artist_events" to table: "media_events"
CREATE INDEX `mediaevent_create_time_artist_events` ON `media_events` (`create_time`, `artist_events`);
-- create index "mediaevent_create_time_album_events" to table: "media_events"
CREATE INDEX `mediaevent_create_time_album_events` ON `media_events` (`create_time`, `album_events`);
-- create index "mediaevent_create_time_book_events" to table: "media_events"
CREATE INDEX `mediaevent_create_time_book_events` ON `media_events` (`create_time`, `book_events`);
-- create index "mediaevent_create_time_book_series_events" to table: "media_events"
CREATE INDEX `mediaevent_create_time_book_series_events` ON `media_events` (`create_time`, `book_series_events`);
-- enable back the enforcement of foreign-keys constraints
PRAGMA foreign_keys = on;
