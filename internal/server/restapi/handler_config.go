package restapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/ffmpeg"
	"github.com/datahearth/streamline/internal/restart"
)

// GetConfigAuth returns the runtime-safe auth configuration. Admin only.
func (s *Server) GetConfigAuth(
	ctx context.Context,
	_ GetConfigAuthRequestObject,
) (GetConfigAuthResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetConfigAuth403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	return GetConfigAuth200JSONResponse{
		AuthConfigJSONResponse: authConfigView(config.Get().Auth),
	}, nil
}

// UpdateConfigAuth applies a partial update to the auth config. Admin only.
// Changes take effect immediately — no restart required.
func (s *Server) UpdateConfigAuth(
	ctx context.Context,
	req UpdateConfigAuthRequestObject,
) (UpdateConfigAuthResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateConfigAuth403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}

	patch := config.AuthPatch{}
	if req.Body.RegistrationMode != nil {
		v := string(*req.Body.RegistrationMode)
		patch.RegistrationMode = &v
	}
	if req.Body.SessionTtl != nil {
		patch.SessionTTL = req.Body.SessionTtl
	}
	if req.Body.DefaultRole != nil {
		v := string(*req.Body.DefaultRole)
		patch.DefaultRole = &v
	}
	if req.Body.Lockout != nil {
		threshold, err := narrowUint8(
			"lockout.threshold", req.Body.Lockout.Threshold, 255,
		)
		if err != nil {
			return UpdateConfigAuth422JSONResponse{
				UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
			}, nil
		}
		patch.Lockout = &config.LockoutPatch{
			Threshold: threshold,
			Window:    req.Body.Lockout.Window,
			Duration:  req.Body.Lockout.Duration,
		}
	}

	updated, err := config.UpdateAuth(ctx, patch)
	if configLocked(err) {
		return UpdateConfigAuth403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	}
	if err != nil {
		return UpdateConfigAuth422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	slog.InfoContext(ctx, "auth config updated",
		"registration_mode", updated.RegistrationMode,
		"session_ttl", updated.SessionTTL,
		"default_role", updated.DefaultRole,
	)
	return UpdateConfigAuth200JSONResponse{
		AuthConfigJSONResponse: authConfigView(updated),
	}, nil
}

// GetConfigLibrary returns the runtime-editable library configuration. Admin
// only.
func (s *Server) GetConfigLibrary(
	ctx context.Context,
	_ GetConfigLibraryRequestObject,
) (GetConfigLibraryResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetConfigLibrary403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	return GetConfigLibrary200JSONResponse{
		LibraryConfigJSONResponse: libraryConfigView(config.Get().Library),
	}, nil
}

// UpdateConfigLibrary applies a partial update to the library config. Admin
// only. Changes take effect immediately — no restart required.
func (s *Server) UpdateConfigLibrary(
	ctx context.Context,
	req UpdateConfigLibraryRequestObject,
) (UpdateConfigLibraryResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateConfigLibrary403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}

	grabFailures, errGrab := narrowUint8(
		"max_grab_failures", req.Body.MaxGrabFailures, 255,
	)
	attempts, errAttempts := narrowUint8(
		"import_max_attempts", req.Body.ImportMaxAttempts, 255,
	)
	drift, errDrift := narrowUint8(
		"drift_grace_ticks", req.Body.DriftGraceTicks, 20,
	)
	if err := errors.Join(errGrab, errAttempts, errDrift); err != nil {
		return UpdateConfigLibrary422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}

	patch := config.LibraryPatch{
		MonitorSpecials:      req.Body.MonitorSpecials,
		MovieNaming:          req.Body.MovieNaming,
		SeriesNaming:         req.Body.SeriesNaming,
		KeepTorrentSeeding:   req.Body.KeepTorrentSeeding,
		NoMatchCooldown:      req.Body.NoMatchCooldown,
		MaxGrabFailures:      grabFailures,
		ImportMaxAttempts:    attempts,
		DriftGraceTicks:      drift,
		AllowedDownloadRoots: req.Body.AllowedDownloadRoots,
	}
	if req.Body.ImportMode != nil {
		v := string(*req.Body.ImportMode)
		patch.ImportMode = &v
	}
	if req.Body.Probe != nil {
		patch.Probe = &config.ProbePatch{
			AlwaysAsk:        req.Body.Probe.AlwaysAsk,
			MinDurationRatio: req.Body.Probe.MinDurationRatio,
		}
	}

	updated, err := config.UpdateLibrary(ctx, patch)
	if configLocked(err) {
		return UpdateConfigLibrary403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	}
	if err != nil {
		return UpdateConfigLibrary422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	slog.InfoContext(ctx, "library config updated",
		"monitor_specials", updated.MonitorSpecials,
	)
	return UpdateConfigLibrary200JSONResponse{
		LibraryConfigJSONResponse: libraryConfigView(updated),
	}, nil
}

