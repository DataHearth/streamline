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
		Expect(secretOf()).NotTo(BeEmpty())
		reloaded, err := config.Load(config.Path())
		Expect(err).NotTo(HaveOccurred())
		Expect(reloaded.Auth.SessionSecret).To(Equal(secretOf()))
	})

	// The file wins in config.SecretValue, so a secret generated beside an
	// empty file would never be the one signing: boot has to stop.
	DescribeTable(
		"refuses to boot on a session_secret_file that reads back empty",
		func(content string, backingFile bool) {
			path := filepath.Join(GinkgoT().TempDir(), "session_secret")
			Expect(os.WriteFile(path, []byte(content), 0o600)).To(Succeed())
			overrides := map[string]any{
				"auth": map[string]any{"session_secret_file": path},
			}
			if backingFile {
				configtest.SetupFile(overrides)
			} else {
				configtest.Setup(overrides)
			}

			err := ensureSessionSecret(context.Background())
			Expect(err).To(MatchError(ContainSubstring("auth.session_secret_file")))
			Expect(err).To(MatchError(ContainSubstring(path)))
			Expect(config.Get().Auth.SessionSecret).To(BeEmpty())

			_, err = NewFromConfig(context.Background())
			Expect(err).To(MatchError(ContainSubstring("auth.session_secret_file")))
		},
		Entry("empty, no backing config file", "", false),
		Entry("whitespace only, no backing config file", " \n\t\n", false),
		Entry("empty, writable config file", "", true),
		Entry("a UTF-8 byte-order mark and nothing else", "\xef\xbb\xbf", false),
		Entry(
			"an empty UTF-16 file as PowerShell 5 writes it",
			"\xff\xfe\r\x00\n\x00",
			false,
		),
		Entry("a placeholder under the floor", "changeme\n", false),
	)

	DescribeTable("refuses to boot on an inline session_secret under the floor",
		func(secret string) {
			configtest.SetupFile(map[string]any{
				"auth": map[string]any{"session_secret": secret},
			})

			err := ensureSessionSecret(context.Background())
			Expect(err).To(MatchError(ContainSubstring("auth.session_secret:")))
			// Nothing is generated over what the operator wrote.
			Expect(config.Get().Auth.SessionSecret).To(Equal(secret))

			_, err = NewFromConfig(context.Background())
			Expect(err).To(MatchError(ContainSubstring("auth.session_secret:")))
		},
		Entry("a lone space", " "),
		Entry("a short placeholder", "changeme"),
	)

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
