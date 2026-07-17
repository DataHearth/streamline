package restapi

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/download"
	"github.com/datahearth/streamline/internal/indexer"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/media/tvshow"
	"github.com/datahearth/streamline/internal/mediaserver"
	"github.com/datahearth/streamline/internal/metadata"

	openapi_types "github.com/oapi-codegen/runtime/types"
)

// --- response-body helpers -------------------------------------------------
//
// Strict-server generates a per-operation response type (e.g.
// UpdateMe401JSONResponse) that embeds the shared payload struct (e.g.
// UnauthorizedJSONResponse). Handlers assemble the wrapper at the call site;
// these helpers cover the shared inner payload so the message lives in one
// place per status code.

func unauthorizedResp(msg string) UnauthorizedJSONResponse {
	return UnauthorizedJSONResponse{Message: msg}
}

func notFoundResp(msg string) NotFoundJSONResponse {
	return NotFoundJSONResponse{Message: msg}
}

func unprocessableResp(msg string) UnprocessableEntityJSONResponse {
	return UnprocessableEntityJSONResponse{Message: msg}
}

func forbiddenResp(msg string) ForbiddenJSONResponse {
	return ForbiddenJSONResponse{Message: msg}
}

// configLocked reports whether err means the configuration can't be mutated
// through the API: the instance runs read-only, or the targeted secret is
// file-managed. Both map to 403.
func configLocked(err error) bool {
	return errors.Is(err, config.ErrReadOnly) ||
		errors.Is(err, config.ErrSecretFileManaged)
}

func conflictResp(code, msg string) ConflictJSONResponse {
	return ConflictJSONResponse{Code: &code, Message: msg}
}

func movieToAPI(m *ent.Movie) Movie {
	mov := Movie{
		Id:            m.ID,
		Title:         m.Title,
		OriginalTitle: m.OriginalTitle,
		Year:          m.Year,
		Status:        MovieStatus(m.Status),
		Monitored:     m.Monitored,
		TmdbId:        m.TmdbID,
	}
	if m.Overview != "" {
		mov.Overview = &m.Overview
	}
	if m.Runtime != 0 {
		rt := m.Runtime
		mov.Runtime = &rt
	}
	return mov
}

func castToAPI(cast []metadata.CastMember) []CastMember {
	out := make([]CastMember, 0, len(cast))
	for _, c := range cast {
		m := CastMember{Name: c.Name}
		if c.TMDBID != 0 {
			id := c.TMDBID
			m.TmdbId = &id
		}
		if c.Character != "" {
			m.Character = &c.Character
		}
		if c.ProfileURL != "" {
			m.ProfileUrl = &c.ProfileURL
		}
		// Person page link: TVDB cast carries it directly; TMDB cast derives
		// it from the person id.
		personURL := c.PersonURL
		if personURL == "" && c.TMDBID != 0 {
			personURL = fmt.Sprintf("https://www.themoviedb.org/person/%d", c.TMDBID)
		}
		if personURL != "" {
			m.PersonUrl = &personURL
		}
		out = append(out, m)
	}
	return out
}

func toAPIUser(u *ent.User) User {
	var dn *string
	if u.DisplayName != "" {
		d := u.DisplayName
		dn = &d
	}
	email := openapi_types.Email(u.Email)
	return User{
		Id:          u.ID,
		Email:       email,
		Role:        UserRole(u.Role),
		AuthMethod:  UserAuthMethod(u.AuthMethod),
		DisplayName: dn,
		CreatedAt:   u.CreateTime,
	}
}

func toAPIInvite(i *ent.Invite) Invite {
	var email *openapi_types.Email
	if i.Email != "" {
		e := openapi_types.Email(i.Email)
		email = &e
	}
	return Invite{
		Id:        i.ID,
		Email:     email,
		Role:      InviteRole(i.Role),
		ExpiresAt: i.ExpiresAt,
		UsedAt:    i.UsedAt,
		CreatedAt: i.CreateTime,
	}
}