// GetConfigFfmpeg returns the ffmpeg configuration plus the current
// process's live probe result. Admin only.
func (s *Server) GetConfigFfmpeg(
	ctx context.Context,
	_ GetConfigFfmpegRequestObject,
) (GetConfigFfmpegResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetConfigFfmpeg403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	return GetConfigFfmpeg200JSONResponse{
		FFmpegConfigJSONResponse: ffmpegConfigView(
			ctx, config.Get().FFmpeg, s.prober,
		),
	}, nil
}

// UpdateConfigFfmpeg applies a partial update to the ffmpeg config. Admin
// only. enabled takes effect immediately; path is read once at boot into the
// long-lived Prober (ffmpeg.NewCLI in wire.go), so a path change flips the
// process-wide restart flag — the probe result echoed back keeps describing
// the process's current prober until that restart, per UpdateFFmpeg's doc
// comment.
func (s *Server) UpdateConfigFfmpeg(
	ctx context.Context,
	req UpdateConfigFfmpegRequestObject,
) (UpdateConfigFfmpegResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateConfigFfmpeg403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}

	prevPath := config.Get().FFmpeg.Path

	updated, err := config.UpdateFFmpeg(ctx, config.FFmpegPatch{
		Enabled: req.Body.Enabled,
		Path:    req.Body.Path,
	})
	if configLocked(err) {
		return UpdateConfigFfmpeg403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	}
	if err != nil {
		return UpdateConfigFfmpeg422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	// A client re-sending the current path must not raise the flag — only an
	// actual change does.
	if req.Body.Path != nil && *req.Body.Path != prevPath {
		restart.Mark()
	}
	slog.InfoContext(ctx, "ffmpeg config updated",
		"enabled", updated.Enabled,
		"path", updated.Path,
	)
	return UpdateConfigFfmpeg200JSONResponse{
		FFmpegConfigJSONResponse: ffmpegConfigView(ctx, updated, s.prober),
	}, nil
}

// GetConfigTranscoding returns the transcoding configuration. Admin only.
func (s *Server) GetConfigTranscoding(
	ctx context.Context,
	_ GetConfigTranscodingRequestObject,
) (GetConfigTranscodingResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetConfigTranscoding403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	status, reason := s.transcodeHWStatus(ctx)
	return GetConfigTranscoding200JSONResponse{
		TranscodingConfigJSONResponse: transcodingConfigView(
			config.Get().Transcoding, status, reason,
		),
	}, nil
}

