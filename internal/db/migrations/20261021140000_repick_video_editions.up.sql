-- Hydration took a CD + DVD edition's video chapters for tracks, and the
-- canonical pick kept whichever same-day release MusicBrainz listed first:
-- Avatar's Feathers & Flesh came out on CD and on CD + live DVD the same day,
-- and its DVD became a second disc of eight tracks no audio release carries,
-- so the album could never be complete. Both are fixed, but a hydrated album
-- is never re-picked.
--
-- A video disc lands as a disc after the first that no file fills. Unstamping
-- every album with such a disc hands it back to the hydration worker, which
-- re-picks the edition and drops the tracks it no longer lists unless they
-- hold a file. An album missing a real second disc is fetched again for
-- nothing and comes back the same.
UPDATE `albums` SET `metadata_fetched_at` = NULL
WHERE `metadata_fetched_at` IS NOT NULL
  AND EXISTS (
    SELECT 1 FROM `tracks` AS `t`
    WHERE `t`.`album_tracks` = `albums`.`id`
      AND `t`.`disc` > 1
      AND NOT EXISTS (
        SELECT 1 FROM `tracks` AS `held`
        JOIN `media_files` ON `media_files`.`track_media_files` = `held`.`id`
        WHERE `held`.`album_tracks` = `albums`.`id` AND `held`.`disc` = `t`.`disc`
      )
  );