func downloadClientToAPI(e config.DownloadClientEntry) DownloadClient {
	useSSL := e.UseSSL
	prio := e.Priority
	d := DownloadClient{
		Name:        e.Name,
		ClientType:  DownloadClientClientType(e.ClientType),
		Host:        e.Host,
		Port:        e.Port,
		AuthMethod:  DownloadClientAuthMethod(e.AuthMethod),
		Enabled:     e.Enabled,
		ApiKeySet:   e.APIKey != "" || e.APIKeyFile != "",
		PasswordSet: e.Password != "" || e.PasswordFile != "",
		UseSsl:      &useSSL,
		Priority:    &prio,
	}
	if e.Username != "" {
		username := e.Username
		d.Username = &username
	}
	// builtin entries carry no auth; the read schema still requires a valid
	// enum value, so default it.
	if e.AuthMethod == "" {
		d.AuthMethod = DownloadClientAuthMethod("password")
	}
	if e.DownloadDir != "" {
		v := e.DownloadDir
		d.DownloadDir = &v
	}
	if e.ListenPort != 0 {
		v := e.ListenPort
		d.ListenPort = &v
	}
	if e.MaxUploadKbps != 0 {
		v := e.MaxUploadKbps
		d.MaxUploadKbps = &v
	}
	if e.MaxDownloadKbps != 0 {
		v := e.MaxDownloadKbps
		d.MaxDownloadKbps = &v
	}
	if e.SeedRatio != 0 {
		v := e.SeedRatio
		d.SeedRatio = &v
	}
	if e.SeedTime != "" {
		v := e.SeedTime
		d.SeedTime = &v
	}
	if e.DisableDHT {
		v := true
		d.DisableDht = &v
	}
	if e.BindInterface != "" {
		v := e.BindInterface
		d.BindInterface = &v
	}
	return d
}

func toAPIApiKey(k *ent.ApiKey) ApiKey {
	return ApiKey{
		Id:         k.ID,
		Name:       k.Name,
		CreatedAt:  k.CreateTime,
		LastUsedAt: k.LastUsedAt,
	}
}

func toAPISession(sess *ent.Session, currentJTI string) Session {
	out := Session{
		Id:         sess.ID,
		CreatedAt:  sess.CreateTime,
		ExpiresAt:  sess.ExpiresAt,
		IsCurrent:  sess.Jti == currentJTI,
		LastSeenAt: sess.LastSeenAt,
	}
	if sess.IP != "" {
		ip := sess.IP
		out.Ip = &ip
	}
	if sess.UserAgent != "" {
		ua := sess.UserAgent
		out.UserAgent = &ua
	}
	return out
}

func playOnToAPI(r mediaserver.PlayOnResult) PlayOnLink {
	out := PlayOnLink{
		Name:       r.Name,
		ServerType: PlayOnLinkServerType(r.ServerType),
		Fallback:   r.Fallback,
		Status:     PlayOnLinkStatus(string(r.Status)),
	}
	if r.URL != "" {
		u := r.URL
		out.Url = &u
	}
	return out
}

func mediaFileToAPI(f *ent.MediaFile) MediaFile {
	parsed := library.Parse(filepath.Base(f.Path))
	out := MediaFile{
		Id:   f.ID,
		Path: f.Path,
		Size: f.Size,
	}
	if f.Quality != "" {
		q := f.Quality
		out.Quality = &q
	}
	if f.Format != "" {
		fmtStr := f.Format
		out.Format = &fmtStr
	}
	if f.ReleaseGroup != "" {
		rg := f.ReleaseGroup
		out.ReleaseGroup = &rg
	}
	if parsed.Source != "" {
		ps := parsed.Source
		out.ParsedSource = &ps
	}
	if parsed.Resolution != "" {
		pr := parsed.Resolution
		out.ParsedResolution = &pr
	}
	if parsed.Codec != "" {
		pc := parsed.Codec
		out.ParsedCodec = &pc
	}
	return out
}

func mediaServerToAPI(e config.MediaServerEntry) MediaServer {
	out := MediaServer{
		Name:       e.Name,
		ServerType: MediaServerServerType(e.ServerType),
		Host:       e.Host,
		Enabled:    e.Enabled,
		ApiKeySet:  e.APIKey != "" || e.APIKeyFile != "",
	}
	if e.LibrarySection != nil {
		out.LibrarySection = e.LibrarySection
	}
	return out
}

