package db

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/book"
	"github.com/datahearth/streamline/ent/downloadrecord"
	"github.com/datahearth/streamline/ent/episode"
	"github.com/datahearth/streamline/ent/mediaevent"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/ent/movie"
	"github.com/datahearth/streamline/ent/predicate"
	"github.com/datahearth/streamline/ent/request"
	"github.com/datahearth/streamline/ent/session"
	"github.com/datahearth/streamline/ent/transcodejob"
	"github.com/datahearth/streamline/internal/utils/numeric"
)

// StatsSlice is one bucket of a library breakdown.
type StatsSlice struct {
	Key   string `sql:"k"`
	Count uint32 `sql:"n"`
	Bytes int64  `sql:"bytes"`
}

// StatsMedia is one medium of the library. Wanted is in the medium's own unit:
// movies, episodes, albums, books.
type StatsMedia struct {
	Kind   string
	Titles uint32
	Files  uint32
	Bytes  int64
	Wanted uint32
}

// StatsLargest is one of the largest files in the library.
type StatsLargest struct {
	Title   string
	Href    string
	Kind    string
	Bytes   int64
	Quality string
}

// StatsLibrary is what the library holds now, whatever the range.
type StatsLibrary struct {
	Media      []StatsMedia
	Resolution []StatsSlice
	VideoCodec []StatsSlice
	HDR        []StatsSlice
	Audio      []StatsSlice
	Unprobed   uint32
	Largest    []StatsLargest
}

// StatsDay is one day of the pipeline; StatsTotals sums a stretch of them.
type StatsDay struct {
	Day      string
	Grabbed  uint32
	Imported uint32
	Failed   uint32
	Requests uint32
}

// StatsTotals is the same counters over a stretch of days.
type StatsTotals struct {
	Grabbed  uint32
	Imported uint32
	Failed   uint32
	Requests uint32
}

// StatsFailure is one failure code with its count and last occurrence.
type StatsFailure struct {
	Reason string
	Count  uint32
	LastAt time.Time
}

// StatsIndexer is one indexer's grabs and what became of them.
type StatsIndexer struct {
	Name     string
	Grabs    uint32
	Imported uint32
	Failed   uint32
}

// StatsTranscode sums the transcode jobs that finished in the range.
type StatsTranscode struct {
	Succeeded  uint32
	Failed     uint32
	Rejected   uint32
	SavedBytes int64
	BusySecs   int64
}

// StatsPipeline is how the library filled over the range.
type StatsPipeline struct {
	Daily                                   []StatsDay
	Searched, Grabbed, Downloaded, Imported uint32
	Held                                    uint32
	GrabToImportP50, GrabToImportP90        int64
	Failures                                []StatsFailure
	Indexers                                []StatsIndexer
	Transcode                               StatsTranscode
	DriftDetected, DriftConfirmed           uint32
}

// StatsPerson is one account with its requests in the range.
type StatsPerson struct {
	ID         uint32
	Name       string
	Email      string
	Role       string
	Requests   uint32
	Approved   uint32
	Denied     uint32
	Available  uint32
	LastSeenAt *time.Time
}

// StatsUsers is who is here and what they asked for.
type StatsUsers struct {
	Total, Active                                               uint32
	RequestsTotal, Pending, Approved, Denied, RequestsAvailable uint32
	DecideP50, AvailableP50                                     int64
	People                                                      []StatsPerson
}

// Stats is the admin stats page's report.
type Stats struct {
	Library  StatsLibrary
	Pipeline StatsPipeline
	Users    StatsUsers
	Previous StatsTotals
}

const (
	statsResolutionKey = `CASE
		WHEN width >= 3200 OR height >= 1800 THEN '2160p'
		WHEN width >= 1800 OR height >= 900 THEN '1080p'
		WHEN width >= 1200 OR height >= 650 THEN '720p'
		WHEN width > 0 THEN 'sd'
		ELSE 'unknown' END`
	statsVideoCodecKey = `CASE COALESCE(lower(video_codec), '')
		WHEN '' THEN 'unknown'
		WHEN 'hevc' THEN 'hevc'
		WHEN 'h264' THEN 'h264'
		WHEN 'av1' THEN 'av1'
		ELSE 'other' END`
	statsAudioKey = `CASE COALESCE(lower(audio_codec), '')
		WHEN '' THEN 'unknown'
		WHEN 'truehd' THEN 'truehd'
		WHEN 'dts' THEN 'dts'
		WHEN 'eac3' THEN 'eac3'
		WHEN 'ac3' THEN 'ac3'
		WHEN 'aac' THEN 'aac'
		WHEN 'flac' THEN 'flac'
		ELSE 'other' END`
	// ponytail: read off the release name, since the probe's HDR flag is not
	// stored. A column would be exact, at the price of re-probing the library
	// (a new probe column nulls probed_at).
	statsHDRKey = `CASE
		WHEN lower(path) LIKE '%dovi%' OR lower(path) LIKE '%dolby%vision%'
			OR lower(path) LIKE '%.dv.%' OR lower(path) LIKE '% dv %' THEN 'dv'
		WHEN lower(path) LIKE '%hdr%' THEN 'hdr10'
		ELSE 'sdr' END`
)

