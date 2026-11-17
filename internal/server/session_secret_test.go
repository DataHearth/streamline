package server

import (
	"context"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("ensureSessionSecret", Label("unit", "server", "auth"), func() {
	secretOf := func() string {
		a := config.Get().Auth
		return config.SecretValue(a.SessionSecret, a.SessionSecretFile)
	}

	It("leaves a configured secret alone", func() {
		configtest.Setup(map[string]any{
			"auth": map[string]any{"session_secret": "configured-session-secret"},
		})
		Expect(ensureSessionSecret(context.Background())).To(Succeed())
		Expect(secretOf()).To(Equal("configured-session-secret"))
	})

	It("generates one in memory when nothing is configured", func() {
		configtest.Setup(nil)
		Expect(ensureSessionSecret(context.Background())).To(Succeed())
		Expect(secretOf()).NotTo(BeEmpty())
	})

	It("generates and persists one into a writable config", func() {
		configtest.SetupFile(nil)
		Expect(ensureSessionSecret(context.Background())).To(Succeed())
		// config.Load replaces the singleton secretOf reads, so the generated
		// secret is taken before the file is read back.
		want := secretOf()
		Expect(want).NotTo(BeEmpty())
		reloaded, err := config.Load(config.Path())
		Expect(err).NotTo(HaveOccurred())
		Expect(reloaded.Auth.SessionSecret).To(Equal(want))
	})

	It("accepts a session_secret_file that holds a secret", func() {
		path := filepath.Join(GinkgoT().TempDir(), "session_secret")
		Expect(
			os.WriteFile(path, []byte("from-file-session-secret\n"), 0o600),
		).To(Succeed())
		configtest.Setup(map[string]any{
			"auth": map[string]any{"session_secret_file": path},
		})
		Expect(ensureSessionSecret(context.Background())).To(Succeed())
		Expect(secretOf()).To(Equal("from-file-session-secret"))
	})
})
