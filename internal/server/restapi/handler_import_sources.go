package restapi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/datahearth/streamline/ent/schema"
	"github.com/datahearth/streamline/internal/arr"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/quality"
)

// knownFormat is the closure arr.TranslateProfile needs. It keeps that
// package free of the builtin table and the config singleton.
func knownFormat(name string) bool {
	if quality.IsBuiltinName(name) {
		return true
	}
	_, ok := config.FindCustomFormat(name)
	return ok
}

// arrSource validates the parts of a request that name an instance and opens
// a connection to it. A non-empty message is the 422 to answer with; every
// arr error text is composed by that package and safe to show verbatim — it
// is the only place the upstream reason appears.
func (s *Server) arrSource(
	ctx context.Context, app, url string, apiKey *string,
) (arr.Library, string) {
	if apiKey == nil || *apiKey == "" {
		return nil, "an API key is required"
	}
	if draftTargetRefused(ctx, url) {
		return nil, draftTargetRefusedMessage
	}
	client, err := s.arrClients.Client(arr.App(app), url, *apiKey)
	if err != nil {
		return nil, err.Error()
	}
	if err := client.TestConnection(ctx); err != nil {
		return nil, err.Error()
	}
	return client, ""
}

func (s *Server) PreviewImportSource(
	ctx context.Context, req PreviewImportSourceRequestObject,
) (PreviewImportSourceResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return PreviewImportSource403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	fail := func(msg string) (PreviewImportSourceResponseObject, error) {
		return PreviewImportSource422JSONResponse{
			UnprocessableEntityJSONResponse: errConnectionFailed(msg),
		}, nil
	}
	app := arr.App(req.Body.App)
	client, msg := s.arrSource(ctx, string(app), req.Body.Url, req.Body.ApiKey)
	if msg != "" {
		return fail(msg)
	}

	preview, err := buildPreview(ctx, app, client)
	if err != nil {
		return fail(err.Error())
	}
	return PreviewImportSource200JSONResponse{
		ArrPreviewJSONResponse: ArrPreviewJSONResponse(preview),
	}, nil
}

// rootFor returns the index of the longest root folder containing path, or
// -1. The longest wins so /media/movies cannot claim /media/movies-4k titles.
func rootFor(path string, roots []arr.RootFolder) int {
	best, bestLen := -1, -1
	clean := filepath.Clean(path)
	for i, r := range roots {
		root := filepath.Clean(r.Path)
		if clean != root &&
			!strings.HasPrefix(clean, root+string(filepath.Separator)) {
			continue
		}
		if len(root) > bestLen {
			best, bestLen = i, len(root)
		}
	}
	return best
}

