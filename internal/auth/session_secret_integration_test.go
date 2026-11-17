package auth

import (
	"context"
	"time"

	"github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/db"
)

// An empty HMAC key is a key everyone knows. The config refuses a configured
// one (config.Load); these specs pin the sign and verify paths, which refuse
// to use one whatever the service was built with.
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

	// Runs on the suite's own non-empty secret, so it only shows that a token
	// signed with the empty key fails verification against a real one. The
	// stored-empty-key spec below is what shows an empty key is never used.
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
})
