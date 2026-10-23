-- canonicalRelease compared MusicBrainz dates as raw strings, so a release
-- dated only by its year ("2022") beat every fully dated one from that year
-- ("2022-10-07"): Lamb of God's Omens took a Russian pressing with three
-- bonus tracks, and every standard 10-track release then looked 3 files
-- short. The pick is fixed, but a hydrated album is never re-picked.
--
-- Only an album whose stored edition date is partial can have won that way.
-- Unstamping it hands it back to the hydration worker, which re-picks and
-- drops the tracks the new edition does not list. An album holding any track
-- file is left alone: its files were matched to the edition it has.
UPDATE `albums` SET `metadata_fetched_at` = NULL
WHERE length(`edition_date`) IN (4, 7)
  AND `metadata_fetched_at` IS NOT NULL
  AND NOT EXISTS (
    SELECT 1 FROM `tracks`
    JOIN `media_files` ON `media_files`.`track_media_files` = `tracks`.`id`
    WHERE `tracks`.`album_tracks` = `albums`.`id`
  );