func videoFile() predicate.MediaFile {
	return mediafile.And(
		mediafile.MissingSinceIsNil(),
		mediafile.Or(mediafile.HasMovie(), mediafile.HasEpisode()),
	)
}

func (db *DB) statsSlices(ctx context.Context, key string) ([]StatsSlice, error) {
	out := []StatsSlice{}
	err := db.client.MediaFile.Query().
		Where(videoFile(), mediafile.ProbedAtNotNil()).
		Modify(func(s *entsql.Selector) {
			s.Select(
				entsql.As("("+key+")", "k"),
				entsql.As("COUNT(*)", "n"),
				entsql.As("COALESCE(SUM(size), 0)", "bytes"),
			).GroupBy("k").OrderExpr(entsql.Expr("n DESC, bytes DESC"))
		}).
		Scan(ctx, &out)
	if err != nil {
		return nil, fmt.Errorf("stats breakdown: %w", err)
	}
	return out, nil
}

func (db *DB) statsFiles(
	ctx context.Context,
	owner predicate.MediaFile,
) (uint32, int64, error) {
	var rows []struct {
		N     uint32 `sql:"n"`
		Bytes int64  `sql:"bytes"`
	}
	err := db.client.MediaFile.Query().
		Where(mediafile.MissingSinceIsNil(), owner).
		Modify(func(s *entsql.Selector) {
			s.Select(
				entsql.As("COUNT(*)", "n"),
				entsql.As("COALESCE(SUM(size), 0)", "bytes"),
			)
		}).
		Scan(ctx, &rows)
	if err != nil {
		return 0, 0, fmt.Errorf("stats files: %w", err)
	}
	if len(rows) == 0 {
		return 0, 0, nil
	}
	return rows[0].N, rows[0].Bytes, nil
}

func (db *DB) statsMedia(ctx context.Context, now time.Time) ([]StatsMedia, error) {
	type counter func() (int, error)
	media := []struct {
		kind           string
		owner          predicate.MediaFile
		titles, wanted counter
	}{
		{
			"movie", mediafile.HasMovie(),
			func() (int, error) { return db.client.Movie.Query().Count(ctx) },
			func() (int, error) {
				return db.client.Movie.Query().
					Where(movie.StatusEQ(movie.StatusWanted), movie.MonitoredEQ(true)).
					Count(ctx)
			},
		},
		{
			"series", mediafile.HasEpisode(),
			func() (int, error) { return db.client.TVShow.Query().Count(ctx) },
			func() (int, error) {
				return db.client.Episode.Query().
					Where(missingEpisode(now), episode.StatusEQ(episode.StatusWanted)).
					Count(ctx)
			},
		},
		{
			"music",
			mediafile.HasTrack(),
			func() (int, error) { return db.client.Artist.Query().Count(ctx) },
			func() (int, error) { return db.client.Album.Query().Where(wantedAlbum(now)).Count(ctx) },
		},
		{
			"books", mediafile.HasBook(),
			func() (int, error) { return db.client.Book.Query().Count(ctx) },
			func() (int, error) {
				return db.client.Book.Query().
					Where(searchable(now), book.Or(
						book.And(
							book.EbookMonitored(true),
							book.EbookStatusEQ(book.EbookStatusWanted),
						),
						book.And(
							book.AudiobookMonitored(true),
							book.AudiobookStatusEQ(book.AudiobookStatusWanted),
						),
					)).Count(ctx)
			},
		},
	}
	out := make([]StatsMedia, 0, len(media))
	for _, m := range media {
		titles, err := m.titles()
		if err != nil {
			return nil, fmt.Errorf("stats titles: %w", err)
		}
		wanted, err := m.wanted()
		if err != nil {
			return nil, fmt.Errorf("stats wanted: %w", err)
		}
		files, size, err := db.statsFiles(ctx, m.owner)
		if err != nil {
			return nil, err
		}
		out = append(out, StatsMedia{
			Kind:   m.kind,
			Titles: numeric.SaturateU32(titles),
			Files:  files,
			Bytes:  size,
			Wanted: numeric.SaturateU32(wanted),
		})
	}
	return out, nil
}

