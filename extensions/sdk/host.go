package sdk

//go:wasmimport extism:host/user sql_query
func hostSQLQuery(offset uint64) uint64

//go:wasmimport extism:host/user sql_exec
func hostSQLExec(offset uint64) uint64

//go:wasmimport extism:host/user log
func hostLog(offset uint64)

//go:wasmimport extism:host/user ffmpeg_run
func hostFFmpegRun(offset uint64) uint64

//go:wasmimport extism:host/user scan_status
func hostScanStatus(offset uint64) uint64

//go:wasmimport extism:host/user scan_start
func hostScanStart(offset uint64) uint64
