-- reverse: create index "share_user_shares" to table: "shares"
DROP INDEX `share_user_shares`;
-- reverse: create index "share_token" to table: "shares"
DROP INDEX `share_token`;
-- reverse: create "shares" table
DROP TABLE `shares`;
-- reverse: create "radio_stations" table
DROP TABLE `radio_stations`;
-- reverse: create index "playlist_user_playlists" to table: "playlists"
DROP INDEX `playlist_user_playlists`;
-- reverse: create "playlists" table
DROP TABLE `playlists`;
-- reverse: create index "play_queues_user_play_queue_key" to table: "play_queues"
DROP INDEX `play_queues_user_play_queue_key`;
-- reverse: create "play_queues" table
DROP TABLE `play_queues`;
-- reverse: create index "musicannotation_user_id_track_id" to table: "music_annotations"
DROP INDEX `musicannotation_user_id_track_id`;
-- reverse: create index "musicannotation_user_id_album_id" to table: "music_annotations"
DROP INDEX `musicannotation_user_id_album_id`;
-- reverse: create index "musicannotation_user_id_artist_id" to table: "music_annotations"
DROP INDEX `musicannotation_user_id_artist_id`;
-- reverse: create "music_annotations" table
DROP TABLE `music_annotations`;
-- reverse: create index "bookmark_user_id_track_id" to table: "bookmarks"
DROP INDEX `bookmark_user_id_track_id`;
-- reverse: create "bookmarks" table
DROP TABLE `bookmarks`;
