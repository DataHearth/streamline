-- reverse: add the Subsonic permission columns to table: "users"
ALTER TABLE `users` DROP COLUMN `subsonic_max_bit_rate`;
ALTER TABLE `users` DROP COLUMN `subsonic_can_change_password`;
ALTER TABLE `users` DROP COLUMN `subsonic_can_rate`;
ALTER TABLE `users` DROP COLUMN `subsonic_can_share`;
ALTER TABLE `users` DROP COLUMN `subsonic_can_manage_playlists`;
ALTER TABLE `users` DROP COLUMN `subsonic_can_download`;
ALTER TABLE `users` DROP COLUMN `subsonic_can_stream`;
