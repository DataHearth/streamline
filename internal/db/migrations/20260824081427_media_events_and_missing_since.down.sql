-- reverse: create index "mediaevent_create_time_tv_show_events" to table: "media_events"
DROP INDEX `mediaevent_create_time_tv_show_events`;
-- reverse: create index "mediaevent_create_time_episode_events" to table: "media_events"
DROP INDEX `mediaevent_create_time_episode_events`;
-- reverse: create index "mediaevent_create_time_movie_events" to table: "media_events"
DROP INDEX `mediaevent_create_time_movie_events`;
-- reverse: create index "mediaevent_type_create_time" to table: "media_events"
DROP INDEX `mediaevent_type_create_time`;
-- reverse: create index "mediaevent_create_time" to table: "media_events"
DROP INDEX `mediaevent_create_time`;
-- reverse: create "media_events" table
DROP TABLE `media_events`;
-- reverse: add column "missing_since" to table: "media_files"
ALTER TABLE `media_files` DROP COLUMN `missing_since`;