// UpdateConfigTranscoding applies a partial update to the transcoding config.
// Admin only. The worker reads the scalar keys per tick and re-runs the
// hardware probe when hw_accel or hw_device changes, so nothing here marks
// the restart flag.
func (s *Server) UpdateConfigTranscoding(
	ctx context.Context,
	req UpdateConfigTranscodingRequestObject,
) (UpdateConfigTranscodingResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateConfigTranscoding403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}

	concurrent, errConcurrent := narrowUint8(
		"max_concurrent", req.Body.MaxConcurrent, 8,
	)
	failures, errFailures := narrowUint8(
		"max_failures", req.Body.MaxFailures, 10,
	)
	var verify config.TranscodeVerifyPatch
	var errVerify error
	if vp := req.Body.Verify; vp != nil {
		var errMax, errMin, errVMAF error
		verify.MaxSizePercent, errMax = narrowUint8Range(
			"max_size_percent",
			vp.MaxSizePercent,
			0,
			200,
		)
		verify.MinSizePercent, errMin = narrowUint8Range(
			"min_size_percent",
			vp.MinSizePercent,
			0,
			100,
		)
		verify.MinVMAF, errVMAF = narrowUint8Range("min_vmaf", vp.MinVmaf, 0, 100)
		verify.HealthCheck = vp.HealthCheck
		errVerify = errors.Join(errMax, errMin, errVMAF)
	}
	if err := errors.Join(errConcurrent, errFailures, errVerify); err != nil {
		return UpdateConfigTranscoding422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}

	var hwAccel *string
	if req.Body.HwAccel != nil {
		hwAccel = new(string(*req.Body.HwAccel))
	}
	updated, err := config.UpdateTranscoding(ctx, config.TranscodingPatch{
		Enabled:       req.Body.Enabled,
		MaxConcurrent: concurrent,
		MaxFailures:   failures,
		HWAccel:       hwAccel,
		HWDevice:      req.Body.HwDevice,
		Verify:        verify,
	})
	if configLocked(err) {
		return UpdateConfigTranscoding403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	}
	if err != nil {
		return UpdateConfigTranscoding422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	slog.InfoContext(ctx, "transcoding config updated",
		"enabled", updated.Enabled,
		"max_concurrent", updated.MaxConcurrent,
		"max_failures", updated.MaxFailures,
		"hw_accel", updated.HWAccel,
		"hw_device", updated.HWDevice,
		"verify_max_size_percent", updated.Verify.MaxSizePercent,
		"verify_min_size_percent", updated.Verify.MinSizePercent,
		"verify_health_check", updated.Verify.HealthCheck,
		"verify_min_vmaf", updated.Verify.MinVMAF,
	)
	status, reason := s.transcodeHWStatus(ctx)
	return UpdateConfigTranscoding200JSONResponse{
		TranscodingConfigJSONResponse: transcodingConfigView(
			updated, status, reason,
		),
	}, nil
}

// transcodeHWStatus reads the worker's hardware probe outcome, reporting off
// when the composition wired no worker.
func (s *Server) transcodeHWStatus(ctx context.Context) (string, string) {
	if s.transcoder == nil {
		return "off", ""
	}
	return s.transcoder.HWStatus(ctx)
}

// GetConfigSystem returns the logging, telemetry and retention configuration.
// Admin only.
func (s *Server) GetConfigSystem(
	ctx context.Context,
	_ GetConfigSystemRequestObject,
) (GetConfigSystemResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetConfigSystem403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	return GetConfigSystem200JSONResponse{
		SystemConfigJSONResponse: systemConfigView(config.Get()),
	}, nil
}

// UpdateConfigSystem applies a partial update to the log, otel and events
// sections. Admin only. events_retention reaches the next cleanup tick on its
// own; the log handlers and OTLP exporters are built once at boot, so a change
// to either flips the restart flag.
func (s *Server) UpdateConfigSystem(
	ctx context.Context,
	req UpdateConfigSystemRequestObject,
) (UpdateConfigSystemResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateConfigSystem403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}

	prev := config.Get()
	patch := config.SystemPatch{
		OTelEndpoint:    req.Body.OtelEndpoint,
		EventsRetention: req.Body.EventsRetention,
		Log:             logPatchFromAPI(req.Body.Log),
	}

	updated, err := config.UpdateSystem(ctx, patch)
	if configLocked(err) {
		return UpdateConfigSystem403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	}
	if err != nil {
		return UpdateConfigSystem422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	// Retention is the one field this process picks up on its own, so a patch
	// that only moved it must not ask for a restart.
	if updated.Log != prev.Log || updated.OTel != prev.OTel {
		restart.Mark()
	}
	slog.InfoContext(ctx, "system config updated",
		"log.app.level", updated.Log.App.Level,
		"otel.endpoint", updated.OTel.Endpoint,
		"events.retention", updated.Events.Retention,
	)
	return UpdateConfigSystem200JSONResponse{
		SystemConfigJSONResponse: systemConfigView(updated),
	}, nil
}

