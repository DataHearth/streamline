package auth

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

// An empty HMAC key is a key everyone knows. These specs pin the three places
// that keep one out: construction, rotation, and the sign/verify paths
// themselves.
var _ = Describe("empty session secret", Label("integration", "auth"), func() {
	var (
		ctx   context.Context
		store db.Store
	)

	BeforeEach(func() {
		ctx = context.Background()
		dbClient, err := db.Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { dbClient.Close() })
		store = db.New(dbClient)
	})

	// forge signs admin claims for an existing session with the empty key,
	// which is all an attacker holding one live session needs to know.
	forge := func(svc *auth, token string) string {
		GinkgoHelper()
		live, err := svc.ValidateToken(token)
		Expect(err).NotTo(HaveOccurred())
		now := time.Now()
		forged, err := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
			UserID:    live.UserID + 1,
			Email:     "attacker@example.com",
			Role:      "admin",
			JTI:       live.JTI,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			Issuer:    "streamline",
			ID:        live.JTI,
		}).SignedString([]byte{})
		Expect(err).NotTo(HaveOccurred())
		return forged
	}

	Context("when auth.session_secret_file reads back empty", func() {
		BeforeEach(func() {
			path := filepath.Join(GinkgoT().TempDir(), "session_secret")
			Expect(os.WriteFile(path, []byte(" \n"), 0o600)).To(Succeed())
			configtest.Setup(map[string]any{
				"auth": map[string]any{
					"session_secret_file": path,
					"session_ttl":         "1h",
				},
			})
		})

		It("New refuses to build a service on it", func() {
			svc, err := New(store)
			Expect(err).To(MatchError(ErrEmptySessionSecret))
			Expect(svc).To(BeNil())
		})
	})

	It("New refuses when no secret is configured at all", func() {
		configtest.Setup(map[string]any{
			"auth": map[string]any{"session_ttl": "1h"},
		})
		_, err := New(store)
		Expect(err).To(MatchError(ErrEmptySessionSecret))
	})

	It("NewWithSecret refuses an empty secret", func() {
		_, err := NewWithSecret(store, "")
		Expect(err).To(MatchError(ErrEmptySessionSecret))
	})

	// Not empty, and no more secret than the empty key: each is a value
	// anyone can guess from knowing how the file came to be written.
	DescribeTable("NewWithSecret refuses a secret that is only technically there",
		func(secret string, want error) {
			_, err := NewWithSecret(store, secret)
			Expect(err).To(MatchError(want))
		},
		Entry("a lone space", " ", ErrEmptySessionSecret),
		Entry("a UTF-8 byte-order mark", "\xef\xbb\xbf", ErrShortSessionSecret),
		Entry(
			"an empty UTF-16 file as PowerShell 5 writes it",
			"\xff\xfe\r\x00\n\x00",
			ErrShortSessionSecret,
		),
		Entry(
			"one byte under the floor",
			strings.Repeat("k", MinSessionSecretLen-1),
			ErrShortSessionSecret,
		),
		Entry(
			"the floor in bytes, but padded to it with whitespace",
			strings.Repeat("k", MinSessionSecretLen-1)+" ",
			ErrShortSessionSecret,
		),
	)

	It("NewWithSecret accepts a secret exactly at the floor", func() {
		_, err := NewWithSecret(store, strings.Repeat("k", MinSessionSecretLen))
		Expect(err).NotTo(HaveOccurred())
	})

	// A sanity spec, not the regression for the empty-file boot: it runs on
	// the suite's own non-empty secret and passes without the fix. The specs
	// that pin the fix are New refusing above and the stored-empty-key one
	// below.
	It("a healthy service rejects a token signed with the empty key", func() {
		m, err := New(store)
		Expect(err).NotTo(HaveOccurred())
		svc := m.(*auth)
		_, token, err := svc.Register(
			ctx, "user@example.com", "passw0rd!", "member", SessionMeta{},
		)
		Expect(err).NotTo(HaveOccurred())

		_, err = svc.ValidateToken(forge(svc, token))
		Expect(err).To(HaveOccurred())
	})

	It("neither signs nor verifies should the key ever be empty", func() {
		m, err := New(store)
		Expect(err).NotTo(HaveOccurred())
		svc := m.(*auth)
		u, token, err := svc.Register(
			ctx, "user@example.com", "passw0rd!", "member", SessionMeta{},
		)
		Expect(err).NotTo(HaveOccurred())
		forged := forge(svc, token)

		empty := []byte{}
		svc.jwtSecret.Store(&empty)

		_, err = svc.ValidateToken(forged)
		Expect(err).To(MatchError(ErrEmptySessionSecret))
		_, err = svc.issueToken(ctx, u, SessionMeta{})
		Expect(err).To(MatchError(ErrEmptySessionSecret))
	})

	It(
		"refuses to rotate to an empty or short secret and keeps the live key",
		func() {
			m, err := New(store)
			Expect(err).NotTo(HaveOccurred())
			svc := m.(*auth)
			u, token, err := svc.Register(
				ctx, "user@example.com", "passw0rd!", "admin", SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())

			_, span := tracer.Start(ctx, "test")
			defer span.End()
			_, err = svc.applyJWTSecret(ctx, span, u.ID, "")
			Expect(err).To(MatchError(ErrEmptySessionSecret))
			_, err = svc.applyJWTSecret(ctx, span, u.ID, "too-short")
			Expect(err).To(MatchError(ErrShortSessionSecret))

			_, err = svc.ValidateToken(token)
			Expect(err).NotTo(HaveOccurred())
		},
	)
})
