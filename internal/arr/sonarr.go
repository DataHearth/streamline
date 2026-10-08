package arr

import (
	"context"
	"strconv"
)

func (c *client) Series(ctx context.Context) ([]Series, error) {
	return get[[]Series](ctx, c, "/series", "")
}

// Episodes asks for the embedded file record, which carries path and size, so
// the separate /episodefile route is never needed and a show costs one
// request instead of two.
func (c *client) Episodes(ctx context.Context, seriesID uint32) ([]Episode, error) {
	q := "seriesId=" + strconv.FormatUint(uint64(seriesID), 10) +
		"&includeEpisodeFile=true"
	return get[[]Episode](ctx, c, "/episode", q)
}
