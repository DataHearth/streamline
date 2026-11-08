-- Runs after the text migration's down has rebuilt the id columns as integers.
-- That rebuild copies '603' back as 603, but NULL and the empty string the
-- text columns came to hold stay what they are, and the integer-typed code
-- reads neither: the columns that defaulted to 0 get 0 back, the others NULL.
UPDATE `persons` SET `tmdb_id` = 0 WHERE `tmdb_id` IS NULL OR `tmdb_id` = '';
UPDATE `persons` SET `tvdb_id` = 0 WHERE `tvdb_id` IS NULL OR `tvdb_id` = '';
UPDATE `requests` SET `media_id` = 0 WHERE `media_id` IS NULL OR `media_id` = '';
UPDATE `import_scan_files` SET `tmdb_id` = NULL WHERE `tmdb_id` = '';
UPDATE `import_scan_files` SET `decision_tmdb_id` = NULL WHERE `decision_tmdb_id` = '';
UPDATE `import_scan_shows` SET `tvdb_id` = NULL WHERE `tvdb_id` = '';
UPDATE `import_scan_shows` SET `decision_tvdb_id` = NULL WHERE `decision_tvdb_id` = '';
UPDATE `import_scan_books` SET `book_hardcover_id` = NULL WHERE `book_hardcover_id` = '';
UPDATE `import_scan_books` SET `decision_book_hardcover_id` = NULL WHERE `decision_book_hardcover_id` = '';
UPDATE `movies` SET `tmdb_id` = 0 WHERE `tmdb_id` = '';
UPDATE `tv_shows` SET `tvdb_id` = 0 WHERE `tvdb_id` = '';
UPDATE `authors` SET `hardcover_id` = 0 WHERE `hardcover_id` = '';
UPDATE `books` SET `hardcover_id` = 0 WHERE `hardcover_id` = '';
UPDATE `book_series` SET `hardcover_id` = 0 WHERE `hardcover_id` = '';
UPDATE `book_editions` SET `hardcover_edition_id` = 0 WHERE `hardcover_edition_id` = '';

-- The candidates' ids back to numbers, '' to 0.
UPDATE `import_scan_files` SET `candidates` = (
  SELECT json_group_array(
    json_set(c.`value`, '$.tmdb_id',
      CAST(COALESCE(NULLIF(json_extract(c.`value`, '$.tmdb_id'), ''), 0) AS INTEGER))
    ORDER BY CAST(c.`key` AS INTEGER))
  FROM json_each(`import_scan_files`.`candidates`) AS c
)
WHERE json_valid(`candidates`) AND json_type(`candidates`) = 'array'
  AND json_array_length(`candidates`) > 0;

UPDATE `import_scan_shows` SET `candidates` = (
  SELECT json_group_array(
    json_set(c.`value`, '$.tvdb_id',
      CAST(COALESCE(NULLIF(json_extract(c.`value`, '$.tvdb_id'), ''), 0) AS INTEGER))
    ORDER BY CAST(c.`key` AS INTEGER))
  FROM json_each(`import_scan_shows`.`candidates`) AS c
)
WHERE json_valid(`candidates`) AND json_type(`candidates`) = 'array'
  AND json_array_length(`candidates`) > 0;

UPDATE `import_scan_books` SET `candidates` = (
  SELECT json_group_array(
    json_set(c.`value`, '$.book_hardcover_id',
      CAST(COALESCE(NULLIF(json_extract(c.`value`, '$.book_hardcover_id'), ''), 0) AS INTEGER))
    ORDER BY CAST(c.`key` AS INTEGER))
  FROM json_each(`import_scan_books`.`candidates`) AS c
)
WHERE json_valid(`candidates`) AND json_type(`candidates`) = 'array'
  AND json_array_length(`candidates`) > 0;
