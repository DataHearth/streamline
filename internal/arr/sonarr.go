package arr

import (
	"context"
	"strconv"
)

func (c *client) Series(ctx context.Context) ([]Series, error) {
	return list[Series](ctx, c, "/series")
}

// Episodes asks for the embedded file record, which carries path and size, so
// a show costs one request instead of two on a Sonarr that honours
// includeEpisodeFile. Sonarr only gained that parameter in 4.0.10: 3.x and
// earlier 4.0 builds answer without any episodeFile, which would migrate every
// show with no files at all and leave its whole library wanted. An episode
// that names a file (episodeFileId) without embedding it is therefore filled
// from GET /episodefile?seriesId=, which every v3 API serves, joined on the id.
func (c *client) Episodes(ctx context.Context, seriesID uint32) ([]Episode, error) {
	id := strconv.FormatUint(uint64(seriesID), 10)
	eps, err := get[[]Episode](
		ctx, c, "/episode", "seriesId="+id+"&includeEpisodeFile=true",
	)
	if err != nil {
		return nil, err
	}
	missing := false
	for _, e := range eps {
		if e.EpisodeFileID != 0 && e.EpisodeFile == nil {
			missing = true
			break
		}
	}
	if !missing {
		return eps, nil
	}
	files, err := get[[]EpisodeFile](ctx, c, "/episodefile", "seriesId="+id)
	if err != nil {
		return nil, err
	}
	byID := make(map[uint32]*EpisodeFile, len(files))
	for i := range files {
		byID[files[i].ID] = &files[i]
	}
	for i := range eps {
		if eps[i].EpisodeFile == nil && eps[i].EpisodeFileID != 0 {
			eps[i].EpisodeFile = byID[eps[i].EpisodeFileID]
		}
	}
	return eps, nil
}