// logPatchFromAPI maps the generated LogConfig — one all-optional schema
// serving both the view and the patch — into the config layer's patch types.
func logPatchFromAPI(in *LogConfig) *config.LogPatch {
	if in == nil {
		return nil
	}
	out := &config.LogPatch{}
	if in.App != nil {
		out.App = &config.AppLogPatch{
			Enabled: in.App.Enabled,
			Output:  in.App.Output,
			Rotate:  rotatePatchFromAPI(in.App.Rotate),
		}
		if in.App.Level != nil {
			v := string(*in.App.Level)
			out.App.Level = &v
		}
		if in.App.Format != nil {
			v := string(*in.App.Format)
			out.App.Format = &v
		}
	}
	if in.Http != nil {
		out.HTTP = &config.HTTPLogPatch{
			Enabled: in.Http.Enabled,
			Output:  in.Http.Output,
			Rotate:  rotatePatchFromAPI(in.Http.Rotate),
		}
		if in.Http.Format != nil {
			v := string(*in.Http.Format)
			out.HTTP.Format = &v
		}
	}
	return out
}

func rotatePatchFromAPI(in *LogRotateConfig) *config.LogRotatePatch {
	if in == nil {
		return nil
	}
	return &config.LogRotatePatch{
		MaxSizeMB:  in.MaxSizeMb,
		MaxBackups: in.MaxBackups,
		MaxAgeDays: in.MaxAgeDays,
		Compress:   in.Compress,
	}
}

// systemConfigView maps the three sections into the generated view. Every
// nested field is populated even though the schema marks them optional — the
// shape is shared with the patch, where optional means "leave alone".
func systemConfigView(c *config.Config) SystemConfigJSONResponse {
	app, http := c.Log.App, c.Log.HTTP
	appLevel := AppLogConfigLevel(app.Level)
	appFormat := AppLogConfigFormat(app.Format)
	httpFormat := HTTPLogConfigFormat(http.Format)
	return SystemConfigJSONResponse{
		Log: LogConfig{
			App: &AppLogConfig{
				Enabled: &app.Enabled,
				Level:   &appLevel,
				Format:  &appFormat,
				Output:  &app.Output,
				Rotate:  rotateView(app.Rotate),
			},
			Http: &HTTPLogConfig{
				Enabled: &http.Enabled,
				Format:  &httpFormat,
				Output:  &http.Output,
				Rotate:  rotateView(http.Rotate),
			},
		},
		OtelEndpoint:    c.OTel.Endpoint,
		EventsRetention: c.Events.Retention,
		RestartRequired: restart.Pending(),
	}
}

func rotateView(r config.LogRotate) *LogRotateConfig {
	return &LogRotateConfig{
		MaxSizeMb:  &r.MaxSizeMB,
		MaxBackups: &r.MaxBackups,
		MaxAgeDays: &r.MaxAgeDays,
		Compress:   &r.Compress,
	}
}

// ListOIDCProviders returns every configured provider plus the process-wide
// restart-required flag. Admin only.
func (s *Server) ListOIDCProviders(
	ctx context.Context,
	_ ListOIDCProvidersRequestObject,
) (ListOIDCProvidersResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return ListOIDCProviders403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	providers := config.Get().Auth.OIDC
	items := make([]OIDCProviderView, 0, len(providers))
	for _, p := range providers {
		items = append(items, oidcProviderView(p))
	}
	return ListOIDCProviders200JSONResponse{
		OIDCProviderListJSONResponse: OIDCProviderListJSONResponse{
			Providers:       items,
			RestartRequired: restart.Pending(),
		},
	}, nil
}