func (db *DB) statsLargest(ctx context.Context) ([]StatsLargest, error) {
	rows, err := db.client.MediaFile.Query().
		Where(mediafile.MissingSinceIsNil()).
		Order(mediafile.BySize(entsql.OrderDesc()), mediafile.ByID()).
		Limit(5).
		WithMovie().
		WithEpisode(func(q *ent.EpisodeQuery) {
			q.WithSeason(func(q *ent.SeasonQuery) { q.WithTvShow() })
		}).
		WithTrack(func(q *ent.TrackQuery) {
			q.WithAlbum(func(q *ent.AlbumQuery) { q.WithArtist() })
		}).
		WithBook().
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("stats largest files: %w", err)
	}
	out := make([]StatsLargest, 0, len(rows))
	for _, f := range rows {
		l := StatsLargest{Bytes: f.Size, Quality: f.Quality}
		switch {
		case f.Edges.Movie != nil:
			l.Kind, l.Title = "movie", f.Edges.Movie.Title
			l.Href = fmt.Sprintf("/movies/%d", f.Edges.Movie.ID)
		case f.Edges.Episode != nil && f.Edges.Episode.Edges.Season != nil &&
			f.Edges.Episode.Edges.Season.Edges.TvShow != nil:
			ep, season := f.Edges.Episode, f.Edges.Episode.Edges.Season
			l.Kind = "series"
			l.Title = fmt.Sprintf(
				"%s · S%02dE%02d",
				season.Edges.TvShow.Title,
				season.Number,
				ep.Number,
			)
			l.Href = fmt.Sprintf("/series/%d", season.Edges.TvShow.ID)
		case f.Edges.Track != nil && f.Edges.Track.Edges.Album != nil &&
			f.Edges.Track.Edges.Album.Edges.Artist != nil:
			al := f.Edges.Track.Edges.Album
			l.Kind, l.Title = "music", al.Title
			l.Href = fmt.Sprintf("/music/%d", al.Edges.Artist.ID)
		case f.Edges.Book != nil:
			l.Kind, l.Title = "books", f.Edges.Book.Title
			l.Href = fmt.Sprintf("/books/%d", f.Edges.Book.ID)
		default:
			continue
		}
		out = append(out, l)
	}
	return out, nil
}

func (db *DB) statsLibrary(
	ctx context.Context,
	now time.Time,
) (StatsLibrary, error) {
	var (
		lib StatsLibrary
		err error
	)
	if lib.Media, err = db.statsMedia(ctx, now); err != nil {
		return lib, err
	}
	for _, b := range []struct {
		dst *[]StatsSlice
		key string
	}{
		{&lib.Resolution, statsResolutionKey},
		{&lib.VideoCodec, statsVideoCodecKey},
		{&lib.HDR, statsHDRKey},
		{&lib.Audio, statsAudioKey},
	} {
		if *b.dst, err = db.statsSlices(ctx, b.key); err != nil {
			return lib, err
		}
	}
	unprobed, err := db.client.MediaFile.Query().
		Where(videoFile(), mediafile.ProbedAtIsNil()).Count(ctx)
	if err != nil {
		return lib, fmt.Errorf("stats unprobed files: %w", err)
	}
	lib.Unprobed = numeric.SaturateU32(unprobed)
	lib.Largest, err = db.statsLargest(ctx)
	return lib, err
}

// percentileSecs is the nearest-rank percentile of the waits, which it sorts.
func percentileSecs(waits []int64, p int) int64 {
	if len(waits) == 0 {
		return 0
	}
	slices.Sort(waits)
	return waits[(len(waits)-1)*p/100]
}

// failureCode files a download record's free-text failure_reason under the
// codes the stats page names. ponytail: substring match on messages we compose
// ourselves; a failure_code column if a code ever needs to be exact. Order
// matters: "no space left" must not read as a no_match.
func failureCode(reason string) string {
	r := strings.ToLower(reason)
	for _, c := range []struct {
		code  string
		words []string
	}{
		{"no_space", []string{"no space", "disk full", "enospc", "not enough space"}},
		{"stalled", []string{"stall"}},
		{"verification", []string{"verif", "checksum", "probe", "corrupt"}},
		{"unpack", []string{"unpack", "extract", "archive"}},
		{"tracker", []string{"tracker", "announce", "refused the torrent"}},
		{"no_match", []string{"no files", "no match", "matched", "unmatched", "no video", "no audio", "not found", "no such file"}},
	} {
		for _, w := range c.words {
			if strings.Contains(r, w) {
				return c.code
			}
		}
	}
	return "other"
}

