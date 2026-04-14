package auth

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/invite"
	entuser "github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("oidcRoleFromClaims", Label("unit", "auth"), func() {
	cfg := &config.Config{Auth: config.AuthConfig{OIDC: []config.OIDCConfig{{
		Name:        "kc",
		RoleClaim:   "groups",
		RoleMapping: map[string]string{"admins": "admin", "staff": "member"},
	}}}}

	It("returns the highest-privilege role across matching groups", func() {
		role, ok := oidcRoleFromClaims(cfg, "kc",
			map[string]any{"groups": []any{"staff", "admins"}})
		Expect(ok).To(BeTrue())
		Expect(role).To(Equal("admin"))
	})

	It("accepts a single string claim", func() {
		role, ok := oidcRoleFromClaims(cfg, "kc", map[string]any{"groups": "staff"})
		Expect(ok).To(BeTrue())
		Expect(role).To(Equal("member"))
	})

	It("returns false when no group maps", func() {
		_, ok := oidcRoleFromClaims(cfg, "kc",
			map[string]any{"groups": []any{"randos"}})
		Expect(ok).To(BeFalse())
	})

	It("returns false when the provider configures no mapping", func() {
		bare := &config.Config{Auth: config.AuthConfig{
			OIDC: []config.OIDCConfig{{Name: "kc"}},
		}}
		_, ok := oidcRoleFromClaims(bare, "kc", map[string]any{"groups": "admins"})
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("LoginOIDC unit", Label("unit", "auth"), func() {
	const ctxType = "*context.valueCtx"

	var (
		ctx       context.Context
		storeMock *dbmocks.MockStore_Expecter
		svc       *auth
	)

	BeforeEach(func() {
		ctx = context.Background()
		store := dbmocks.NewMockStore(GinkgoT())
		storeMock = store.EXPECT()
		m, err := New(store)
		svc = m.(*auth)
		Expect(err).NotTo(HaveOccurred())

		configtest.Setup(map[string]any{
			"auth": map[string]any{
				"session_secret":    "test-secret",
				"session_ttl":       "1h",
				"registration_mode": "open",
				"oidc_default_role": "member",
			},
		})
	})

	When("an OIDC identity already exists", func() {
		It("logs in the linked user without re-syncing when claims match", func() {
			owner := &ent.User{
				ID:          1,
				Email:       "u@x.com",
				DisplayName: "U",
				Role:        entuser.RoleMember,
			}
			id := &ent.OIDCIdentity{
				ID:    1,
				Edges: ent.OIDCIdentityEdges{Owner: owner},
			}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(id, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			u, tok, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(u).To(Equal(owner))
			Expect(tok).NotTo(BeEmpty())
		})

		It("syncs display_name when the claim differs and reloads the user", func() {
			owner := &ent.User{
				ID:          1,
				Email:       "u@x.com",
				DisplayName: "Old",
				Role:        entuser.RoleMember,
			}
			reloaded := &ent.User{
				ID:          1,
				Email:       "u@x.com",
				DisplayName: "New",
				Role:        entuser.RoleMember,
			}
			id := &ent.OIDCIdentity{
				ID:    1,
				Edges: ent.OIDCIdentityEdges{Owner: owner},
			}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(id, nil).
				Once()
			storeMock.UpdateUser(mock.AnythingOfType(ctxType), uint32(1),
				mock.MatchedBy(func(p db.UpdateUserParams) bool {
					return p.DisplayName != nil && *p.DisplayName == "New" &&
						p.Email == nil
				})).
				Return(reloaded, nil).Once()
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(reloaded, nil).Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			u, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"New",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(u.DisplayName).To(Equal("New"))
		})

		It("skips email update when the claim collides with another user", func() {
			owner := &ent.User{
				ID:          1,
				Email:       "old@x.com",
				DisplayName: "U",
				Role:        entuser.RoleMember,
			}
			collision := &ent.User{ID: 2, Email: "new@x.com"}
			id := &ent.OIDCIdentity{
				ID:    1,
				Edges: ent.OIDCIdentityEdges{Owner: owner},
			}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(id, nil).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "new@x.com").
				Return(collision, nil).Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			u, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"new@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(u.Email).To(Equal("old@x.com"))
		})

		It("clears lockout state when the user was previously locked", func() {
			locked := time.Now().Add(5 * time.Minute)
			lastFail := time.Now().Add(-1 * time.Minute)
			owner := &ent.User{
				ID: 1, Email: "u@x.com", DisplayName: "U",
				FailedLoginCount:  3,
				LastFailedLoginAt: &lastFail,
				LockedUntil:       &locked,
			}
			reloaded := &ent.User{ID: 1, Email: "u@x.com", DisplayName: "U"}
			id := &ent.OIDCIdentity{
				ID:    1,
				Edges: ent.OIDCIdentityEdges{Owner: owner},
			}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(id, nil).
				Once()
			storeMock.UpdateUser(mock.AnythingOfType(ctxType), uint32(1),
				mock.MatchedBy(func(p db.UpdateUserParams) bool {
					return p.FailedLoginCount != nil && *p.FailedLoginCount == 0 &&
						p.ClearLastFailedLoginAt && p.ClearLockedUntil
				})).
				Return(reloaded, nil).Once()
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(reloaded, nil).Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	When("the identity lookup errors with non-NotFound", func() {
		It("wraps the error", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, errors.New("query fail")).
				Once()
			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("query oidc identity")))
		})
	})

	When("email is unverified", func() {
		It("rejects with ErrOIDCEmailUnverified", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				false,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ErrOIDCEmailUnverified))
		})
	})

	When("a user with the same email already exists", func() {
		It("links identity, promotes local→both, and logs in", func() {
			existing := &ent.User{
				ID:         1,
				Email:      "u@x.com",
				AuthMethod: entuser.AuthMethodLocal,
			}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(existing, nil).Once()
			storeMock.CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.MatchedBy(func(p db.CreateOIDCIdentityParams) bool {
				return p.Provider == "google" && p.OwnerID == 1
			})).
				Return(&ent.OIDCIdentity{ID: 1}, nil).
				Once()
			storeMock.UpdateUser(mock.AnythingOfType(ctxType), uint32(1), mock.MatchedBy(func(p db.UpdateUserParams) bool {
				return p.AuthMethod != nil && *p.AuthMethod == entuser.AuthMethodBoth
			})).
				Return(&ent.User{ID: 1, AuthMethod: entuser.AuthMethodBoth}, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			u, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(u.AuthMethod).To(Equal(entuser.AuthMethodBoth))
		})

		It("does not promote when auth_method is already oidc", func() {
			existing := &ent.User{ID: 1, AuthMethod: entuser.AuthMethodOidc}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(existing, nil).Once()
			storeMock.CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
				Return(&ent.OIDCIdentity{ID: 1}, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
		})

		It("wraps CreateOIDCIdentity errors", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(&ent.User{ID: 1, AuthMethod: entuser.AuthMethodLocal}, nil).
				Once()
			storeMock.CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
				Return(nil, errors.New("link fail")).
				Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("link oidc identity")))
		})
	})

	When("no user matches and registration_mode is open", func() {
		It("creates a new user with the default role", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(nil, &ent.NotFoundError{}).Once()
			storeMock.CreateUser(mock.AnythingOfType(ctxType), mock.MatchedBy(func(p db.CreateUserParams) bool {
				return p.Email == "u@x.com" && p.Role == entuser.RoleMember &&
					p.AuthMethod == entuser.AuthMethodOidc
			})).
				Return(&ent.User{ID: 1, Email: "u@x.com"}, nil).
				Once()
			storeMock.CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
				Return(&ent.OIDCIdentity{ID: 1}, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	When("registration_mode is disabled and table is non-empty", func() {
		It("rejects with ErrOIDCRegDisabled", func() {
			configtest.Setup(map[string]any{
				"auth": map[string]any{
					"session_secret":    "test-secret",
					"session_ttl":       "1h",
					"registration_mode": "disabled",
					"oidc_default_role": "member",
				},
			})
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(nil, &ent.NotFoundError{}).Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ErrOIDCRegDisabled))
		})
	})

	When("registration_mode is invite", func() {
		BeforeEach(func() {
			configtest.Setup(map[string]any{
				"auth": map[string]any{
					"session_secret":    "test-secret",
					"session_ttl":       "1h",
					"registration_mode": "invite",
					"oidc_default_role": "member",
				},
			})
		})

		It("rejects with ErrOIDCNoInvite when no matching invite exists", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(nil, &ent.NotFoundError{}).Once()
			storeMock.FindUnusedInviteForEmail(mock.AnythingOfType(ctxType), "u@x.com", mock.AnythingOfType("time.Time")).
				Return(nil, &ent.NotFoundError{}).
				Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ErrOIDCNoInvite))
		})

		It("consumes the invite and uses its role", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(nil, &ent.NotFoundError{}).Once()
			storeMock.FindUnusedInviteForEmail(mock.AnythingOfType(ctxType), "u@x.com", mock.AnythingOfType("time.Time")).
				Return(&ent.Invite{ID: 9, Role: invite.RoleAdmin, ExpiresAt: time.Now().Add(time.Hour)}, nil).
				Once()
			storeMock.MarkInviteUsed(mock.AnythingOfType(ctxType), uint32(9), mock.AnythingOfType("time.Time")).
				Return(&ent.Invite{ID: 9}, nil).
				Once()
			storeMock.CreateUser(mock.AnythingOfType(ctxType), mock.MatchedBy(func(p db.CreateUserParams) bool {
				return p.Role == entuser.RoleAdmin
			})).
				Return(&ent.User{ID: 1, Role: entuser.RoleAdmin}, nil).
				Once()
			storeMock.CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
				Return(&ent.OIDCIdentity{ID: 1}, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			u, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(u.Role).To(Equal(entuser.RoleAdmin))
		})

		It("wraps MarkInviteUsed errors", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(nil, &ent.NotFoundError{}).Once()
			storeMock.FindUnusedInviteForEmail(mock.AnythingOfType(ctxType), "u@x.com", mock.AnythingOfType("time.Time")).
				Return(&ent.Invite{ID: 9, Role: invite.RoleMember, ExpiresAt: time.Now().Add(time.Hour)}, nil).
				Once()
			storeMock.MarkInviteUsed(mock.AnythingOfType(ctxType), uint32(9), mock.AnythingOfType("time.Time")).
				Return(nil, errors.New("mark fail")).
				Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("mark invite used")))
		})
	})

	When("CreateUser fails for the new-user path", func() {
		It("wraps the error", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(nil, &ent.NotFoundError{}).Once()
			storeMock.CreateUser(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateUserParams")).
				Return(nil, errors.New("create fail")).
				Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("create user")))
		})
	})

	When("issueToken fails for the existing-identity path", func() {
		It("returns the user with empty token and the wrapped error", func() {
			owner := &ent.User{ID: 7, Email: "u@x.com", DisplayName: "U"}
			id := &ent.OIDCIdentity{
				ID:    1,
				Edges: ent.OIDCIdentityEdges{Owner: owner},
			}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(id, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(nil, errors.New("session fail")).
				Once()

			u, tok, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(HaveOccurred())
			Expect(u).To(Equal(owner))
			Expect(tok).To(BeEmpty())
		})
	})

	When("FindUserByEmail returns a non-NotFound error", func() {
		It("wraps the error", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(nil, errors.New("query fail")).Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("query user by email")))
		})
	})

	When(
		"UpdateUser fails for the linked-existing path (auth_method promotion)",
		func() {
			It("wraps the error", func() {
				existing := &ent.User{ID: 1, AuthMethod: entuser.AuthMethodLocal}
				storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
					Return(nil, &ent.NotFoundError{}).
					Once()
				storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
					Return(existing, nil).Once()
				storeMock.CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
					Return(&ent.OIDCIdentity{ID: 1}, nil).
					Once()
				storeMock.UpdateUser(mock.AnythingOfType(ctxType), uint32(1), mock.AnythingOfType("db.UpdateUserParams")).
					Return(nil, errors.New("update fail")).
					Once()

				_, _, err := svc.LoginOIDC(
					ctx,
					"google",
					"sub-1",
					"u@x.com",
					"U",
					true,
					nil,
					SessionMeta{},
				)
				Expect(err).To(MatchError(ContainSubstring("update auth_method")))
			})
		},
	)

	When("issueToken fails for the linked-existing path", func() {
		It("returns the user with empty token and the wrapped error", func() {
			existing := &ent.User{ID: 1, AuthMethod: entuser.AuthMethodOidc}
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(existing, nil).Once()
			storeMock.CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
				Return(&ent.OIDCIdentity{ID: 1}, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(nil, errors.New("session fail")).
				Once()

			u, tok, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(HaveOccurred())
			Expect(u).To(Equal(existing))
			Expect(tok).To(BeEmpty())
		})
	})

	When("CreateOIDCIdentity fails after CreateUser succeeded", func() {
		It("wraps the error", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(nil, &ent.NotFoundError{}).Once()
			storeMock.CreateUser(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateUserParams")).
				Return(&ent.User{ID: 1}, nil).
				Once()
			storeMock.CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
				Return(nil, errors.New("create id fail")).
				Once()

			_, _, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("create identity")))
		})
	})

	When("issueToken fails for the new-user path", func() {
		It("returns the user with empty token and the wrapped error", func() {
			storeMock.FindOIDCIdentity(mock.AnythingOfType(ctxType), "google", "sub-1").
				Return(nil, &ent.NotFoundError{}).
				Once()
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(nil, &ent.NotFoundError{}).Once()
			created := &ent.User{ID: 1, Email: "u@x.com"}
			storeMock.CreateUser(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateUserParams")).
				Return(created, nil).
				Once()
			storeMock.CreateOIDCIdentity(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateOIDCIdentityParams")).
				Return(&ent.OIDCIdentity{ID: 1}, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(nil, errors.New("session fail")).
				Once()

			u, tok, err := svc.LoginOIDC(
				ctx,
				"google",
				"sub-1",
				"u@x.com",
				"U",
				true,
				nil,
				SessionMeta{},
			)
			Expect(err).To(HaveOccurred())
			Expect(u).To(Equal(created))
			Expect(tok).To(BeEmpty())
		})
	})
})
