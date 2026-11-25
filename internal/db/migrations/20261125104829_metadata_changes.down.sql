-- reverse: create index "metadatachange_state" to table: "metadata_changes"
DROP INDEX `metadatachange_state`;
-- reverse: create index "metadatachange_kind_book_metadata_changes" to table: "metadata_changes"
DROP INDEX `metadatachange_kind_book_metadata_changes`;
-- reverse: create index "metadatachange_kind_movie_metadata_changes" to table: "metadata_changes"
DROP INDEX `metadatachange_kind_movie_metadata_changes`;
-- reverse: create index "metadatachange_kind_tv_show_metadata_changes" to table: "metadata_changes"
DROP INDEX `metadatachange_kind_tv_show_metadata_changes`;
-- reverse: create index "metadatachange_kind_album_metadata_changes" to table: "metadata_changes"
DROP INDEX `metadatachange_kind_album_metadata_changes`;
-- reverse: create index "metadatachange_kind_artist_metadata_changes" to table: "metadata_changes"
DROP INDEX `metadatachange_kind_artist_metadata_changes`;
-- reverse: create "metadata_changes" table
DROP TABLE `metadata_changes`;
