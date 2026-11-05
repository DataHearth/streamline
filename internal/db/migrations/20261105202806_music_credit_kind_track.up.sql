-- create index "musiccredit_kind_track_credits" to table: "music_credits"
CREATE INDEX `musiccredit_kind_track_credits` ON `music_credits` (`kind`, `track_credits`);
