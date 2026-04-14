package restapi

import (
	"context"

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
	return GetSystemInfo200JSONResponse{
		SystemInfoJSONResponse: SystemInfoJSONResponse(snapshotToAPI(snap)),
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
	return out
}

func diskUsageToAPI(u sysinfo.DiskUsage) *DiskUsage {
	return &DiskUsage{
		Used:  u.Used,
		Total: u.Total,
		Free:  u.Free,
		Pct:   u.Pct,
		Kind:  DiskUsageKind(u.Kind),
	}
}