// GetOIDCProvider returns a single provider by name. Admin only.
func (s *Server) GetOIDCProvider(
	ctx context.Context,
	req GetOIDCProviderRequestObject,
) (GetOIDCProviderResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetOIDCProvider403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	for _, p := range config.Get().Auth.OIDC {
		if p.Name == req.Name {
			return GetOIDCProvider200JSONResponse{
				OIDCProviderJSONResponse: OIDCProviderJSONResponse(
					oidcProviderView(p),
				),
			}, nil
		}
	}
	return GetOIDCProvider404JSONResponse{
		NotFoundJSONResponse: errNotFound("oidc provider not found"),
	}, nil
}

// CreateOIDCProvider validates the issuer via OIDC discovery and persists the
// provider. Success flips the process-wide restart-required flag because the
// runtime OIDC manager is initialised at startup.
func (s *Server) CreateOIDCProvider(
	ctx context.Context,
	req CreateOIDCProviderRequestObject,
) (CreateOIDCProviderResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return CreateOIDCProvider403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	p := config.OIDCConfig{
		Name:         req.Body.Name,
		Issuer:       req.Body.Issuer,
		ClientID:     req.Body.ClientId,
		ClientSecret: req.Body.ClientSecret,
	}
	switch err := config.AddOIDCProvider(ctx, p); {
	case errors.Is(err, config.ErrOIDCProviderExists):
		return CreateOIDCProvider409JSONResponse{
			ConflictJSONResponse: errConflict("oidc provider name already exists"),
		}, nil
	case errors.Is(err, config.ErrOIDCDiscoveryFailed):
		return CreateOIDCProvider422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	case configLocked(err):
		return CreateOIDCProvider403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return nil, err
	}
	restart.Mark()
	slog.InfoContext(
		ctx,
		"oidc provider mutated",
		"name",
		p.Name,
		"action",
		"create",
	)
	return CreateOIDCProvider201JSONResponse{
		OIDCProviderCreatedJSONResponse: OIDCProviderCreatedJSONResponse(
			oidcProviderView(p),
		),
	}, nil
}

// UpdateOIDCProvider merges patch fields into the named provider. Blank
// client_secret preserves the existing secret. Flips the restart flag.
func (s *Server) UpdateOIDCProvider(
	ctx context.Context,
	req UpdateOIDCProviderRequestObject,
) (UpdateOIDCProviderResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateOIDCProvider403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	patch := config.OIDCProviderPatch{
		Issuer:       req.Body.Issuer,
		ClientID:     req.Body.ClientId,
		ClientSecret: req.Body.ClientSecret,
	}
	switch err := config.UpdateOIDCProvider(ctx, req.Name, patch); {
	case errors.Is(err, config.ErrOIDCProviderNotFound):
		return UpdateOIDCProvider404JSONResponse{
			NotFoundJSONResponse: errNotFound("oidc provider not found"),
		}, nil
	case errors.Is(err, config.ErrOIDCDiscoveryFailed):
		return UpdateOIDCProvider422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	case configLocked(err):
		return UpdateOIDCProvider403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return nil, err
	}
	restart.Mark()
	slog.InfoContext(
		ctx,
		"oidc provider mutated",
		"name",
		req.Name,
		"action",
		"update",
	)
	for _, p := range config.Get().Auth.OIDC {
		if p.Name == req.Name {
			return UpdateOIDCProvider200JSONResponse{
				OIDCProviderJSONResponse: OIDCProviderJSONResponse(
					oidcProviderView(p),
				),
			}, nil
		}
	}
	// Should not happen — Update succeeded so the provider exists.
	return UpdateOIDCProvider404JSONResponse{
		NotFoundJSONResponse: errNotFound("oidc provider not found"),
	}, nil
}

