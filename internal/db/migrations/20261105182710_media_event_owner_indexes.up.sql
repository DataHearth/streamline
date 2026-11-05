-- drop index "mediaevent_create_time_movie_events" from table: "media_events"
DROP INDEX `mediaevent_create_time_movie_events`;
-- drop index "mediaevent_create_time_episode_events" from table: "media_events"
DROP INDEX `mediaevent_create_time_episode_events`;
-- drop index "mediaevent_create_time_tv_show_events" from table: "media_events"
DROP INDEX `mediaevent_create_time_tv_show_events`;
-- drop index "mediaevent_create_time_artist_events" from table: "media_events"
DROP INDEX `mediaevent_create_time_artist_events`;
-- drop index "mediaevent_create_time_album_events" from table: "media_events"
DROP INDEX `mediaevent_create_time_album_events`;
-- drop index "mediaevent_create_time_book_events" from table: "media_events"
DROP INDEX `mediaevent_create_time_book_events`;
-- drop index "mediaevent_create_time_book_series_events" from table: "media_events"
DROP INDEX `mediaevent_create_time_book_series_events`;
-- create index "mediaevent_movie_events" to table: "media_events"
CREATE INDEX `mediaevent_movie_events` ON `media_events` (`movie_events`);
-- create index "mediaevent_episode_events" to table: "media_events"
CREATE INDEX `mediaevent_episode_events` ON `media_events` (`episode_events`);
-- create index "mediaevent_tv_show_events" to table: "media_events"
CREATE INDEX `mediaevent_tv_show_events` ON `media_events` (`tv_show_events`);
-- create index "mediaevent_artist_events" to table: "media_events"
CREATE INDEX `mediaevent_artist_events` ON `media_events` (`artist_events`);
-- create index "mediaevent_album_events" to table: "media_events"
CREATE INDEX `mediaevent_album_events` ON `media_events` (`album_events`);
-- create index "mediaevent_book_events" to table: "media_events"
CREATE INDEX `mediaevent_book_events` ON `media_events` (`book_events`);
-- create index "mediaevent_book_series_events" to table: "media_events"
CREATE INDEX `mediaevent_book_series_events` ON `media_events` (`book_series_events`);
