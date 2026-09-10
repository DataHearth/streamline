-- Carry the cast the library already holds into persons/credits. The previous
-- migration only creates the two tables; without this, every existing title
-- shows an empty cast until something refreshes its metadata, which for a
-- settled library is never.
--
-- Three quoting/NULL traps, all load-bearing:
--   * the column is literally named `cast`, a SQLite keyword;
--   * `order` is one too;
--   * json_each(NULL) is an error, and an optional JSON column that was never
--     written is NULL rather than '[]', hence COALESCE.
--
-- Identity follows the write path (resolvePerson in internal/db/cast.go), in
-- the same order: tmdb_id when non-zero, else tvdb_id when non-zero, else the
-- folded name. Stored entries predate the tvdb_id field entirely, so movie
-- cast keys on tmdb_id and series cast keys name-only; the series people gain
-- their real ids on the next metadata refresh, which finds them by name.
--
-- The name key is folded with replace()/lower() rather than with the Go fold()
-- the runtime registers on its connection — a migration has no access to it.
-- It lower-cases and turns the punctuation that occurs in cast names into
-- spaces, but leaves diacritics alone: stripping those in SQL costs a
-- replace() per accented letter, and the entries merged here come from one
-- provider that spells a given person one way. A pair this key does keep apart
-- is merged by the runtime fold on the next refresh, which matches on
-- fold(persons.name) and not on this key.
--
-- persons and credits are empty at this point — the tables were created one
-- migration ago and migrations run before the app writes anything — so the
-- inserts below need no de-duplication against existing rows.

CREATE TABLE `_cast_backfill` (
  `owner_kind`  text NOT NULL,
  `owner_id`    integer NOT NULL,
  `ord`         integer NOT NULL,
  `tmdb_id`     integer NOT NULL,
  `tvdb_id`     integer NOT NULL,
  `name`        text NOT NULL,
  `character`   text NOT NULL,
  `profile_url` text NOT NULL,
  `fold_name`   text NOT NULL,
  `identity`    text NOT NULL
);

-- `ord` is the entry's position in the provider's list, capped at 255 because
-- credits.order is a uint8 on the Go side.
INSERT INTO `_cast_backfill`
SELECT 'movie', m.`id`, min(c.`key`, 255),
       CAST(COALESCE(json_extract(c.`value`, '$.tmdb_id'), 0) AS INTEGER),
       CAST(COALESCE(json_extract(c.`value`, '$.tvdb_id'), 0) AS INTEGER),
       COALESCE(json_extract(c.`value`, '$.name'), ''),
       COALESCE(json_extract(c.`value`, '$.character'), ''),
       COALESCE(json_extract(c.`value`, '$.profile_url'), ''),
       '', ''
FROM `movies` m, json_each(COALESCE(m.`cast`, '[]')) c
WHERE COALESCE(json_extract(c.`value`, '$.name'), '') <> '';

INSERT INTO `_cast_backfill`
SELECT 'series', t.`id`, min(c.`key`, 255),
       CAST(COALESCE(json_extract(c.`value`, '$.tmdb_id'), 0) AS INTEGER),
       CAST(COALESCE(json_extract(c.`value`, '$.tvdb_id'), 0) AS INTEGER),
       COALESCE(json_extract(c.`value`, '$.name'), ''),
       COALESCE(json_extract(c.`value`, '$.character'), ''),
       COALESCE(json_extract(c.`value`, '$.profile_url'), ''),
       '', ''
FROM `tv_shows` t, json_each(COALESCE(t.`cast`, '[]')) c
WHERE COALESCE(json_extract(c.`value`, '$.name'), '') <> '';

UPDATE `_cast_backfill` SET `fold_name` =
  trim(replace(replace(replace(replace(replace(replace(
    lower(`name`),
    '.', ' '), ',', ' '), '-', ' '), '''', ' '), '  ', ' '), '  ', ' '));

UPDATE `_cast_backfill` SET `identity` = CASE
  WHEN `tmdb_id` <> 0 THEN 'tmdb:' || `tmdb_id`
  WHEN `tvdb_id` <> 0 THEN 'tvdb:' || `tvdb_id`
  ELSE 'name:' || `fold_name`
END;

-- An entry carrying no id at all joins the person an id-carrying entry of the
-- same folded name already established, which is what resolvePerson's name
-- fallback does at runtime: 0 means "this provider gave us no id for them",
-- and an unknown id contradicts nothing. Without this, an actor whose movie
-- cast came from TMDB and whose series cast came from TVDB is backfilled as
-- two people — the same fragmentation this change exists to remove.
UPDATE `_cast_backfill` SET `identity` = COALESCE((
  SELECT MIN(o.`identity`) FROM `_cast_backfill` o
  WHERE o.`fold_name` = `_cast_backfill`.`fold_name`
    AND (o.`tmdb_id` <> 0 OR o.`tvdb_id` <> 0)
), `identity`)
WHERE `tmdb_id` = 0 AND `tvdb_id` = 0;

CREATE TABLE `_person_backfill` (
  `identity`    text NOT NULL PRIMARY KEY,
  `tmdb_id`     integer NOT NULL,
  `tvdb_id`     integer NOT NULL,
  `name`        text NOT NULL,
  `profile_url` text NOT NULL,
  `person_id`   integer NULL
);

-- One row per identity. MIN(name) picks a deterministic spelling and is what
-- the persons row below is written with, which is what lets the credits step
-- find that row again without folding a second time. MAX(profile_url) is "the
-- first non-empty one": '' sorts below every real value.
INSERT INTO `_person_backfill`
SELECT `identity`, MAX(`tmdb_id`), MAX(`tvdb_id`), MIN(`name`),
       MAX(`profile_url`), NULL
FROM `_cast_backfill`
GROUP BY `identity`;

INSERT INTO `persons` (`create_time`, `update_time`, `tmdb_id`, `tvdb_id`,
                       `name`, `profile_url`)
SELECT datetime('now'), datetime('now'), `tmdb_id`, `tvdb_id`, `name`,
       `profile_url`
FROM `_person_backfill`;

UPDATE `_person_backfill` SET `person_id` = (
  SELECT MIN(p.`id`) FROM `persons` p
  WHERE p.`name` = `_person_backfill`.`name`
    AND p.`tmdb_id` = `_person_backfill`.`tmdb_id`
    AND p.`tvdb_id` = `_person_backfill`.`tvdb_id`
);

INSERT INTO `credits` (`create_time`, `update_time`, `character`, `order`,
                       `person_credits`, `movie_credits`, `tv_show_credits`)
SELECT datetime('now'), datetime('now'), e.`character`, e.`ord`, b.`person_id`,
       CASE WHEN e.`owner_kind` = 'movie' THEN e.`owner_id` END,
       CASE WHEN e.`owner_kind` = 'series' THEN e.`owner_id` END
FROM `_cast_backfill` e
JOIN `_person_backfill` b ON b.`identity` = e.`identity`
WHERE b.`person_id` IS NOT NULL;

DROP TABLE `_person_backfill`;
DROP TABLE `_cast_backfill`;