// DeleteOIDCProvider removes the named provider. Flips the restart flag.
func (s *Server) DeleteOIDCProvider(
	ctx context.Context,
	req DeleteOIDCProviderRequestObject,
) (DeleteOIDCProviderResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return DeleteOIDCProvider403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	switch err := config.DeleteOIDCProvider(ctx, req.Name); {
	case errors.Is(err, config.ErrOIDCProviderNotFound):
		return DeleteOIDCProvider404JSONResponse{
			NotFoundJSONResponse: errNotFound("oidc provider not found"),
		}, nil
	case configLocked(err):
		return DeleteOIDCProvider403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	case err != nil:
		return nil, err
	}
	restart.Mark()
	slog.InfoContext(
		ctx,
		"oidc provider mutated",
		"name",
		req.Name,
		"action",
		"delete",
	)
	return DeleteOIDCProvider204Response{}, nil
}

// narrowUint8 converts a spec-bounded integer to the uint8 the config stores,
// refusing anything outside [1, max]. Every key that reaches this is a
// counter whose floor is 1, so only the ceiling varies.
func narrowUint8(key string, v *int, max int) (*uint8, error) {
	return narrowUint8Range(key, v, 1, max)
}

// narrowUint8Range rejects a request value outside [lo, hi] before it is
// converted: nothing validates a body against the spec's bounds, and
// uint8(300) is 44, a value the config's own tags then happily accept.
func narrowUint8Range(key string, v *int, lo, hi int) (*uint8, error) {
	if v == nil {
		return nil, nil
	}
	// gosec's overflow check needs a literal bound on both sides to prove the
	// conversion below is safe; `lo`/`hi` are parameters, not literals, so the
	// 0 and math.MaxUint8 clauses carry that proof and every caller's actual
	// range is still enforced by the lo/hi clauses beside them.
	if *v < 0 || *v < lo || *v > hi || *v > math.MaxUint8 {
		return nil, fmt.Errorf("%s: must be between %d and %d", key, lo, hi)
	}
	n := uint8(*v)
	return &n, nil
}

// authConfigView maps config.AuthConfig into the generated AuthConfigView.
func authConfigView(a config.AuthConfig) AuthConfigJSONResponse {
	threshold := int(a.Lockout.Threshold)
	window := a.Lockout.Window
	duration := a.Lockout.Duration
	return AuthConfigJSONResponse{
		RegistrationMode: AuthConfigViewRegistrationMode(a.RegistrationMode),
		SessionTtl:       a.SessionTTL,
		DefaultRole:      AuthConfigViewDefaultRole(a.DefaultRole),
		Lockout: LockoutConfig{
			Threshold: &threshold,
			Window:    &window,
			Duration:  &duration,
		},
	}
}

// libraryConfigView maps config.LibraryConfig into the generated view. The
// three roots ride along read-only: the settings page shows where the library
// lives, and pathmigrate owns moving it. probe is always populated (never
// null) so the page reflects the stored values instead of falling back to a
// client-side guess.
func libraryConfigView(l config.LibraryConfig) LibraryConfigJSONResponse {
	alwaysAsk := l.Probe.AlwaysAsk
	minDurationRatio := l.Probe.MinDurationRatio
	roots := l.AllowedDownloadRoots
	if roots == nil {
		roots = []string{}
	}
	moviePath, seriesPath, downloadPath := l.MoviePath, l.SeriesPath, l.DownloadPath
	return LibraryConfigJSONResponse{
		MonitorSpecials:      l.MonitorSpecials,
		MovieNaming:          l.MovieNaming,
		SeriesNaming:         l.SeriesNaming,
		ImportMode:           LibraryConfigViewImportMode(l.ImportMode),
		KeepTorrentSeeding:   l.KeepTorrentSeeding,
		NoMatchCooldown:      l.NoMatchCooldown,
		MaxGrabFailures:      int(l.MaxGrabFailures),
		ImportMaxAttempts:    int(l.ImportMaxAttempts),
		DriftGraceTicks:      int(l.DriftGraceTicks),
		AllowedDownloadRoots: &roots,
		MoviePath:            &moviePath,
		SeriesPath:           &seriesPath,
		DownloadPath:         &downloadPath,
		Probe: &ProbeConfig{
			AlwaysAsk:        &alwaysAsk,
			MinDurationRatio: &minDurationRatio,
		},
	}
}