func (db *DB) statsEvents(
	ctx context.Context,
	daily []StatsDay,
	since, prevSince time.Time,
	p *StatsPipeline,
	prev *StatsTotals,
) error {
	var rows []struct {
		Type mediaevent.Type `sql:"type"`
		At   time.Time       `sql:"create_time"`
	}
	err := db.client.MediaEvent.Query().
		Where(
			mediaevent.CreateTimeGTE(prevSince),
			mediaevent.TypeIn(
				mediaevent.TypeSearched,
				mediaevent.TypeGrabbed,
				mediaevent.TypeDownloadCompleted,
				mediaevent.TypeImported,
				mediaevent.TypeDownloadFailed,
				mediaevent.TypeImportFailed,
				mediaevent.TypeDriftDetected,
				mediaevent.TypeDriftConfirmed,
			),
		).
		Select(mediaevent.FieldType, mediaevent.FieldCreateTime).
		Scan(ctx, &rows)
	if err != nil {
		return fmt.Errorf("stats events: %w", err)
	}
	byDay := dayIndex(daily)
	for _, e := range rows {
		if e.At.Before(since) {
			switch e.Type {
			case mediaevent.TypeGrabbed:
				prev.Grabbed++
			case mediaevent.TypeImported:
				prev.Imported++
			case mediaevent.TypeDownloadFailed, mediaevent.TypeImportFailed:
				prev.Failed++
			default:
			}
			continue
		}
		d := byDay[e.At.UTC().Format(time.DateOnly)]
		switch e.Type {
		case mediaevent.TypeSearched:
			p.Searched++
		case mediaevent.TypeGrabbed:
			p.Grabbed++
			if d != nil {
				d.Grabbed++
			}
		case mediaevent.TypeDownloadCompleted:
			p.Downloaded++
		case mediaevent.TypeImported:
			p.Imported++
			if d != nil {
				d.Imported++
			}
		case mediaevent.TypeDownloadFailed, mediaevent.TypeImportFailed:
			if d != nil {
				d.Failed++
			}
		case mediaevent.TypeDriftDetected:
			p.DriftDetected++
		case mediaevent.TypeDriftConfirmed:
			p.DriftConfirmed++
		default:
		}
	}
	return nil
}

func dayIndex(daily []StatsDay) map[string]*StatsDay {
	out := make(map[string]*StatsDay, len(daily))
	for i := range daily {
		out[daily[i].Day] = &daily[i]
	}
	return out
}

