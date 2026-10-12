-- create "bookmarks" table
CREATE TABLE `bookmarks` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `position` integer NOT NULL, `comment` text NULL, `track_id` integer NOT NULL, `user_id` integer NOT NULL, CONSTRAINT `bookmarks_tracks_bookmarks` FOREIGN KEY (`track_id`) REFERENCES `tracks` (`id`) ON DELETE CASCADE, CONSTRAINT `bookmarks_users_bookmarks` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create index "bookmark_user_id_track_id" to table: "bookmarks"
CREATE UNIQUE INDEX `bookmark_user_id_track_id` ON `bookmarks` (`user_id`, `track_id`);
-- create "music_annotations" table
CREATE TABLE `music_annotations` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `starred_at` datetime NULL, `rating` integer NOT NULL DEFAULT (0), `play_count` integer NOT NULL DEFAULT (0), `played_at` datetime NULL, `album_id` integer NULL, `artist_id` integer NULL, `track_id` integer NULL, `user_id` integer NOT NULL, CONSTRAINT `music_annotations_albums_annotations` FOREIGN KEY (`album_id`) REFERENCES `albums` (`id`) ON DELETE CASCADE, CONSTRAINT `music_annotations_artists_annotations` FOREIGN KEY (`artist_id`) REFERENCES `artists` (`id`) ON DELETE CASCADE, CONSTRAINT `music_annotations_tracks_annotations` FOREIGN KEY (`track_id`) REFERENCES `tracks` (`id`) ON DELETE CASCADE, CONSTRAINT `music_annotations_users_music_annotations` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create index "musicannotation_user_id_artist_id" to table: "music_annotations"
CREATE UNIQUE INDEX `musicannotation_user_id_artist_id` ON `music_annotations` (`user_id`, `artist_id`);
-- create index "musicannotation_user_id_album_id" to table: "music_annotations"
CREATE UNIQUE INDEX `musicannotation_user_id_album_id` ON `music_annotations` (`user_id`, `album_id`);
-- create index "musicannotation_user_id_track_id" to table: "music_annotations"
CREATE UNIQUE INDEX `musicannotation_user_id_track_id` ON `music_annotations` (`user_id`, `track_id`);
-- create "play_queues" table
CREATE TABLE `play_queues` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `track_ids` json NULL, `current` integer NOT NULL DEFAULT (0), `position` integer NOT NULL DEFAULT (0), `changed_by` text NULL, `user_play_queue` integer NOT NULL, CONSTRAINT `play_queues_users_play_queue` FOREIGN KEY (`user_play_queue`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create index "play_queues_user_play_queue_key" to table: "play_queues"
CREATE UNIQUE INDEX `play_queues_user_play_queue_key` ON `play_queues` (`user_play_queue`);
-- create "playlists" table
CREATE TABLE `playlists` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `name` text NOT NULL, `comment` text NULL, `public` bool NOT NULL DEFAULT (false), `track_ids` json NULL, `user_playlists` integer NOT NULL, CONSTRAINT `playlists_users_playlists` FOREIGN KEY (`user_playlists`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create index "playlist_user_playlists" to table: "playlists"
CREATE INDEX `playlist_user_playlists` ON `playlists` (`user_playlists`);
-- create "radio_stations" table
CREATE TABLE `radio_stations` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `name` text NOT NULL, `stream_url` text NOT NULL, `homepage_url` text NULL);
-- create "shares" table
CREATE TABLE `shares` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `token` text NOT NULL, `description` text NULL, `items` json NOT NULL, `expires_at` datetime NULL, `last_visited_at` datetime NULL, `visit_count` integer NOT NULL DEFAULT (0), `user_shares` integer NOT NULL, CONSTRAINT `shares_users_shares` FOREIGN KEY (`user_shares`) REFERENCES `users` (`id`) ON DELETE CASCADE);
-- create index "share_token" to table: "shares"
CREATE UNIQUE INDEX `share_token` ON `shares` (`token`);
-- create index "share_user_shares" to table: "shares"
CREATE INDEX `share_user_shares` ON `shares` (`user_shares`);
