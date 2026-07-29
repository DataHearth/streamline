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
	"github.com/datahearth/streamline/internal/server"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

const (
	AdminEmail    = "e2e-admin@streamline.local"
	AdminPassword = "e2e-Admin-Passw0rd!"
)

// Start boots the real wired app (temp SQLite, seeded admin) on an ephemeral
// port and returns its base URL. Teardown is registered via DeferCleanup;
// call from BeforeSuite.
func Start() string {
	GinkgoHelper()
	// Same seam as the server suite: the HTTP access logger writes to
	// stderr with no injection point; repoint it at GinkgoWriter.
	prev := observability.StderrSink
	observability.StderrSink = GinkgoWriter
	DeferCleanup(func() { observability.StderrSink = prev })

	configtest.Setup(map[string]any{
		"auth": map[string]any{
			"session_secret": "e2e-session-secret",
			"seed_admin": map[string]any{
				"email":    AdminEmail,
				"password": AdminPassword,
			},
		},
	})

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
	return srv.URL
}
