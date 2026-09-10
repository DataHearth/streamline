-- add column "biography" to table: "persons"
ALTER TABLE `persons` ADD COLUMN `biography` text NULL;
-- add column "known_for" to table: "persons"
ALTER TABLE `persons` ADD COLUMN `known_for` text NULL;
-- add column "birthday" to table: "persons"
ALTER TABLE `persons` ADD COLUMN `birthday` text NULL;
-- add column "deathday" to table: "persons"
ALTER TABLE `persons` ADD COLUMN `deathday` text NULL;
-- add column "place_of_birth" to table: "persons"
ALTER TABLE `persons` ADD COLUMN `place_of_birth` text NULL;
-- add column "imdb_id" to table: "persons"
ALTER TABLE `persons` ADD COLUMN `imdb_id` text NULL;
-- add column "instagram_id" to table: "persons"
ALTER TABLE `persons` ADD COLUMN `instagram_id` text NULL;
-- add column "twitter_id" to table: "persons"
ALTER TABLE `persons` ADD COLUMN `twitter_id` text NULL;
-- add column "details_fetched_at" to table: "persons"
ALTER TABLE `persons` ADD COLUMN `details_fetched_at` datetime NULL;
