-- reverse: create index "mediaevent_book_series_events" to table: "media_events"
DROP INDEX `mediaevent_book_series_events`;
-- reverse: create index "mediaevent_book_events" to table: "media_events"
DROP INDEX `mediaevent_book_events`;
-- reverse: create index "mediaevent_album_events" to table: "media_events"
DROP INDEX `mediaevent_album_events`;
-- reverse: create index "mediaevent_artist_events" to table: "media_events"
DROP INDEX `mediaevent_artist_events`;
-- reverse: create index "mediaevent_tv_show_events" to table: "media_events"
DROP INDEX `mediaevent_tv_show_events`;
-- reverse: create index "mediaevent_episode_events" to table: "media_events"
DROP INDEX `mediaevent_episode_events`;
-- reverse: create index "mediaevent_movie_events" to table: "media_events"
DROP INDEX `mediaevent_movie_events`;
-- reverse: drop index "mediaevent_create_time_book_series_events" from table: "media_events"
CREATE INDEX `mediaevent_create_time_book_series_events` ON `media_events` (`create_time`, `book_series_events`);
-- reverse: drop index "mediaevent_create_time_book_events" from table: "media_events"
CREATE INDEX `mediaevent_create_time_book_events` ON `media_events` (`create_time`, `book_events`);
-- reverse: drop index "mediaevent_create_time_album_events" from table: "media_events"
CREATE INDEX `mediaevent_create_time_album_events` ON `media_events` (`create_time`, `album_events`);
-- reverse: drop index "mediaevent_create_time_artist_events" from table: "media_events"
CREATE INDEX `mediaevent_create_time_artist_events` ON `media_events` (`create_time`, `artist_events`);
-- reverse: drop index "mediaevent_create_time_tv_show_events" from table: "media_events"
CREATE INDEX `mediaevent_create_time_tv_show_events` ON `media_events` (`create_time`, `tv_show_events`);
-- reverse: drop index "mediaevent_create_time_episode_events" from table: "media_events"
CREATE INDEX `mediaevent_create_time_episode_events` ON `media_events` (`create_time`, `episode_events`);
-- reverse: drop index "mediaevent_create_time_movie_events" from table: "media_events"
CREATE INDEX `mediaevent_create_time_movie_events` ON `media_events` (`create_time`, `movie_events`);
