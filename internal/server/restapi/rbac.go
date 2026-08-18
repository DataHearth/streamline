package restapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/observability"
)

// minRole is the authorization table for /api/v1: the lowest role allowed to
// invoke each operation. It is default-deny — roleGuard rejects any operation
// absent from the map, so a newly generated route is locked until it is listed
// here. rbac_test.go fails if an operation in StrictServerInterface is missing.
//
// Handlers keep their own requireAdmin/requireNotRequestOnly calls: those
// produce the spec-declared 403 envelope for the route. This table is the
// backstop that makes forgetting one non-exploitable, and it is authoritative
// whenever it is stricter.
//
// Reading an infrastructure resource is an admin act here, not a harmless GET:
// the download-client, indexer, media-server and torrent views carry hosts,
// ports, usernames, download directories, library sections and every connected
// peer's IP. request_only exists to file requests, so none of it is theirs to
// see — the tier is the enforcement, not a field filter in convert.go.
var minRole = map[string]string{
	"ListActivity":          roleRequestOnly,
	"ListDownloadHistory":   roleRequestOnly,
	"ClearCompletedHistory": roleAdmin,
	"DeleteHistoryItem":     roleAdmin,
	"ListPending":           roleRequestOnly,
	"IgnorePending":         roleAdmin,
	"ImportPending":         roleAdmin,
	"ReplacePending":        roleAdmin,
	"GetDownloadQueue":      roleRequestOnly,
	"CancelQueueItem":       roleAdmin,
	"PauseQueueItem":        roleAdmin,
	"ResumeQueueItem":       roleAdmin,

	"ListInvites":     roleAdmin,
	"CreateInvite":    roleAdmin,
	"RevokeInvite":    roleAdmin,
	"RotateJWTSecret": roleAdmin,

	"AuthMe":          roleRequestOnly,
	"UpdateMe":        roleRequestOnly,
	"ListMyApiKeys":   roleRequestOnly,
	"CreateMyApiKey":  roleRequestOnly,
	"DeleteMyApiKey":  roleRequestOnly,
	"ListMySessions":  roleRequestOnly,
	"DeleteMySession": roleRequestOnly,
	"ChangePassword":  roleRequestOnly,

	"ListUpcomingReleases": roleRequestOnly,

	"GetConfigAuth":       roleAdmin,
	"UpdateConfigAuth":    roleAdmin,
	"GetConfigLibrary":    roleAdmin,
	"UpdateConfigLibrary": roleAdmin,
	"GetConfigFfmpeg":     roleAdmin,
	"UpdateConfigFfmpeg":  roleAdmin,
	"ListOIDCProviders":   roleAdmin,
	"CreateOIDCProvider":  roleAdmin,
	"GetOIDCProvider":     roleAdmin,
	"UpdateOIDCProvider":  roleAdmin,
	"DeleteOIDCProvider":  roleAdmin,

	"ListDownloadClients":     roleAdmin,
	"CreateDownloadClient":    roleAdmin,
	"UpdateDownloadClient":    roleAdmin,
	"DeleteDownloadClient":    roleAdmin,
	"TestDownloadClient":      roleAdmin,
	"TestDraftDownloadClient": roleAdmin,

	"ListIndexers":     roleAdmin,
	"CreateIndexer":    roleAdmin,
	"UpdateIndexer":    roleAdmin,
	"DeleteIndexer":    roleAdmin,
	"TestIndexer":      roleAdmin,
	"TestDraftIndexer": roleAdmin,

	"ListImports":              roleAdmin,
	"StartImport":              roleAdmin,
	"GetImport":                roleAdmin,
	"DeleteImport":             roleAdmin,
	"CancelImport":             roleAdmin,
	"CommitImport":             roleAdmin,
	"ListImportFiles":          roleAdmin,
	"UpdateImportFileDecision": roleAdmin,
	"ListImportShows":          roleAdmin,
	"UpdateImportShowDecision": roleAdmin,

	"GetPathMigration":      roleAdmin,
	"GetPathMigrationRoots": roleAdmin,
	"StartPathMigration":    roleAdmin,
	"PreviewPathMigration":  roleAdmin,

	"ListMediaServers":            roleAdmin,
	"GetMediaServer":              roleAdmin,
	"CreateMediaServer":           roleAdmin,
	"UpdateMediaServer":           roleAdmin,
	"DeleteMediaServer":           roleAdmin,
	"TestMediaServer":             roleAdmin,
	"TestDraftMediaServer":        roleAdmin,
	"DiscoverMediaServerSections": roleAdmin,

	"ListMovies":              roleRequestOnly,
	"GetMovie":                roleRequestOnly,
	"GetMovieCounts":          roleRequestOnly,
	"GetMovieRecommendations": roleRequestOnly,
	"GetMoviePlayOnLinks":     roleMember,
	"AddMovie":                roleMember,
	"PatchMovie":              roleMember,
	"DeleteMovie":             roleMember,
	"DeleteMovieFile":         roleMember,
	"GrabMovieRelease":        roleMember,
	"RefreshMovieMetadata":    roleMember,
	"RenameMovieFiles":        roleMember,
	"SearchMovie":             roleMember,
	"SearchMovieNow":          roleMember,

	"ListQualityProfiles":  roleRequestOnly,
	"CreateQualityProfile": roleAdmin,
	"UpdateQualityProfile": roleAdmin,
	"DeleteQualityProfile": roleAdmin,

	"ListRequests":       roleRequestOnly,
	"CreateRequest":      roleRequestOnly,
	"GetRequestCounts":   roleRequestOnly,
	"GetRequestMetadata": roleRequestOnly,
	"ApproveRequest":     roleMember,
	"DenyRequest":        roleMember,
	"ReopenRequest":      roleMember,

	"ListSchedules":  roleAdmin,
	"GetSchedule":    roleAdmin,
	"UpdateSchedule": roleAdmin,
	"PauseSchedule":  roleAdmin,
	"ResumeSchedule": roleAdmin,
	"RunSchedule":    roleAdmin,

	"SearchTMDBMovie":    roleRequestOnly,
	"GetTMDBMovieDetail": roleRequestOnly,

	"ListSeries":              roleRequestOnly,
	"GetSeries":               roleRequestOnly,
	"GetSeriesCounts":         roleRequestOnly,
	"LookupSeries":            roleRequestOnly,
	"GetSeriesLookupDetail":   roleRequestOnly,
	"GetSeriesPlayOnLinks":    roleMember,
	"AddSeries":               roleMember,
	"PatchSeries":             roleMember,
	"DeleteSeries":            roleMember,
	"PatchSeason":             roleMember,
	"PatchEpisode":            roleMember,
	"DeleteEpisodeFile":       roleMember,
	"GrabSeriesRelease":       roleMember,
	"GrabSeasonRelease":       roleMember,
	"GrabEpisodeRelease":      roleMember,
	"BrowseSeriesReleases":    roleMember,
	"BrowseSeasonReleases":    roleMember,
	"BrowseEpisodeReleases":   roleMember,
	"RefreshSeriesMetadata":   roleMember,
	"RenameSeriesFiles":       roleMember,
	"SearchSeries":            roleMember,
	"ApplySpecialsToExisting": roleAdmin,

	"GetSystemInfo": roleAdmin,

	"ListTorrents":           roleAdmin,
	"GetTorrent":             roleAdmin,
	"AddTorrent":             roleAdmin,
	"DeleteTorrent":          roleAdmin,
	"PauseTorrent":           roleAdmin,
	"ResumeTorrent":          roleAdmin,
	"SetTorrentFilePriority": roleAdmin,

	"ListUsers":         roleAdmin,
	"CreateUser":        roleAdmin,
	"GetUser":           roleAdmin,
	"UpdateUser":        roleAdmin,
	"DeleteUser":        roleAdmin,
	"ResetUserPassword": roleAdmin,
	"RevokeUserApiKey":  roleAdmin,
	"RevokeUserSession": roleAdmin,
	"UnlockUser":        roleAdmin,
}