func indexerToAPI(e config.IndexerEntry) Indexer {
	useSSL := e.UseSSL
	prio := e.Priority
	i := Indexer{
		Name:      e.Name,
		Host:      e.Host,
		Port:      e.Port,
		Protocol:  IndexerProtocol(e.Protocol),
		Enabled:   e.Enabled,
		ApiKeySet: e.APIKey != "" || e.APIKeyFile != "",
		UseSsl:    &useSSL,
		Priority:  &prio,
	}
	if e.Path != "" {
		path := e.Path
		i.Path = &path
	}
	return i
}

func toAPIImportScan(s *ent.ImportScan) ImportScan {
	out := ImportScan{
		Id:                 s.ID,
		SourcePath:         s.SourcePath,
		Kind:               ImportScanKind(s.Kind),
		Mode:               ImportScanMode(s.Mode),
		Status:             ImportScanStatus(s.Status),
		TotalCount:         s.TotalCount,
		ProcessedCount:     s.ProcessedCount,
		CommitSuccessCount: s.CommitSuccessCount,
		CommitFailedCount:  s.CommitFailedCount,
		CreatedAt:          s.CreateTime,
	}
	ut := s.UpdateTime
	out.UpdatedAt = &ut
	if s.ImportMode != "" {
		im := ImportScanImportMode(s.ImportMode)
		out.ImportMode = &im
	}
	if s.FailureReason != "" {
		fr := s.FailureReason
		out.FailureReason = &fr
	}
	if s.ScannedAt != nil {
		out.ScannedAt = s.ScannedAt
	}
	if s.CommittedAt != nil {
		out.CommittedAt = s.CommittedAt
	}
	return out
}

func toActivityEvent(e *ent.MovieEvent) ActivityEvent {
	out := ActivityEvent{
		Id:        e.ID,
		Type:      ActivityEventType(e.Type),
		CreatedAt: e.CreateTime,
	}
	if len(e.Payload) > 0 {
		p := e.Payload
		out.Payload = &p
	}
	if e.Edges.Movie != nil {
		out.Movie = movieToAPI(e.Edges.Movie)
	}
	return out
}

func toUpcomingEpisode(e *ent.Episode) UpcomingEpisode {
	out := UpcomingEpisode{
		Episode:   e.Number,
		Monitored: &e.Monitored,
	}
	if !e.AirDate.IsZero() {
		out.AirDate = e.AirDate
	}
	if e.Title != "" {
		out.Title = &e.Title
	}
	if se := e.Edges.Season; se != nil {
		out.Season = se.Number
		if show := se.Edges.TvShow; show != nil {
			out.SeriesId = show.ID
			out.SeriesTitle = show.Title
		}
	}
	return out
}

func toUpcomingMovie(m *ent.Movie) UpcomingMovie {
	out := UpcomingMovie{
		Id:     m.ID,
		Title:  m.Title,
		Year:   m.Year,
		TmdbId: m.TmdbID,
	}
	if m.DigitalReleaseDate != nil {
		out.DigitalReleaseDate = *m.DigitalReleaseDate
	}
	return out
}

func toAPIImportScanFile(f *ent.ImportScanFile) ImportScanFile {
	out := ImportScanFile{
		Id:             f.ID,
		SourcePath:     f.SourcePath,
		Size:           f.Size,
		Classification: ImportScanFileClassification(f.Classification),
		Decision:       ImportScanFileDecision(f.Decision),
		Outcome:        ImportScanFileOutcome(f.Outcome),
	}
	if f.ParsedTitle != "" {
		pt := f.ParsedTitle
		out.ParsedTitle = &pt
	}
	if f.ParsedYear != nil {
		out.ParsedYear = f.ParsedYear
	}
	if f.ParsedQuality != "" {
		pq := f.ParsedQuality
		out.ParsedQuality = &pq
	}
	if f.ParsedReleaseGroup != "" {
		prg := f.ParsedReleaseGroup
		out.ParsedReleaseGroup = &prg
	}
	if len(f.Candidates) > 0 {
		cands := make([]ImportScanCandidate, 0, len(f.Candidates))
		for _, c := range f.Candidates {
			cands = append(cands, ImportScanCandidate{
				TmdbId: c.TMDBID,
				Title:  c.Title,
				Year:   c.Year,
			})
		}
		out.Candidates = &cands
	}
	if f.TmdbID != 0 {
		v := f.TmdbID
		out.TmdbId = &v
	}
	if f.ExistingMovieID != 0 {
		v := f.ExistingMovieID
		out.ExistingMovieId = &v
	}
	if f.DecisionTmdbID != 0 {
		v := f.DecisionTmdbID
		out.DecisionTmdbId = &v
	}
	if f.OutcomeMessage != "" {
		om := f.OutcomeMessage
		out.OutcomeMessage = &om
	}
	if f.CreatedMovieID != 0 {
		v := f.CreatedMovieID
		out.CreatedMovieId = &v
	}
	ct := f.CreateTime
	out.CreatedAt = &ct
	ut := f.UpdateTime
	out.UpdatedAt = &ut
	return out
}

