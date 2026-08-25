package restapi

import (
	"context"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/sysinfo"
)

// GetSystemInfo returns the read-only environment summary surfaced on
// Settings → General. Admin only.
func (s *Server) GetSystemInfo(
	ctx context.Context,
	_ GetSystemInfoRequestObject,
) (GetSystemInfoResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetSystemInfo403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	snap := sysinfo.Collect()
	out := snapshotToAPI(snap)
	// Disabled ffmpeg means the operator opted out — no warning either way.
	if config.Get().FFmpeg.Enabled && !s.prober.Available() {
		warn := true
		out.FfmpegWarn = &warn
	}
	return GetSystemInfo200JSONResponse{
		SystemInfoJSONResponse: SystemInfoJSONResponse(out),
	}, nil
}

func snapshotToAPI(s sysinfo.Snapshot) SystemInfo {
	out := SystemInfo{
		AppName:   s.AppName,
		PublicUrl: s.PublicURL,
		HttpsWarn: s.HTTPSWarn,
		AuthMode:  s.AuthMode,
		DataDir:   s.DataDir,
		DbPath:    s.DBPath,
		Version:   s.Version,
		GoVersion: s.GoVersion,
		GoOsArch:  s.GoOSArch,
	}
	if s.Commit != "" {
		c := s.Commit
		out.Commit = &c
	}
	if s.BuiltAt != "" {
		b := s.BuiltAt
		out.BuiltAt = &b
	}
	if s.DBSize != "" {
		sz := s.DBSize
		out.DbSize = &sz
	}
	if s.DataUsage != nil {
		out.DataUsage = diskUsageToAPI(*s.DataUsage)
	}
	if s.DBUsage != nil {
		out.DbUsage = diskUsageToAPI(*s.DBUsage)
	}
	if s.LibraryDir != "" {
		d := s.LibraryDir
		out.LibraryDir = &d
	}
	if s.LibraryUsage != nil {
		out.LibraryUsage = diskUsageToAPI(*s.LibraryUsage)
	}
	if s.SeriesDir != "" {
		d := s.SeriesDir
		out.SeriesDir = &d
	}
	if s.SeriesUsage != nil {
		out.SeriesUsage = diskUsageToAPI(*s.SeriesUsage)
	}
	return out
}

func diskUsageToAPI(u sysinfo.DiskUsage) *DiskUsage {
	return &DiskUsage{
		Used:      u.Used,
		Total:     u.Total,
		Free:      u.Free,
		FreeBytes: u.FreeBytes,
		Pct:       u.Pct,
		Kind:      DiskUsageKind(u.Kind),
	}
}