func buildPreview(
	ctx context.Context, app arr.App, client arr.Library,
) (ArrPreview, error) {
	st, err := client.Status(ctx)
	if err != nil {
		return ArrPreview{}, err
	}
	roots, err := client.RootFolders(ctx)
	if err != nil {
		return ArrPreview{}, err
	}
	profiles, err := client.QualityProfiles(ctx)
	if err != nil {
		return ArrPreview{}, err
	}
	indexers, err := client.Indexers(ctx)
	if err != nil {
		return ArrPreview{}, err
	}
	clients, err := client.DownloadClients(ctx)
	if err != nil {
		return ArrPreview{}, err
	}

	out := ArrPreview{
		App:             ArrPreviewApp(app),
		Version:         st.Version,
		RootFolders:     make([]ArrRootFolder, len(roots)),
		QualityProfiles: make([]ArrProfileTranslation, 0, len(profiles)),
		Indexers:        []ArrIndexerOption{},
		DownloadClients: []ArrClientOption{},
	}
	if st.InstanceName != "" {
		name := st.InstanceName
		out.InstanceName = &name
	}
	for i, r := range roots {
		out.RootFolders[i] = ArrRootFolder{Path: r.Path, Accessible: r.Accessible}
	}
	setSample := func(i int, path string) {
		if i >= 0 && out.RootFolders[i].SamplePath == nil && path != "" {
			p := path
			out.RootFolders[i].SamplePath = &p
		}
	}

	if app == arr.Sonarr {
		shows, err := client.Series(ctx)
		if err != nil {
			return ArrPreview{}, err
		}
		// A series carries no file paths, so a sample costs an episode
		// request: one per root folder, against its first show with a file.
		sampled := map[int]bool{}
		for _, sh := range shows {
			out.Counts.Titles++
			if sh.Monitored {
				out.Counts.Monitored++
			}
			i := rootFor(sh.Path, roots)
			if i >= 0 {
				out.RootFolders[i].TitleCount++
			}
			if sh.Statistics == nil || sh.Statistics.EpisodeFileCount == 0 {
				continue
			}
			out.Counts.WithFile++
			if i < 0 || sampled[i] {
				continue
			}
			sampled[i] = true
			eps, err := client.Episodes(ctx, sh.ID)
			if err != nil {
				return ArrPreview{}, err
			}
			for _, e := range eps {
				if e.EpisodeFile != nil {
					setSample(i, e.EpisodeFile.Path)
					break
				}
			}
		}
	} else {
		movies, err := client.Movies(ctx)
		if err != nil {
			return ArrPreview{}, err
		}
		for _, m := range movies {
			out.Counts.Titles++
			if m.Monitored {
				out.Counts.Monitored++
			}
			i := rootFor(m.Path, roots)
			if i >= 0 {
				out.RootFolders[i].TitleCount++
			}
			if m.MovieFile == nil {
				continue
			}
			out.Counts.WithFile++
			setSample(i, m.MovieFile.Path)
		}
	}

	for _, p := range profiles {
		entry, notes := arr.TranslateProfile(p, knownFormat)
		t := ArrProfileTranslation{
			SourceId:   p.ID,
			SourceName: p.Name,
			Translated: qualityProfileCreateFromEntry(entry),
			Notes:      notes,
		}
		if t.Notes == nil {
			t.Notes = []string{}
		}
		if _, ok := config.LookupQualityProfile(p.Name); ok {
			name := p.Name
			t.Existing = &name
		}
		out.QualityProfiles = append(out.QualityProfiles, t)
	}

	for _, ix := range arr.TranslateIndexers(indexers) {
		opt := ArrIndexerOption{
			Name:        ix.Name,
			Kind:        ArrIndexerOptionKind(ix.Kind),
			Collapses:   ix.Collapses,
			NeedsSecret: ix.NeedsSecret,
			Enabled:     ix.Entry.Enabled,
		}
		if ix.Reason != "" {
			reason := ix.Reason
			opt.Reason = &reason
		}
		if ix.Kind != arr.IndexerUnsupported {
			_, opt.Conflict = config.FindIndexer(ix.Name)
		}
		out.Indexers = append(out.Indexers, opt)
	}

	for _, dc := range arr.TranslateDownloadClients(clients) {
		opt := ArrClientOption{
			Name:        dc.Name,
			NeedsSecret: dc.NeedsSecret,
			Enabled:     dc.Entry.Enabled,
		}
		if dc.Reason != "" {
			reason := dc.Reason
			opt.Reason = &reason
		} else {
			ct := ArrClientOptionClientType(dc.Entry.ClientType)
			opt.ClientType = &ct
			_, opt.Conflict = config.FindDownloadClient(dc.Name)
		}
		out.DownloadClients = append(out.DownloadClients, opt)
	}
	return out, nil
}

func (s *Server) CheckImportSourcePaths(
	ctx context.Context, req CheckImportSourcePathsRequestObject,
) (CheckImportSourcePathsResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return CheckImportSourcePaths403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	out := make([]ArrRootCheck, 0, len(req.Body.Roots))
	for _, r := range req.Body.Roots {
		var sample string
		if r.SamplePath != nil {
			sample = *r.SamplePath
		}
		out = append(out, checkRoot(r.From, r.To, sample))
	}
	return CheckImportSourcePaths200JSONResponse{Roots: out}, nil
}

