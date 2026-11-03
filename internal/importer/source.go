package importer

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/library"
)

// sourceFilter keeps an import to the files of the record's own torrent.
// save_path is derived as <download_path>/<torrent name>, so two torrents
// named alike share one folder — RuTracker roots many uploads at the artist's
// name — and a walk of it read the other torrent's files as this release: an
// Omens LP held on ten tracks of Resolution, and a resolve would have imported
// them into its slots. A single-file payload needs no filter. A client that
// cannot list the torrent (removed, unreachable, no metadata) leaves the walk
// unfiltered, which is what every import did before.
func (w *Worker) sourceFilter(
	ctx context.Context,
	rec *ent.DownloadRecord,
) library.SourceFilter {
	if w.dl == nil || rec.TorrentHash == "" || rec.DownloadClientName == "" {
		return nil
	}
	if info, err := os.Stat(rec.SavePath); err != nil || !info.IsDir() {
		return nil
	}
	files, err := w.dl.ListTorrentFiles(ctx, rec.DownloadClientName, rec.TorrentHash)
	if err != nil || len(files) == 0 {
		slog.WarnContext(
			ctx,
			"torrent file list unavailable, importing every file under the save path",
			"download_record.id",
			rec.ID,
			"save_path",
			rec.SavePath,
			"error",
			err,
		)
		return nil
	}
	root := filepath.Clean(rec.SavePath)
	name := filepath.Base(root)
	owned := make(map[string]struct{}, len(files))
	for _, f := range files {
		// The builtin engine reports a path inside the torrent's folder;
		// qBittorrent, Transmission and Deluge lead with the folder itself.
		// Both readings are kept, since an inner folder can carry the
		// torrent's own name.
		owned[filepath.Join(root, filepath.FromSlash(f.Path))] = struct{}{}
		if rest, ok := strings.CutPrefix(f.Path, name+"/"); ok {
			owned[filepath.Join(root, filepath.FromSlash(rest))] = struct{}{}
		}
	}
	return func(p string) bool {
		_, ok := owned[filepath.Clean(p)]
		return ok
	}
}
