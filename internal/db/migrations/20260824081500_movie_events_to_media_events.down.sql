CREATE TABLE `movie_events` (`id` integer NOT NULL PRIMARY KEY AUTOINCREMENT, `create_time` datetime NOT NULL, `update_time` datetime NOT NULL, `type` text NOT NULL, `payload` json NULL, `movie_events` integer NOT NULL, CONSTRAINT `movie_events_movies_events` FOREIGN KEY (`movie_events`) REFERENCES `movies` (`id`) ON DELETE CASCADE);
CREATE INDEX `movieevent_create_time` ON `movie_events` (`create_time`);
CREATE INDEX `movieevent_type_create_time` ON `movie_events` (`type`, `create_time`);
-- Episode- and series-owned rows have no home in the old table and are dropped.
INSERT INTO `movie_events` (`id`, `create_time`, `update_time`, `type`, `payload`, `movie_events`)
SELECT `id`, `create_time`, `update_time`, `type`, `payload`, `movie_events` FROM `media_events` WHERE `movie_events` IS NOT NULL;
CREATE INDEX `movieevent_create_time_movie_events` ON `movie_events` (`create_time`, `movie_events`);
