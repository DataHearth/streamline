package auth

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("AuthService end-to-end", Label("integration", "auth"), func() {
	var (
		svc *auth
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		dbClient, err := db.Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { dbClient.Close() })
		svc = newTestService(dbClient)
	})

	It("re-reads auth.session_ttl from config on each issueToken", func() {
		configtest.Setup(map[string]any{
			"auth": map[string]any{
				"session_secret": "test-secret-key-for-jwt",
				"session_ttl":    "30m",
			},
		})

		_, _, err := svc.Register(
			ctx, "ttl@example.com", "passw0rd!", "member", SessionMeta{},
		)
		Expect(err).NotTo(HaveOccurred())

		token, err := svc.Login(ctx, "ttl@example.com", "passw0rd!", SessionMeta{})
		Expect(err).NotTo(HaveOccurred())
		claims, err := svc.ValidateToken(token)
		Expect(err).NotTo(HaveOccurred())
		Expect(claims.ExpiresAt.Time).To(
			BeTemporally("~", time.Now().Add(30*time.Minute), time.Minute),
		)
	})

	It("Register+Login+ValidateToken happy path round-trips claims", func() {
		u, regToken, err := svc.Register(
			ctx,
			"rt@x.com",
			"password123",
			"admin",
			SessionMeta{},
		)
		Expect(err).NotTo(HaveOccurred())

		regClaims, err := svc.ValidateToken(regToken)
		Expect(err).NotTo(HaveOccurred())
		Expect(regClaims.UserID).To(Equal(u.ID))

		token, err := svc.Login(ctx, "rt@x.com", "password123", SessionMeta{})
		Expect(err).NotTo(HaveOccurred())
		claims, err := svc.ValidateToken(token)
		Expect(err).NotTo(HaveOccurred())
		Expect(claims.Email).To(Equal("rt@x.com"))
		Expect(claims.Role).To(Equal("admin"))
	})
})
