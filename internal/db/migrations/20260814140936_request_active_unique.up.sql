-- Collapse duplicate active requests left behind by the check-then-insert race
-- this index exists to close, keeping the earliest row of each set. Without
-- this, upgrading a database that already lost the race fails here, and the
-- index that prevents any further duplicates never gets created.
DELETE FROM `requests`
WHERE `status` IN ('pending', 'approved', 'available')
  AND `id` NOT IN (
    SELECT MIN(`id`)
    FROM `requests`
    WHERE `status` IN ('pending', 'approved', 'available')
    GROUP BY `media_type`, `media_id`
  );

-- create index "request_media_type_media_id" to table: "requests"
CREATE UNIQUE INDEX `request_media_type_media_id` ON `requests` (`media_type`, `media_id`) WHERE status IN ('pending', 'approved', 'available');