func toAPIImportScanShow(sh *ent.ImportScanShow) ImportScanShow {
	out := ImportScanShow{
		Id:             sh.ID,
		FolderPath:     sh.FolderPath,
		Classification: ImportScanShowClassification(sh.Classification),
		FileCount:      sh.FileCount,
		Decision:       ImportScanShowDecision(sh.Decision),
		Outcome:        ImportScanShowOutcome(sh.Outcome),
	}
	if sh.ParsedTitle != "" {
		pt := sh.ParsedTitle
		out.ParsedTitle = &pt
	}
	out.ParsedYear = sh.ParsedYear
	out.TvdbId = sh.TvdbID
	out.ExistingTvshowId = sh.ExistingTvshowID
	out.DecisionTvdbId = sh.DecisionTvdbID
	out.CreatedTvshowId = sh.CreatedTvshowID
	if sh.OutcomeMessage != "" {
		om := sh.OutcomeMessage
		out.OutcomeMessage = &om
	}
	if len(sh.Candidates) > 0 {
		cands := make([]ImportScanShowCandidate, 0, len(sh.Candidates))
		for _, c := range sh.Candidates {
			cand := ImportScanShowCandidate{TvdbId: c.TVDBID, Title: c.Title}
			if c.Year != 0 {
				y := c.Year
				cand.Year = &y
			}
			cands = append(cands, cand)
		}
		out.Candidates = &cands
	}
	ct := sh.CreateTime
	out.CreatedAt = &ct
	ut := sh.UpdateTime
	out.UpdatedAt = &ut
	return out
}

func toQueueEntry(e download.QueueEntry) QueueEntry {
	out := QueueEntry{
		Id:        e.RecordID,
		Status:    QueueEntryStatus(e.Status),
		Title:     e.Title,
		Size:      e.Size,
		Progress:  e.Progress,
		CreatedAt: e.CreatedAt,
	}
	if e.Movie != nil {
		out.Movie = movieToAPI(e.Movie)
	}
	out.Episode = episodeRef(e.Episode)
	if e.DownloadSpeed != 0 {
		ds := e.DownloadSpeed
		out.DownloadSpeed = &ds
	}
	if e.ETA != 0 {
		eta := e.ETA
		out.Eta = &eta
	}
	if e.Quality != "" {
		out.Quality = &e.Quality
	}
	if e.ReleaseGroup != "" {
		out.ReleaseGroup = &e.ReleaseGroup
	}
	if e.Indexer != "" {
		out.Indexer = &e.Indexer
	}
	if e.DownloadClient != "" {
		out.DownloadClient = &e.DownloadClient
	}
	if e.FailureReason != "" {
		out.FailureReason = &e.FailureReason
	}
	return out
}

