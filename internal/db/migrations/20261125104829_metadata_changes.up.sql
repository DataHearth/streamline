-- create "metadata_changes" table
CREATE TABLE `metadata_changes` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `kind` text NOT NULL, `state` text NOT NULL DEFAULT ('pending'), `fingerprint` text NOT NULL DEFAULT (''), `payload` json NULL, `album_metadata_changes` integer NULL, `artist_metadata_changes` integer NULL, `book_metadata_changes` integer NULL, `movie_metadata_changes` integer NULL, `tv_show_metadata_changes` integer NULL, CONSTRAINT `metadata_changes_albums_metadata_changes` FOREIGN KEY (`album_metadata_changes`) REFERENCES `albums` (`id`) ON DELETE CASCADE, CONSTRAINT `metadata_changes_artists_metadata_changes` FOREIGN KEY (`artist_metadata_changes`) REFERENCES `artists` (`id`) ON DELETE CASCADE, CONSTRAINT `metadata_changes_books_metadata_changes` FOREIGN KEY (`book_metadata_changes`) REFERENCES `books` (`id`) ON DELETE CASCADE, CONSTRAINT `metadata_changes_movies_metadata_changes` FOREIGN KEY (`movie_metadata_changes`) REFERENCES `movies` (`id`) ON DELETE CASCADE, CONSTRAINT `metadata_changes_tv_shows_metadata_changes` FOREIGN KEY (`tv_show_metadata_changes`) REFERENCES `tv_shows` (`id`) ON DELETE CASCADE);
-- create index "metadatachange_kind_artist_metadata_changes" to table: "metadata_changes"
CREATE UNIQUE INDEX `metadatachange_kind_artist_metadata_changes` ON `metadata_changes` (`kind`, `artist_metadata_changes`);
-- create index "metadatachange_kind_album_metadata_changes" to table: "metadata_changes"
CREATE UNIQUE INDEX `metadatachange_kind_album_metadata_changes` ON `metadata_changes` (`kind`, `album_metadata_changes`);
-- create index "metadatachange_kind_tv_show_metadata_changes" to table: "metadata_changes"
CREATE UNIQUE INDEX `metadatachange_kind_tv_show_metadata_changes` ON `metadata_changes` (`kind`, `tv_show_metadata_changes`);
-- create index "metadatachange_kind_movie_metadata_changes" to table: "metadata_changes"
CREATE UNIQUE INDEX `metadatachange_kind_movie_metadata_changes` ON `metadata_changes` (`kind`, `movie_metadata_changes`);
-- create index "metadatachange_kind_book_metadata_changes" to table: "metadata_changes"
CREATE UNIQUE INDEX `metadatachange_kind_book_metadata_changes` ON `metadata_changes` (`kind`, `book_metadata_changes`);
-- create index "metadatachange_state" to table: "metadata_changes"
CREATE INDEX `metadatachange_state` ON `metadata_changes` (`state`);