func (db *DB) statsRecords(
	ctx context.Context,
	since time.Time,
	p *StatsPipeline,
) error {
	var rows []struct {
		Status     downloadrecord.Status `sql:"status"`
		Indexer    *string               `sql:"indexer_name"`
		Reason     *string               `sql:"failure_reason"`
		Created    time.Time             `sql:"create_time"`
		Updated    time.Time             `sql:"update_time"`
		ImportedAt *time.Time            `sql:"imported_at"`
	}
	err := db.client.DownloadRecord.Query().
		Where(
			downloadrecord.UploadedEQ(false),
			downloadrecord.Or(
				downloadrecord.CreateTimeGTE(since),
				downloadrecord.UpdateTimeGTE(since),
			),
		).
		Select(
			downloadrecord.FieldStatus, downloadrecord.FieldIndexerName,
			downloadrecord.FieldFailureReason, downloadrecord.FieldCreateTime,
			downloadrecord.FieldUpdateTime, downloadrecord.FieldImportedAt,
		).
		Scan(ctx, &rows)
	if err != nil {
		return fmt.Errorf("stats download records: %w", err)
	}
	var waits []int64
	failures := map[string]*StatsFailure{}
	indexers := map[string]*StatsIndexer{}
	for _, r := range rows {
		if r.ImportedAt != nil && !r.ImportedAt.Before(since) {
			waits = append(waits, int64(r.ImportedAt.Sub(r.Created).Seconds()))
		}
		if r.Status == downloadrecord.StatusFailed && !r.Updated.Before(since) {
			code := failureCode(deref(r.Reason))
			f := failures[code]
			if f == nil {
				f = &StatsFailure{Reason: code}
				failures[code] = f
			}
			f.Count++
			if r.Updated.After(f.LastAt) {
				f.LastAt = r.Updated
			}
		}
		if name := deref(r.Indexer); name != "" && !r.Created.Before(since) {
			ix := indexers[name]
			if ix == nil {
				ix = &StatsIndexer{Name: name}
				indexers[name] = ix
			}
			ix.Grabs++
			switch r.Status {
			case downloadrecord.StatusCompleted:
				ix.Imported++
			case downloadrecord.StatusFailed:
				ix.Failed++
			default:
			}
		}
	}
	p.GrabToImportP50 = percentileSecs(waits, 50)
	p.GrabToImportP90 = percentileSecs(waits, 90)
	p.Failures = make([]StatsFailure, 0, len(failures))
	for _, f := range failures {
		p.Failures = append(p.Failures, *f)
	}
	slices.SortFunc(p.Failures, func(a, b StatsFailure) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Reason, b.Reason))
	})
	p.Indexers = make([]StatsIndexer, 0, len(indexers))
	for _, ix := range indexers {
		p.Indexers = append(p.Indexers, *ix)
	}
	slices.SortFunc(p.Indexers, func(a, b StatsIndexer) int {
		return cmp.Or(cmp.Compare(b.Grabs, a.Grabs), cmp.Compare(a.Name, b.Name))
	})
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (db *DB) statsTranscode(
	ctx context.Context,
	since time.Time,
) (StatsTranscode, error) {
	var out StatsTranscode
	rows, err := db.client.TranscodeJob.Query().
		Where(transcodejob.FinishedAtGTE(since)).
		Select(
			transcodejob.FieldStatus, transcodejob.FieldSizeBefore, transcodejob.FieldSizeAfter,
			transcodejob.FieldStartedAt, transcodejob.FieldFinishedAt,
		).
		All(ctx)
	if err != nil {
		return out, fmt.Errorf("stats transcode jobs: %w", err)
	}
	for _, j := range rows {
		switch j.Status {
		case transcodejob.StatusSucceeded:
			out.Succeeded++
			out.SavedBytes += j.SizeBefore - j.SizeAfter
		case transcodejob.StatusFailed:
			out.Failed++
		case transcodejob.StatusRejected:
			out.Rejected++
		default:
			continue
		}
		if j.StartedAt != nil && j.FinishedAt != nil {
			out.BusySecs += int64(j.FinishedAt.Sub(*j.StartedAt).Seconds())
		}
	}
	return out, nil
}

// statsRequests bumps the daily and previous tallies and returns the people's
// request tallies. A decision and an availability both stamp update_time, and no
// other write touches a request afterwards in the common path, which is what the
// two medians read. ponytail: an approved request that later went available loses
// its decision time; decided_at/available_at columns if the medians need it.
func (db *DB) statsRequests(
	ctx context.Context,
	daily []StatsDay,
	since, prevSince time.Time,
	u *StatsUsers,
	prev *StatsTotals,
) (map[uint32]*StatsPerson, error) {
	var rows []struct {
		Status  request.Status `sql:"status"`
		User    uint32         `sql:"user_requests"`
		Created time.Time      `sql:"create_time"`
		Updated time.Time      `sql:"update_time"`
	}
	err := db.client.Request.Query().
		Where(request.Or(request.CreateTimeGTE(prevSince), request.StatusEQ(request.StatusPending))).
		Modify(func(s *entsql.Selector) {
			s.Select(
				s.C(request.FieldStatus),
				s.C(request.RequesterColumn),
				s.C(request.FieldCreateTime),
				s.C(request.FieldUpdateTime),
			)
		}).
		Scan(ctx, &rows)
	if err != nil {
		return nil, fmt.Errorf("stats requests: %w", err)
	}
	byDay := dayIndex(daily)
	people := map[uint32]*StatsPerson{}
	var decide, available []int64
	for _, r := range rows {
		if r.Status == request.StatusPending {
			u.Pending++
		}
		if r.Created.Before(since) {
			if !r.Created.Before(prevSince) {
				prev.Requests++
			}
			continue
		}
		if d := byDay[r.Created.UTC().Format(time.DateOnly)]; d != nil {
			d.Requests++
		}
		p := people[r.User]
		if p == nil {
			p = &StatsPerson{}
			people[r.User] = p
		}
		u.RequestsTotal++
		p.Requests++
		wait := int64(r.Updated.Sub(r.Created).Seconds())
		switch r.Status {
		case request.StatusApproved:
			u.Approved++
			p.Approved++
			decide = append(decide, wait)
		case request.StatusDenied:
			u.Denied++
			p.Denied++
			decide = append(decide, wait)
		case request.StatusAvailable:
			u.Approved++
			u.RequestsAvailable++
			p.Approved++
			p.Available++
			available = append(available, wait)
		default:
		}
	}
	u.DecideP50 = percentileSecs(decide, 50)
	u.AvailableP50 = percentileSecs(available, 50)
	return people, nil
}

