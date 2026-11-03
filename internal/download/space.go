package download

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/datahearth/streamline/internal/config"
)

const gigabyte = 1_000_000_000

// checkFreeSpace refuses a grab of size bytes when it would not fit on the
// client's disk beside what the client's torrents still have to write,
// keeping download.space_reserve_gb free. Counting the in-flight remainder is
// what stops two grabs a minute apart from both claiming the same free space.
//
// A check that cannot be made lets the grab through: an unknown size, a client
// without FreeSpacer, or one that fails to answer must not stop every
// download.
func checkFreeSpace(ctx context.Context, client Client, size int64) error {
	cfg := config.Get().Download
	spacer, ok := client.(FreeSpacer)
	if !cfg.SpaceCheck || !ok || size <= 0 {
		return nil
	}
	free, err := spacer.FreeSpace(ctx)
	if err != nil {
		slog.WarnContext(ctx, "free space unavailable, grab not checked",
			"error", err)
		return nil
	}
	torrents, err := client.ListTorrents(ctx)
	if err != nil {
		slog.WarnContext(ctx, "torrent list unavailable, grab not checked",
			"error", err)
		return nil
	}
	// ponytail: Transmission and Deluge report the whole torrent's size, so a
	// selective download's remainder is overcounted; the error is on the
	// refusing side.
	var pending int64
	for _, t := range torrents {
		pending += int64(float64(t.Size) * max(0, 1-t.Progress))
	}
	reserve := int64(cfg.SpaceReserveGB) * gigabyte
	if size+pending+reserve <= free {
		return nil
	}
	return fmt.Errorf(
		"%w: needs %s, %s free with %s still downloading and %s kept in reserve",
		ErrNoSpace, gb(size), gb(free), gb(pending), gb(reserve),
	)
}

func gb(n int64) string {
	return fmt.Sprintf("%.1f GB", float64(n)/gigabyte)
}
