package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/buildinfo"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/server"
	"github.com/urfave/cli/v3"
)

// versionString assembles the --version line. buildinfo holds the goreleaser
// ldflag values; we fall back to placeholders so the CLI stays readable when
// built via plain go run / go build.
func versionString() string {
	v := buildinfo.Version
	if v == "" {
		v = "dev"
	}
	c := buildinfo.Commit
	if c == "" {
		c = "none"
	}
	d := buildinfo.Date
	if d == "" {
		d = "unknown"
	}
	return fmt.Sprintf("%s (commit %s, built %s)", v, c, d)
}

func main() {
	cmd := &cli.Command{
		Name:        "streamline",
		Usage:       "unified media management platform",
		Description: "Self-hosted unified media manager. Replaces the *arr stack (Radarr, Sonarr, Lidarr, Readarr) and Overseerr with a single binary.",
		Version:     versionString(),
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "path to config file",
			},
		},
		Action: run,
		Commands: []*cli.Command{
			{
				Name:  "auth",
				Usage: "auth maintenance commands",
				Commands: []*cli.Command{
					{
						Name:      "unlock",
						Usage:     "clear lockout state on a user account",
						ArgsUsage: "<email>",
						Action:    authUnlock,
					},
				},
			},
			{
				Name:  "config",
				Usage: "manage configuration",
				Commands: []*cli.Command{
					{
						Name:  "init",
						Usage: "write a default config to stdout or a file",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "output",
								Aliases: []string{"o"},
								Usage:   "output path (default: stdout)",
							},
						},
						Action: configInit,
					},
					{
						Name:  "validate",
						Usage: "load a config file (or stdin) and report any errors",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "config",
								Aliases: []string{"c"},
								Usage:   "path to config file (default: stdin)",
							},
						},
						Action: configValidate,
					},
				},
			},
		},
	}

	if err := cmd.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func configInit(_ context.Context, cmd *cli.Command) error {
	out := cmd.String("output")
	if out == "" {
		return config.DumpDefaults(os.Stdout)
	}
	f, err := os.Create(out)
	if err != nil {
		return fmt.Errorf("create %s: %w", out, err)
	}
	defer f.Close()
	if err := config.DumpDefaults(f); err != nil {
		return fmt.Errorf("write defaults: %w", err)
	}
	fmt.Fprintf(os.Stderr, "wrote default config to %s\n", out)
	return nil
}

func authUnlock(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 1 {
		return errors.New("usage: streamline auth unlock <email>")
	}
	email := strings.ToLower(strings.TrimSpace(cmd.Args().Get(0)))
	if email == "" {
		return errors.New("email is required")
	}
	if _, err := config.Load(cmd.String("config")); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	dbClient, err := db.Open(ctx, config.Get().DatabasePath())
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer dbClient.Close()
	svc, err := auth.New(db.New(dbClient))
	if err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	if err := svc.Unlock(ctx, email, auth.UnlockModeCLI); err != nil {
		return fmt.Errorf("unlock %s: %w", email, err)
	}
	fmt.Fprintf(os.Stderr, "unlocked %s\n", email)
	return nil
}

func configValidate(_ context.Context, cmd *cli.Command) error {
	path := cmd.String("config")
	var err error
	if path == "" {
		err = config.LoadReader(os.Stdin)
	} else {
		_, err = config.Load(path)
	}
	if err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	fmt.Fprintln(os.Stderr, "configuration is valid")
	return nil
}

func run(ctx context.Context, cmd *cli.Command) error {
	// 1. Load configuration
	cfg, err := config.Load(cmd.String("config"))
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// 2. Set up OpenTelemetry + unified slog handler (stderr + OTel bridge).
	//    Install as process-wide default so every package logs through the
	//    same pipeline via slog.Default()/slog.XContext. log.level/format and
	//    otel.endpoint are read from the config singleton internally.
	handler, otelShutdown, err := observability.Setup(
		ctx,
		observability.Config{
			ServiceName:      "streamline",
			ServiceVersion:   buildinfo.Version,
			ServiceCommit:    buildinfo.Commit,
			ServiceBuildDate: buildinfo.Date,
		},
	)
	if err != nil {
		return fmt.Errorf("otel: %w", err)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)

	// 3. Wire application
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	app, err := server.NewFromConfig(ctx)
	if err != nil {
		return fmt.Errorf("startup: %w", err)
	}
	defer app.DB.Close()

	// 5. Start scheduler in background
	go app.Scheduler.Start(ctx)

	// 6. Start HTTP server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	httpSrv := &http.Server{
		Addr:    addr,
		Handler: app.Server.Router(),
	}

	go func() {
		logger.InfoContext(ctx, "server starting", "addr", addr)
		if err := httpSrv.ListenAndServe(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			logger.ErrorContext(ctx, "server error", "error", err)
			stop()
		}
	}()

	// 7. Wait for shutdown signal
	<-ctx.Done()
	logger.InfoContext(ctx, "shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.ErrorContext(shutdownCtx, "http shutdown error", "error", err)
	}
	// Wait for in-flight background touches before closing the DB so SQLite's
	// WAL checkpoint on last connection close is not raced by a late writer.
	if err := app.Auth.Shutdown(shutdownCtx); err != nil {
		logger.ErrorContext(shutdownCtx, "auth shutdown error", "error", err)
	}
	if err := app.HTTPLogger.Close(); err != nil {
		logger.ErrorContext(shutdownCtx, "http access log close error", "error", err)
	}
	if err := otelShutdown(shutdownCtx); err != nil {
		logger.ErrorContext(shutdownCtx, "otel shutdown error", "error", err)
	}

	logger.InfoContext(shutdownCtx, "shutdown complete")
	return nil
}
