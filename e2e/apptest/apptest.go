// Package apptest boots the fully wired application for e2e suites.
package apptest

import (
	"context"
	"errors"
	"net/http/httptest"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/observability"
	"github.com/datahearth/streamline/internal/scheduler"
	"github.com/datahearth/streamline/internal/server"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

const (
	AdminEmail    = "e2e-admin@streamline.local"
	AdminPassword = "e2e-Admin-Passw0rd!"
)

// Start boots the real wired app (temp SQLite, seeded admin) on an ephemeral
// port and returns it with its base URL. Extra override maps merge on top of
// the base config, letting a suite point external clients at its own fakes.
// Teardown is registered via DeferCleanup; call from BeforeSuite.
//
// Only the HTTP server is started — cmd/main.go's other half, the scheduler
// loop, is left to StartScheduler so suites that don't need background jobs
// stay free of them.
func Start(extra ...map[string]any) (*server.App, string) {
	GinkgoHelper()
	// Same seam as the server suite: the HTTP access logger writes to
	// stderr with no injection point; repoint it at GinkgoWriter.
	prev := observability.StderrSink
	observability.StderrSink = GinkgoWriter
	DeferCleanup(func() { observability.StderrSink = prev })

	// SetupFile, not Setup: config-backed resource CRUD (quality profiles,
	// indexers, download clients, media servers, schedules) goes through
	// config.Update, which needs a backing file to write to.
	overrides := make([]map[string]any, 0, 1+len(extra))
	overrides = append(overrides, map[string]any{
		"auth": map[string]any{
			"session_secret": "e2e-session-secret",
			// Hidden seam (see server.apiFailureLimiter): every spec shares one
			// loopback address, and the route sweep alone spends 131 failures.
			"api_failure_limit": "0",
			// The default is "disabled", under which invite creation is
			// refused — specs that exercise invites need a mode that admits
			// new users.
			"registration_mode": "invite",
			"seed_admin": map[string]any{
				"email":    AdminEmail,
				"password": AdminPassword,
			},
		},
	})
	overrides = append(overrides, extra...)
	configtest.SetupFile(overrides...)

	app, err := server.NewFromConfig(context.Background())
	Expect(err).NotTo(HaveOccurred())
	srv := httptest.NewServer(app.Server.Router())
	DeferCleanup(func() {
		srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		Expect(
			errors.Join(
				app.Auth.Shutdown(ctx),
				app.HTTPLogger.Close(),
				app.DB.Close(),
			),
		).To(Succeed())
	})
	return app, srv.URL
}

// StartScheduler runs the scheduler loop cmd/main.go owns in production;
// without it POST /schedules/{name}/run answers ErrNotStarted and no job ever
// fires. Every registered job also runs once the moment the loop starts, so
// call it before a spec seeds the indexers, clients or media those jobs act
// on.
//
// The returned func stops the loop. Registering it as a DeferCleanup only
// after those entities are seeded is what makes teardown stop the jobs before
// deleting what they poll.
func StartScheduler(sched *scheduler.Scheduler) func() {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// A panic in a spec-launched goroutine aborts the process with no
		// attribution; GinkgoRecover turns it into a spec failure.
		defer GinkgoRecover()
		sched.Start(ctx)
	}()
	return cancel
}
