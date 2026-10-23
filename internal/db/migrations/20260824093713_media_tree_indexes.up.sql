-- create index "episode_season_episodes" to table: "episodes"
CREATE INDEX `episode_season_episodes` ON `episodes` (`season_episodes`);
-- create index "season_tv_show_seasons" to table: "seasons"
CREATE INDEX `season_tv_show_seasons` ON `seasons` (`tv_show_seasons`);
-- create index "mediafile_episode_media_files" to table: "media_files"
CREATE INDEX `mediafile_episode_media_files` ON `media_files` (`episode_media_files`);
-- create index "mediafile_movie_media_files" to table: "media_files"
CREATE INDEX `mediafile_movie_media_files` ON `media_files` (`movie_media_files`);