func checkRoot(from, to, sample string) ArrRootCheck {
	row := ArrRootCheck{From: from, To: to, Found: true}
	if sample == "" {
		return row
	}
	row.Resolved, _ = arr.MapRoot(sample, []arr.RootMapping{{From: from, To: to}})
	if _, err := os.Stat(row.Resolved); err != nil {
		row.Found = false
		reason := statReason(err)
		row.Reason = &reason
	}
	return row
}

// statReason keeps the class of a stat failure without the errno text, which
// on a network mount can name the server.
func statReason(err error) ArrRootCheckReason {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return ArrRootCheckReasonNotFound
	case errors.Is(err, os.ErrPermission):
		return ArrRootCheckReasonPermissionDenied
	default:
		return ArrRootCheckReasonUnreadable
	}
}

// migrationMappings validates and converts a create request's mappings. A
// non-empty message is the 422 to answer with.
func migrationMappings(body *ImportScanCreateRequest) (schema.ScanMappings, string) {
	var m schema.ScanMappings
	if body.RootMappings != nil {
		for _, r := range *body.RootMappings {
			var sample string
			if r.SamplePath != nil {
				sample = *r.SamplePath
			}
			if chk := checkRoot(r.From, r.To, sample); !chk.Found {
				return m, fmt.Sprintf(
					"root folder %s: the sample file does not resolve under %s (%s)",
					r.From, r.To, *chk.Reason,
				)
			}
			m.Roots = append(m.Roots, schema.RootMapping{From: r.From, To: r.To})
		}
	}
	if body.ProfileMappings != nil {
		for _, p := range *body.ProfileMappings {
			pm := schema.ProfileMapping{SourceID: p.SourceId, Target: p.Target}
			if p.SourceName != nil {
				pm.SourceName = *p.SourceName
			}
			m.Profiles = append(m.Profiles, pm)
		}
	}
	return m, ""
}

// profilesToCreate collects the profiles a create request asks for. A target
// that already exists identically is skipped, so retrying a start that failed
// after the profiles landed does not collide with its own first attempt; one
// that exists differently is refused before anything is written.
func profilesToCreate(
	body *ImportScanCreateRequest,
) ([]config.QualityProfileEntry, string) {
	if body.ProfileMappings == nil {
		return nil, ""
	}
	var out []config.QualityProfileEntry
	seen := map[string]bool{}
	for _, p := range *body.ProfileMappings {
		if p.Create == nil || seen[p.Target] {
			continue
		}
		seen[p.Target] = true
		create := *p.Create
		create.Name = p.Target
		entry := qualityProfileFromCreate(create)
		if existing, ok := config.LookupQualityProfile(p.Target); ok {
			if reflect.DeepEqual(
				normalizeProfile(existing),
				normalizeProfile(entry),
			) {
				continue
			}
			return nil, fmt.Sprintf(
				"a quality profile named %q already exists; map onto it or pick another name",
				p.Target,
			)
		}
		out = append(out, entry)
	}
	return out, ""
}

// normalizeProfile folds the nil-versus-empty slice difference a YAML round
// trip introduces, so an identical profile compares equal.
func normalizeProfile(e config.QualityProfileEntry) config.QualityProfileEntry {
	if len(e.AllowedCodecs) == 0 {
		e.AllowedCodecs = nil
	}
	if len(e.Formats) == 0 {
		e.Formats = nil
	}
	return e
}

