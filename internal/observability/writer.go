package observability

import (
	"io"
	"os"
	"path/filepath"

	"github.com/DeRuina/timberjack"
	"github.com/datahearth/streamline/internal/config"
)

// StderrSink is the destination for log output configured as "stderr"
// (log.app.output / log.http.output) when no explicit writer is supplied.
// Defaults to os.Stderr. The server test suite repoints it at GinkgoWriter
// (via testutil) so the HTTP access logger — constructed deep inside
// BuildApp with no injection seam — doesn't interleave JSON lines with spec
// output.
var StderrSink io.Writer = os.Stderr

// openLogWriter resolves an Output value into a writer. stderr returns the
// fallback writer (defaulting to StderrSink) with a nil closer; file paths
// return a *timberjack.Logger with rotation knobs applied and a Close() that
// flushes the active file. Relative paths resolve under the configured
// data_dir so logs land with the rest of the runtime data instead of the
// cwd.
func openLogWriter(
	output string,
	rot config.LogRotate,
	fallback io.Writer,
) (io.Writer, io.Closer) {
	if output == "" || output == "stderr" {
		if fallback == nil {
			return StderrSink, nil
		}
		return fallback, nil
	}
	if !filepath.IsAbs(output) {
		if cfg := config.Get(); cfg != nil {
			output = filepath.Join(cfg.DataDir, output)
		}
	}
	tj := &timberjack.Logger{
		Filename:   output,
		MaxSize:    rot.MaxSizeMB,
		MaxBackups: rot.MaxBackups,
		MaxAge:     rot.MaxAgeDays,
		Compress:   rot.Compress,
	}
	return tj, tj
}