func (db *DB) statsUsers(
	ctx context.Context,
	daily []StatsDay,
	since, prevSince time.Time,
	prev *StatsTotals,
) (StatsUsers, error) {
	var u StatsUsers
	tally, err := db.statsRequests(ctx, daily, since, prevSince, &u, prev)
	if err != nil {
		return u, err
	}
	var sessions []struct {
		User     uint32     `sql:"user_sessions"`
		Created  time.Time  `sql:"create_time"`
		LastSeen *time.Time `sql:"last_seen_at"`
	}
	err = db.client.Session.Query().
		Modify(func(s *entsql.Selector) {
			s.Select(
				s.C(session.UserColumn),
				s.C(session.FieldCreateTime),
				s.C(session.FieldLastSeenAt),
			)
		}).
		Scan(ctx, &sessions)
	if err != nil {
		return u, fmt.Errorf("stats sessions: %w", err)
	}
	seen := map[uint32]time.Time{}
	for _, s := range sessions {
		at := s.Created
		if s.LastSeen != nil && s.LastSeen.After(at) {
			at = *s.LastSeen
		}
		if at.After(seen[s.User]) {
			seen[s.User] = at
		}
	}
	accounts, err := db.client.User.Query().All(ctx)
	if err != nil {
		return u, fmt.Errorf("stats users: %w", err)
	}
	u.Total = numeric.SaturateU32(len(accounts))
	u.People = make([]StatsPerson, 0, len(accounts))
	for _, a := range accounts {
		p := StatsPerson{}
		if t := tally[a.ID]; t != nil {
			p = *t
		}
		p.ID, p.Email, p.Role = a.ID, a.Email, string(a.Role)
		p.Name = cmp.Or(a.DisplayName, a.Email)
		if at, ok := seen[a.ID]; ok {
			p.LastSeenAt = &at
			if !at.Before(since) {
				u.Active++
			}
		}
		u.People = append(u.People, p)
	}
	slices.SortFunc(u.People, func(a, b StatsPerson) int {
		return cmp.Or(
			cmp.Compare(b.Requests, a.Requests),
			strings.Compare(a.Name, b.Name),
		)
	})
	return u, nil
}

// GetStats builds the admin stats report over the last days days, today
// included, and compares it with the same stretch before. It reads what the
// tables already hold and is meant to be polled slowly.
func (db *DB) GetStats(ctx context.Context, days int, now time.Time) (Stats, error) {
	var out Stats
	midnight := now.UTC().Truncate(24 * time.Hour)
	since := midnight.AddDate(0, 0, 1-days)
	prevSince := since.AddDate(0, 0, -days)

	daily := make([]StatsDay, days)
	for i := range daily {
		daily[i].Day = since.AddDate(0, 0, i).Format(time.DateOnly)
	}
	var err error
	if out.Library, err = db.statsLibrary(ctx, now); err != nil {
		return out, err
	}
	if err = db.statsEvents(
		ctx,
		daily,
		since,
		prevSince,
		&out.Pipeline,
		&out.Previous,
	); err != nil {
		return out, err
	}
	if err = db.statsRecords(ctx, since, &out.Pipeline); err != nil {
		return out, err
	}
	if out.Pipeline.Transcode, err = db.statsTranscode(ctx, since); err != nil {
		return out, err
	}
	held, err := db.client.DownloadRecord.Query().
		Where(downloadrecord.StatusEQ(downloadrecord.StatusHeld)).Count(ctx)
	if err != nil {
		return out, fmt.Errorf("stats held records: %w", err)
	}
	out.Pipeline.Held = numeric.SaturateU32(held)
	if out.Users, err = db.statsUsers(
		ctx,
		daily,
		since,
		prevSince,
		&out.Previous,
	); err != nil {
		return out, err
	}
	out.Pipeline.Daily = daily
	return out, nil
}