func (s *Server) ApplyImportSourceConfig(
	ctx context.Context, req ApplyImportSourceConfigRequestObject,
) (ApplyImportSourceConfigResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ApplyImportSourceConfig403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	refuse := func(
		resp UnprocessableEntityJSONResponse,
	) (ApplyImportSourceConfigResponseObject, error) {
		return ApplyImportSourceConfig422JSONResponse{
			UnprocessableEntityJSONResponse: resp,
		}, nil
	}

	var wantIdx, wantDC []ArrConfigSelection
	if req.Body.Indexers != nil {
		wantIdx = *req.Body.Indexers
	}
	if req.Body.DownloadClients != nil {
		wantDC = *req.Body.DownloadClients
	}
	out := ApplySourceConfigResponse{
		Indexers:        []string{},
		DownloadClients: []string{},
	}
	if len(wantIdx) == 0 && len(wantDC) == 0 {
		return ApplyImportSourceConfig200JSONResponse{
			ApplySourceConfigResultJSONResponse: ApplySourceConfigResultJSONResponse(
				out,
			),
		}, nil
	}

	client, msg := s.arrSource(
		ctx, string(req.Body.App), req.Body.Url, req.Body.ApiKey,
	)
	if msg != "" {
		return refuse(errConnectionFailed(msg))
	}
	var indexers, clients []arr.Provider
	var err error
	if len(wantIdx) > 0 {
		if indexers, err = client.Indexers(ctx); err != nil {
			return refuse(errConnectionFailed(err.Error()))
		}
	}
	if len(wantDC) > 0 {
		if clients, err = client.DownloadClients(ctx); err != nil {
			return refuse(errConnectionFailed(err.Error()))
		}
	}

	idxEntries, msg := selectIndexers(arr.TranslateIndexers(indexers), wantIdx)
	if msg != "" {
		return refuse(errUnprocessable(msg))
	}
	dcEntries, msg := selectClients(arr.TranslateDownloadClients(clients), wantDC)
	if msg != "" {
		return refuse(errUnprocessable(msg))
	}

	if err := config.AddResources(ctx, nil, idxEntries, dcEntries); err != nil {
		if configLocked(err) {
			return ApplyImportSourceConfig403JSONResponse{
				ForbiddenJSONResponse: forbiddenResp(err.Error()),
			}, nil
		}
		return refuse(errUnprocessable(err.Error()))
	}
	for _, e := range idxEntries {
		out.Indexers = append(out.Indexers, e.Name)
	}
	for _, e := range dcEntries {
		out.DownloadClients = append(out.DownloadClients, e.Name)
	}
	return ApplyImportSourceConfig200JSONResponse{
		ApplySourceConfigResultJSONResponse: ApplySourceConfigResultJSONResponse(
			out,
		),
	}, nil
}

func selectIndexers(
	all []arr.TranslatedIndexer, want []ArrConfigSelection,
) ([]config.IndexerEntry, string) {
	byName := make(map[string]arr.TranslatedIndexer, len(all))
	for _, ix := range all {
		byName[ix.Name] = ix
	}
	var out []config.IndexerEntry
	seen := map[string]bool{}
	for _, w := range want {
		if seen[w.Name] {
			continue
		}
		seen[w.Name] = true
		ix, ok := byName[w.Name]
		switch {
		case !ok:
			return nil, fmt.Sprintf("the instance has no indexer named %q", w.Name)
		case ix.Kind == arr.IndexerUnsupported:
			return nil, fmt.Sprintf("indexer %q: %s", w.Name, ix.Reason)
		}
		if w.Secret != nil && *w.Secret != "" {
			ix.Entry.APIKey = *w.Secret
		} else if ix.NeedsSecret {
			return nil, fmt.Sprintf(
				"indexer %q: the instance did not return its API key; send it as the selection's secret",
				w.Name,
			)
		}
		out = append(out, ix.Entry)
	}
	return out, ""
}

func selectClients(
	all []arr.TranslatedClient, want []ArrConfigSelection,
) ([]config.DownloadClientEntry, string) {
	byName := make(map[string]arr.TranslatedClient, len(all))
	for _, dc := range all {
		byName[dc.Name] = dc
	}
	var out []config.DownloadClientEntry
	seen := map[string]bool{}
	for _, w := range want {
		if seen[w.Name] {
			continue
		}
		seen[w.Name] = true
		dc, ok := byName[w.Name]
		switch {
		case !ok:
			return nil, fmt.Sprintf(
				"the instance has no download client named %q",
				w.Name,
			)
		case dc.Reason != "":
			return nil, fmt.Sprintf("download client %q: %s", w.Name, dc.Reason)
		}
		if w.Secret != nil && *w.Secret != "" {
			dc.Entry.Password = *w.Secret
		} else if dc.NeedsSecret {
			return nil, fmt.Sprintf(
				"download client %q: the instance did not return its password; send it as the selection's secret",
				w.Name,
			)
		}
		out = append(out, dc.Entry)
	}
	return out, ""
}
