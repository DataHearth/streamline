-- Prepares the data for the next migration, which turns every TMDB, TVDB and
-- Hardcover id column into text. That one is Atlas's table rebuild and copies
-- each value as it is: 603 becomes '603', and 0 would become '0'.
--
-- 0 was never an id. It is what an optional column held when the provider gave
-- none — a person from TVDB has no tmdb_id, an artist or album request has no
-- media_id, an unmatched scan row has no tmdb_id — and the code now reads an
-- empty id as "none". So 0 becomes NULL here, while the columns are still
-- integers, and the rebuild copies NULL.
--
-- The requests one is load-bearing, not tidiness: the rebuild recreates the
-- partial unique index on (media_type, media_id) with `media_id <> ''`, and
-- every artist and album request holds media_id 0. Left as '0', two active
-- artist requests would collide on it and the migration would fail.
UPDATE `persons` SET `tmdb_id` = NULL WHERE `tmdb_id` = 0;
UPDATE `persons` SET `tvdb_id` = NULL WHERE `tvdb_id` = 0;
UPDATE `requests` SET `media_id` = NULL WHERE `media_id` = 0;
UPDATE `import_scan_files` SET `tmdb_id` = NULL WHERE `tmdb_id` = 0;
UPDATE `import_scan_files` SET `decision_tmdb_id` = NULL WHERE `decision_tmdb_id` = 0;
UPDATE `import_scan_shows` SET `tvdb_id` = NULL WHERE `tvdb_id` = 0;
UPDATE `import_scan_shows` SET `decision_tvdb_id` = NULL WHERE `decision_tvdb_id` = 0;
UPDATE `import_scan_books` SET `book_hardcover_id` = NULL WHERE `book_hardcover_id` = 0;
UPDATE `import_scan_books` SET `decision_book_hardcover_id` = NULL WHERE `decision_book_hardcover_id` = 0;

-- A scan row's candidates are JSON arrays whose entries carry the id as a
-- number; the code now decodes it as a string, and a number would fail the
-- whole row. Each entry's id is rewritten as text, 0 as '' — the rebuild
-- leaves JSON alone, so this is the only place it happens. json_set inside
-- json_group_array keeps each entry an object (SQLite passes the JSON subtype
-- along), and the ORDER BY keeps the candidates in their ranked order.
UPDATE `import_scan_files` SET `candidates` = (
  SELECT json_group_array(
    json_set(c.`value`, '$.tmdb_id',
      CASE WHEN COALESCE(json_extract(c.`value`, '$.tmdb_id'), 0) = 0 THEN ''
           ELSE CAST(json_extract(c.`value`, '$.tmdb_id') AS TEXT) END)
    ORDER BY CAST(c.`key` AS INTEGER))
  FROM json_each(`import_scan_files`.`candidates`) AS c
)
WHERE json_valid(`candidates`) AND json_type(`candidates`) = 'array'
  AND json_array_length(`candidates`) > 0;

UPDATE `import_scan_shows` SET `candidates` = (
  SELECT json_group_array(
    json_set(c.`value`, '$.tvdb_id',
      CASE WHEN COALESCE(json_extract(c.`value`, '$.tvdb_id'), 0) = 0 THEN ''
           ELSE CAST(json_extract(c.`value`, '$.tvdb_id') AS TEXT) END)
    ORDER BY CAST(c.`key` AS INTEGER))
  FROM json_each(`import_scan_shows`.`candidates`) AS c
)
WHERE json_valid(`candidates`) AND json_type(`candidates`) = 'array'
  AND json_array_length(`candidates`) > 0;

UPDATE `import_scan_books` SET `candidates` = (
  SELECT json_group_array(
    json_set(c.`value`, '$.book_hardcover_id',
      CASE WHEN COALESCE(json_extract(c.`value`, '$.book_hardcover_id'), 0) = 0 THEN ''
           ELSE CAST(json_extract(c.`value`, '$.book_hardcover_id') AS TEXT) END)
    ORDER BY CAST(c.`key` AS INTEGER))
  FROM json_each(`import_scan_books`.`candidates`) AS c
)
WHERE json_valid(`candidates`) AND json_type(`candidates`) = 'array'
  AND json_array_length(`candidates`) > 0;