func toHistoryEntry(r *ent.DownloadRecord) HistoryEntry {
	out := HistoryEntry{
		Id:        r.ID,
		Status:    HistoryEntryStatus(r.Status),
		Title:     r.Title,
		Size:      r.Size,
		CreatedAt: r.CreateTime,
		UpdatedAt: r.UpdateTime,
	}
	if r.Edges.Movie != nil {
		out.Movie = movieToAPI(r.Edges.Movie)
	}
	out.Episode = episodeRef(r.Edges.Episode)
	if r.Quality != "" {
		out.Quality = &r.Quality
	}
	if r.ReleaseGroup != "" {
		out.ReleaseGroup = &r.ReleaseGroup
	}
	if r.IndexerName != "" {
		name := r.IndexerName
		out.Indexer = &name
	}
	if r.DownloadClientName != "" {
		name := r.DownloadClientName
		out.DownloadClient = &name
	}
	if r.FailureReason != "" {
		out.FailureReason = &r.FailureReason
	}
	if r.ImportedAt != nil {
		out.ImportedAt = r.ImportedAt
	}
	return out
}

// episodeRef builds the show + S/E context for a TV download record's queue or
// history row, or nil for a movie record. Expects season + show eager-loaded.
func episodeRef(ep *ent.Episode) *EpisodeRef {
	if ep == nil {
		return nil
	}
	ref := &EpisodeRef{Episode: ep.Number}
	if se := ep.Edges.Season; se != nil {
		ref.Season = se.Number
		if sh := se.Edges.TvShow; sh != nil {
			ref.ShowTitle = sh.Title
		}
	}
	return ref
}

// toPendingItem maps a pending DownloadRecord (with movie/episode edges
// eager-loaded) to its API view.
func toPendingItem(r *ent.DownloadRecord) PendingItem {
	item := PendingItem{
		Id:      r.ID,
		Title:   r.Title,
		Quality: r.Quality,
		Reason:  r.FailureReason,
	}
	switch {
	case r.Edges.Movie != nil:
		m := r.Edges.Movie
		item.HasFile = len(m.Edges.MediaFiles) > 0
		y := m.Year
		item.Media = &PendingMedia{
			Type:  PendingMediaTypeMovie,
			Id:    m.ID,
			Title: m.Title,
			Year:  &y,
		}
	case r.Edges.Episode != nil:
		ep := r.Edges.Episode
		item.HasFile = len(ep.Edges.MediaFiles) > 0
		epNum := ep.Number
		media := &PendingMedia{
			Type:    PendingMediaTypeEpisode,
			Id:      ep.ID,
			Episode: &epNum,
		}
		if se := ep.Edges.Season; se != nil {
			sNum := se.Number
			media.Season = &sNum
			if show := se.Edges.TvShow; show != nil {
				media.Title = show.Title
				y := show.Year
				media.Year = &y
			}
		}
		item.Media = media
	}
	return item
}

// tvShowToAPI maps an eager-loaded TV show (seasons → episodes → media files)
// to the API shape, rolling up per-season availability into show totals via
// tvshow.DeriveSeasonViews. Seasons/episodes are only present when the show
// was loaded with those edges (GET /series/{id}); list responses omit them.
func tvShowToAPI(s *ent.TVShow) TVShow {
	out := TVShow{
		Id:           s.ID,
		Title:        s.Title,
		Year:         s.Year,
		SeriesStatus: TVShowSeriesStatus(s.SeriesStatus),
		Type:         TVShowType(s.Type),
		Monitored:    s.Monitored,
		TvdbId:       s.TvdbID,
	}
	if s.OriginalTitle != "" {
		out.OriginalTitle = &s.OriginalTitle
	}
	if s.Overview != "" {
		out.Overview = &s.Overview
	}
	if s.Network != "" {
		out.Network = &s.Network
	}
	if s.Creator != "" {
		out.Creator = &s.Creator
	}
	if s.Runtime != 0 {
		rt := s.Runtime
		out.Runtime = &rt
	}
	if s.Rating > 0 {
		r := float32(s.Rating)
		out.Rating = &r
	}
	if len(s.Genres) > 0 {
		g := s.Genres
		out.Genres = &g
	}
	if s.QualityProfile != "" {
		out.QualityProfile = &s.QualityProfile
	}

	now := time.Now()
	views := tvshow.DeriveSeasonViews(s, now)
	var have, total, wanted uint32
	seasons := make([]Season, 0, len(s.Edges.Seasons))
	for i, se := range s.Edges.Seasons {
		v := views[i]
		have += uint32(v.Available)
		total += uint32(v.Total)
		wanted += uint32(v.Missing)
		seasons = append(seasons, seasonToAPI(se, v, now))
	}
	out.HaveEpisodes = &have
	out.TotalEpisodes = &total
	out.WantedEpisodes = &wanted
	if len(seasons) > 0 {
		out.Seasons = &seasons
	}
	return out
}