// downloadConfigView maps config.DownloadConfig into the generated view.
func downloadConfigView(d config.DownloadConfig) DownloadConfigJSONResponse {
	return DownloadConfigJSONResponse{
		SelectiveFiles: d.SelectiveFiles,
		SelectionGrace: d.SelectionGrace,
	}
}

// metadataConfigView maps config.MetadataConfig into the generated view. The
// api keys never leave the process — only whether one is configured, and
// whether a *_file path owns it, which is what tells the settings page to
// offer the field read-only rather than accept an edit UpdateMetadata refuses.
//
// restart_required is the same process-wide flag the ffmpeg and OIDC views
// report: these four keys are read once, at client construction.
func metadataConfigView(m config.MetadataConfig) MetadataConfigJSONResponse {
	tmdbFile := m.TMDBAPIKeyFile != ""
	tvdbFile := m.TVDBAPIKeyFile != ""
	return MetadataConfigJSONResponse{
		Language:   m.Language,
		TmdbRegion: m.TMDBRegion,
		TmdbApiKeySet: config.SecretValue(
			m.TMDBAPIKey,
			m.TMDBAPIKeyFile,
		) != "",
		TvdbApiKeySet: config.SecretValue(
			m.TVDBAPIKey,
			m.TVDBAPIKeyFile,
		) != "",
		TmdbApiKeyFileManaged: &tmdbFile,
		TvdbApiKeyFileManaged: &tvdbFile,
		RestartRequired:       restart.Pending(),
	}
}

// GetConfigDownload returns the runtime-editable download configuration.
// Admin only.
func (s *Server) GetConfigDownload(
	ctx context.Context,
	_ GetConfigDownloadRequestObject,
) (GetConfigDownloadResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetConfigDownload403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	return GetConfigDownload200JSONResponse{
		DownloadConfigJSONResponse: downloadConfigView(config.Get().Download),
	}, nil
}

// UpdateConfigDownload applies a partial update to the download config. Admin
// only. Changes take effect immediately — the selective-file paths read
// config.Get() per grab.
func (s *Server) UpdateConfigDownload(
	ctx context.Context,
	req UpdateConfigDownloadRequestObject,
) (UpdateConfigDownloadResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateConfigDownload403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}

	updated, err := config.UpdateDownload(ctx, config.DownloadPatch{
		SelectiveFiles: req.Body.SelectiveFiles,
		SelectionGrace: req.Body.SelectionGrace,
	})
	if configLocked(err) {
		return UpdateConfigDownload403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	}
	if err != nil {
		return UpdateConfigDownload422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	slog.InfoContext(ctx, "download config updated",
		"selective_files", updated.SelectiveFiles,
		"selection_grace", updated.SelectionGrace,
	)
	return UpdateConfigDownload200JSONResponse{
		DownloadConfigJSONResponse: downloadConfigView(updated),
	}, nil
}

// GetConfigMetadata returns the metadata provider configuration, secrets
// elided. Admin only.
func (s *Server) GetConfigMetadata(
	ctx context.Context,
	_ GetConfigMetadataRequestObject,
) (GetConfigMetadataResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return GetConfigMetadata403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}
	return GetConfigMetadata200JSONResponse{
		MetadataConfigJSONResponse: metadataConfigView(config.Get().Metadata),
	}, nil
}