const (
	roleRequestOnly = "request_only"
	roleMember      = "member"
	roleAdmin       = "admin"
)

// roleGuard enforces minRole ahead of every strict handler. Writing the
// rejection itself and returning a nil response short-circuits the generated
// adapter, which only touches the ResponseWriter when the response is non-nil.
func roleGuard(f StrictHandlerFunc, operationID string) StrictHandlerFunc {
	return func(
		ctx context.Context,
		w http.ResponseWriter,
		r *http.Request,
		request any,
	) (any, error) {
		need, listed := minRole[operationID]
		if !listed {
			//nolint:sloglint // LogAttrs takes slog.Attr by API design
			slog.LogAttrs(ctx, observability.LevelCritical,
				"operation missing from the RBAC table, denying",
				slog.String("operation", operationID))
			denyJSON(ctx, w, http.StatusForbidden, "operation is not authorized")
			return nil, nil
		}
		c := auth.ClaimsFromContext(ctx)
		if c == nil {
			denyJSON(ctx, w, http.StatusUnauthorized, "authentication required")
			return nil, nil
		}
		if !auth.RoleAtLeast(c.Role, need) {
			denyJSON(ctx, w, http.StatusForbidden, need+" role required")
			return nil, nil
		}
		return f(ctx, w, r, request)
	}
}

func denyJSON(
	ctx context.Context,
	w http.ResponseWriter,
	status int,
	message string,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(Error{Message: message}); err != nil {
		slog.ErrorContext(ctx, "json encode failed", "error", err)
	}
}
