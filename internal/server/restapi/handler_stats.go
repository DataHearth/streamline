package restapi

import (
	"context"
	"time"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/sysinfo"
)

var statsRangeDays = map[StatsRange]int{
	StatsRangeN7d:  7,
	StatsRangeN30d: 30,
	StatsRangeN90d: 90,
}

// GetStats answers the admin stats page. Admin only: it names every account.
func (s *Server) GetStats(
	ctx context.Context,
	request GetStatsRequestObject,
) (GetStatsResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetStats403JSONResponse{ForbiddenJSONResponse: notAdminResp}, nil
	}
	rng := StatsRangeN30d
	if request.Params.Range != nil {
		rng = *request.Params.Range
	}
	days, ok := statsRangeDays[rng]
	if !ok {
		return GetStats400JSONResponse{
			BadRequestJSONResponse: errBadRequest("range must be 7d, 30d or 90d"),
		}, nil
	}
	now := time.Now()
	st, err := s.store.GetStats(ctx, days, now)
	if err != nil {
		return GetStats500JSONResponse{
			InternalErrorJSONResponse: errInternal(ctx, err),
		}, nil
	}
	return GetStats200JSONResponse{
		StatsResponseJSONResponse: StatsResponseJSONResponse(toStats(st, rng, now)),
	}, nil
}

// statsVolumes lists the library roots and the data directory. Two roots on one
// path are one row; a root that cannot be read is left out.
func statsVolumes() []StatsVolume {
	cfg := config.Get()
	roots := []struct{ key, path string }{
		{"movies", cfg.Library.MoviePath},
		{"series", cfg.Library.SeriesPath},
		{"music", cfg.Library.MusicPath},
		{"ebooks", cfg.Library.EbookPath},
		{"audiobooks", cfg.Library.AudiobookPath},
		{"data", cfg.DataDir},
	}
	out := make([]StatsVolume, 0, len(roots))
	seen := map[string]bool{}
	for _, r := range roots {
		if seen[r.path] {
			continue
		}
		seen[r.path] = true
		if u := sysinfo.DiskUsageFor(r.path); u != nil {
			out = append(out, StatsVolume{
				Key:        r.key,
				Path:       r.path,
				UsedBytes:  u.TotalBytes - u.FreeBytes,
				TotalBytes: u.TotalBytes,
			})
		}
	}
	return out
}

func statsSlices(in []db.StatsSlice) []StatsSlice {
	out := make([]StatsSlice, len(in))
	for i, v := range in {
		out[i] = StatsSlice{Key: v.Key, Count: v.Count, Bytes: v.Bytes}
	}
	return out
}

func toStats(st db.Stats, rng StatsRange, now time.Time) Stats {
	media := make([]StatsMedia, len(st.Library.Media))
	for i, m := range st.Library.Media {
		media[i] = StatsMedia{
			Kind:   StatsKind(m.Kind),
			Titles: m.Titles,
			Files:  m.Files,
			Bytes:  m.Bytes,
			Wanted: m.Wanted,
		}
	}
	largest := make([]StatsLargest, len(st.Library.Largest))
	for i, l := range st.Library.Largest {
		largest[i] = StatsLargest{
			Title:   l.Title,
			Href:    l.Href,
			Kind:    StatsKind(l.Kind),
			Bytes:   l.Bytes,
			Quality: l.Quality,
		}
	}
	daily := make([]StatsDay, len(st.Pipeline.Daily))
	for i, d := range st.Pipeline.Daily {
		daily[i] = StatsDay{
			Day:      d.Day,
			Grabbed:  d.Grabbed,
			Imported: d.Imported,
			Failed:   d.Failed,
			Requests: d.Requests,
		}
	}
	failures := make([]StatsFailure, len(st.Pipeline.Failures))
	for i, f := range st.Pipeline.Failures {
		failures[i] = StatsFailure{
			Reason: f.Reason,
			Count:  f.Count,
			LastAt: f.LastAt,
		}
	}
	indexers := make([]StatsIndexer, len(st.Pipeline.Indexers))
	for i, ix := range st.Pipeline.Indexers {
		indexers[i] = StatsIndexer{
			Name:     ix.Name,
			Grabs:    ix.Grabs,
			Imported: ix.Imported,
			Failed:   ix.Failed,
		}
	}
	people := make([]StatsPerson, len(st.Users.People))
	for i, p := range st.Users.People {
		people[i] = StatsPerson{
			Id:         p.ID,
			Name:       p.Name,
			Email:      p.Email,
			Role:       StatsRole(p.Role),
			Requests:   p.Requests,
			Approved:   p.Approved,
			Denied:     p.Denied,
			Available:  p.Available,
			LastSeenAt: p.LastSeenAt,
		}
	}
	var out Stats
	out.Range = rng
	out.GeneratedAt = now
	out.Library.Media = media
	out.Library.Volumes = statsVolumes()
	out.Library.Resolution = statsSlices(st.Library.Resolution)
	out.Library.VideoCodec = statsSlices(st.Library.VideoCodec)
	out.Library.Hdr = statsSlices(st.Library.HDR)
	out.Library.Audio = statsSlices(st.Library.Audio)
	out.Library.Unprobed = st.Library.Unprobed
	out.Library.Largest = largest
	out.Pipeline.Daily = daily
	out.Pipeline.Funnel.Searched = st.Pipeline.Searched
	out.Pipeline.Funnel.Grabbed = st.Pipeline.Grabbed
	out.Pipeline.Funnel.Downloaded = st.Pipeline.Downloaded
	out.Pipeline.Funnel.Imported = st.Pipeline.Imported
	out.Pipeline.Held = st.Pipeline.Held
	out.Pipeline.GrabToImportP50S = st.Pipeline.GrabToImportP50
	out.Pipeline.GrabToImportP90S = st.Pipeline.GrabToImportP90
	out.Pipeline.Failures = failures
	out.Pipeline.Indexers = indexers
	t := st.Pipeline.Transcode
	out.Pipeline.Transcode.Succeeded = t.Succeeded
	out.Pipeline.Transcode.Failed = t.Failed
	out.Pipeline.Transcode.Rejected = t.Rejected
	out.Pipeline.Transcode.SavedBytes = t.SavedBytes
	out.Pipeline.Transcode.BusyS = t.BusySecs
	out.Pipeline.Drift.Detected = st.Pipeline.DriftDetected
	out.Pipeline.Drift.Confirmed = st.Pipeline.DriftConfirmed
	u := st.Users
	out.Users.Total = u.Total
	out.Users.Active = u.Active
	out.Users.Requests.Total = u.RequestsTotal
	out.Users.Requests.Pending = u.Pending
	out.Users.Requests.Approved = u.Approved
	out.Users.Requests.Denied = u.Denied
	out.Users.Requests.Available = u.RequestsAvailable
	out.Users.DecideP50S = u.DecideP50
	out.Users.AvailableP50S = u.AvailableP50
	out.Users.People = people
	out.Previous = StatsTotals{
		Grabbed: st.Previous.Grabbed, Imported: st.Previous.Imported,
		Failed: st.Previous.Failed, Requests: st.Previous.Requests,
	}
	return out
}