// UpdateConfigMetadata applies a partial update to the metadata config. Admin
// only. Every key here is read once at boot by metadata.NewTMDB/NewTVDB, so a
// real change flips the restart flag; re-sending the stored value does not.
func (s *Server) UpdateConfigMetadata(
	ctx context.Context,
	req UpdateConfigMetadataRequestObject,
) (UpdateConfigMetadataResponseObject, error) {
	if err := requireAdmin(ctx); err != nil {
		return UpdateConfigMetadata403JSONResponse{
			ForbiddenJSONResponse: notAdminResp,
		}, nil
	}

	prev := config.Get().Metadata
	updated, err := config.UpdateMetadata(ctx, config.MetadataPatch{
		TMDBAPIKey: req.Body.TmdbApiKey,
		TVDBAPIKey: req.Body.TvdbApiKey,
		Language:   req.Body.Language,
		TMDBRegion: req.Body.TmdbRegion,
	})
	if configLocked(err) {
		return UpdateConfigMetadata403JSONResponse{
			ForbiddenJSONResponse: forbiddenResp(err.Error()),
		}, nil
	}
	if err != nil {
		return UpdateConfigMetadata422JSONResponse{
			UnprocessableEntityJSONResponse: errUnprocessable(err.Error()),
		}, nil
	}
	if updated != prev {
		restart.Mark()
	}
	slog.InfoContext(ctx, "metadata config updated",
		"language", updated.Language,
		"tmdb_region", updated.TMDBRegion,
	)
	return UpdateConfigMetadata200JSONResponse{
		MetadataConfigJSONResponse: metadataConfigView(updated),
	}, nil
}

// ffmpegConfigView maps config.FFmpegConfig into the generated view, adding
// the live probe result off prober. found/resolved_path describe this
// process's prober, which was built from the path at boot — they lag a path
// change until the next restart even though enabled applies immediately.
// restart_required mirrors the same process-wide flag ListOIDCProviders
// reports, so a path fix surfaces here instead of only on the OIDC page.
func ffmpegConfigView(
	ctx context.Context,
	f config.FFmpegConfig,
	prober ffmpeg.Prober,
) FFmpegConfigJSONResponse {
	out := FFmpegConfigJSONResponse{
		Enabled:         f.Enabled,
		Path:            f.Path,
		RestartRequired: restart.Pending(),
	}
	found := prober.Available()
	out.Found = &found
	if found {
		resolved := prober.ResolvedPath()
		out.ResolvedPath = &resolved
	}
	// found describes ffprobe; the transcode worker needs ffmpeg, which can be
	// missing on its own. Asking is the only way to tell, and a failure is an
	// absent field rather than a failed response.
	if version, err := prober.Version(ctx); err != nil {
		slog.DebugContext(ctx, "ffmpeg version unavailable", "error", err)
	} else {
		out.Version = &version
	}
	return out
}

// transcodingConfigView maps config.TranscodingConfig plus the worker's
// hardware probe outcome into the generated view.
func transcodingConfigView(
	t config.TranscodingConfig,
	hwStatus, hwReason string,
) TranscodingConfigJSONResponse {
	out := TranscodingConfigJSONResponse{
		Enabled:       t.Enabled,
		MaxConcurrent: int(t.MaxConcurrent),
		MaxFailures:   int(t.MaxFailures),
		HwAccel:       TranscodingConfigViewHwAccel(t.HWAccel),
		HwDevice:      t.HWDevice,
		HwStatus:      TranscodingConfigViewHwStatus(hwStatus),
		Verify: TranscodeVerifyConfigView{
			MaxSizePercent: int(t.Verify.MaxSizePercent),
			MinSizePercent: int(t.Verify.MinSizePercent),
			HealthCheck:    t.Verify.HealthCheck,
			MinVmaf:        int(t.Verify.MinVMAF),
		},
	}
	if hwReason != "" {
		out.HwReason = &hwReason
	}
	return out
}

// oidcProviderView maps config.OIDCConfig into the generated OIDCProviderView.
// The raw secret never leaves the process — only a "configured" flag is emitted.
func oidcProviderView(p config.OIDCConfig) OIDCProviderView {
	return OIDCProviderView{
		Name:            p.Name,
		Issuer:          p.Issuer,
		ClientId:        p.ClientID,
		ClientSecretSet: p.ClientSecret != "" || p.ClientSecretFile != "",
	}
}
