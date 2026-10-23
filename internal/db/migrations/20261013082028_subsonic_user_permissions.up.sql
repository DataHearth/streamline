-- add the Subsonic permission columns to table: "users"; existing accounts keep every permission
ALTER TABLE `users` ADD COLUMN `subsonic_can_stream` bool NOT NULL DEFAULT (true);
ALTER TABLE `users` ADD COLUMN `subsonic_can_download` bool NOT NULL DEFAULT (true);
ALTER TABLE `users` ADD COLUMN `subsonic_can_manage_playlists` bool NOT NULL DEFAULT (true);
ALTER TABLE `users` ADD COLUMN `subsonic_can_share` bool NOT NULL DEFAULT (true);
ALTER TABLE `users` ADD COLUMN `subsonic_can_rate` bool NOT NULL DEFAULT (true);
ALTER TABLE `users` ADD COLUMN `subsonic_can_change_password` bool NOT NULL DEFAULT (true);
ALTER TABLE `users` ADD COLUMN `subsonic_max_bit_rate` integer NOT NULL DEFAULT (0);
