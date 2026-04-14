-- reverse: create index "importscanshow_decision" to table: "import_scan_shows"
DROP INDEX `importscanshow_decision`;
-- reverse: create index "importscanshow_classification" to table: "import_scan_shows"
DROP INDEX `importscanshow_classification`;
-- reverse: create "import_scan_shows" table
DROP TABLE `import_scan_shows`;
-- reverse: create index "importscan_kind" to table: "import_scans"
DROP INDEX `importscan_kind`;
-- reverse: create index "importscan_status" to table: "import_scans"
DROP INDEX `importscan_status`;
-- reverse: create "new_import_scans" table
DROP TABLE `new_import_scans`;
