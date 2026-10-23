-- add column "subsonic_password" to table: "users"
ALTER TABLE `users` ADD COLUMN `subsonic_password` text NULL;
