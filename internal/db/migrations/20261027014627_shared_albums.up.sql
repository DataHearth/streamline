-- create "artist_co_albums" table
CREATE TABLE `artist_co_albums` (`artist_id` integer NOT NULL, `album_id` integer NOT NULL, PRIMARY KEY (`artist_id`, `album_id`), CONSTRAINT `artist_co_albums_artist_id` FOREIGN KEY (`artist_id`) REFERENCES `artists` (`id`) ON DELETE CASCADE, CONSTRAINT `artist_co_albums_album_id` FOREIGN KEY (`album_id`) REFERENCES `albums` (`id`) ON DELETE CASCADE);