func seasonToAPI(se *ent.Season, v tvshow.SeasonView, now time.Time) Season {
	avail, miss, un, tot := v.Available, v.Missing, v.Unaired, v.Total
	out := Season{
		Id:        se.ID,
		Number:    se.Number,
		Monitored: se.Monitored,
		Available: &avail,
		Missing:   &miss,
		Unaired:   &un,
		Total:     &tot,
	}
	if se.Name != "" {
		out.Name = &se.Name
	}
	eps := make([]Episode, 0, len(se.Edges.Episodes))
	for _, e := range se.Edges.Episodes {
		eps = append(eps, episodeToAPI(e, now))
	}
	if len(eps) > 0 {
		out.Episodes = &eps
	}
	return out
}

// episodeToAPI maps an episode to the API shape. The "unaired" status is
// derived (the ent enum has no such value): an episode with no file whose
// air_date is in the future surfaces as unaired.
func episodeToAPI(e *ent.Episode, now time.Time) Episode {
	out := Episode{
		Id:        e.ID,
		Number:    e.Number,
		Status:    EpisodeStatus(e.Status),
		Monitored: e.Monitored,
	}
	if len(e.Edges.MediaFiles) == 0 &&
		!e.AirDate.IsZero() && e.AirDate.After(now) {
		out.Status = EpisodeStatusUnaired
	}
	if e.AbsoluteNumber > 0 {
		out.AbsoluteNumber = &e.AbsoluteNumber
	}
	if e.Title != "" {
		out.Title = &e.Title
	}
	if !e.AirDate.IsZero() {
		ad := e.AirDate
		out.AirDate = &ad
	}
	if len(e.Edges.MediaFiles) > 0 {
		f := e.Edges.MediaFiles[0]
		out.Quality = &f.Quality
		sz := f.Size
		out.Size = &sz
	}
	return out
}

func requestUserToAPI(u *ent.User) RequestUser {
	ru := RequestUser{Id: u.ID, Email: u.Email}
	if u.DisplayName != "" {
		ru.DisplayName = &u.DisplayName
	}
	return ru
}

func requestToAPI(r *ent.Request) Request {
	out := Request{
		Id:        r.ID,
		MediaType: RequestMediaType(r.MediaType),
		MediaId:   r.MediaID,
		Title:     r.Title,
		Status:    RequestStatus(r.Status),
		CreatedAt: r.CreateTime,
		UpdatedAt: r.UpdateTime,
	}
	if r.Reason != "" {
		out.Reason = &r.Reason
	}
	if u := r.Edges.Requester; u != nil {
		out.Requester = requestUserToAPI(u)
	}
	if u := r.Edges.ApprovedBy; u != nil {
		au := requestUserToAPI(u)
		out.ApprovedBy = &au
	}
	return out
}

// toSearchResult maps an indexer search result to the API shape, enriching it
// with release metadata parsed from the release title.
func toSearchResult(r indexer.SearchResult) SearchResult {
	item := SearchResult{
		Title:       r.Title,
		DownloadUrl: r.Download,
		Size:        r.Size,
		Seeders:     r.Seeders,
	}
	parsed := library.Parse(filepath.Base(r.Title))
	if r.InfoURL != "" {
		item.InfoUrl = &r.InfoURL
	}
	if r.Leechers > 0 {
		item.Leechers = &r.Leechers
	}
	if !r.PublishDate.IsZero() {
		pub := r.PublishDate
		item.PublishedAt = &pub
	}
	if r.Indexer != "" {
		idx := r.Indexer
		item.Indexer = &idx
	}
	if parsed.Group != "" {
		g := parsed.Group
		item.ReleaseGroup = &g
	}
	if parsed.Resolution != "" {
		res := parsed.Resolution
		item.Resolution = &res
	}
	if parsed.Source != "" {
		src := parsed.Source
		item.Source = &src
	}
	if parsed.Codec != "" {
		cdc := parsed.Codec
		item.Codec = &cdc
	}
	return item
}
