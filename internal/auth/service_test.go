package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
	"golang.org/x/crypto/bcrypt"

	"github.com/datahearth/streamline/ent"
	entuser "github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/testutil"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("AuthService unit", Label("unit", "auth"), func() {
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
	})

	Describe("Register", func() {
		It("hashes password, creates user, and returns a JWT", func() {
			storeMock.CreateUser(mock.AnythingOfType(ctxType), mock.MatchedBy(func(p db.CreateUserParams) bool {
				return p.Email == "a@x.com" &&
					p.Role == entuser.RoleAdmin &&
					p.AuthMethod == entuser.AuthMethodLocal &&
					p.PasswordHash != "" && p.PasswordHash != "pw"
			})).
				Return(&ent.User{ID: 1, Email: "a@x.com", Role: entuser.RoleAdmin}, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			u, token, err := svc.Register(
				ctx,
				"a@x.com",
				"password123",
				"admin",
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(u.Email).To(Equal("a@x.com"))
			Expect(token).NotTo(BeEmpty())
		})

		It("wraps bcrypt failure when the password is too long", func() {
			_, _, err := svc.Register(
				ctx,
				"a@x.com",
				strings.Repeat("p", 80),
				"member",
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("hash password")))
		})

		It("wraps store create errors", func() {
			storeMock.CreateUser(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateUserParams")).
				Return(nil, errors.New("create fail")).
				Once()
			_, _, err := svc.Register(ctx, "a@x.com", "pw", "member", SessionMeta{})
			Expect(err).To(MatchError(ContainSubstring("create user")))
		})

		It("propagates issueToken failures (CreateSession)", func() {
			storeMock.CreateUser(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateUserParams")).
				Return(&ent.User{ID: 1}, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(nil, errors.New("session fail")).
				Once()
			_, _, err := svc.Register(ctx, "a@x.com", "pw", "member", SessionMeta{})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("Login", func() {
		hash, _ := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)

		It("returns invalid credentials when the user is not found", func() {
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "missing@x.com").
				Return(nil, &ent.NotFoundError{}).Once()
			_, err := svc.Login(ctx, "missing@x.com", "pw", SessionMeta{})
			Expect(err).To(MatchError("invalid credentials"))
		})

		It("returns invalid credentials when the store query errors", func() {
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "a@x.com").
				Return(nil, errors.New("query fail")).Once()
			_, err := svc.Login(ctx, "a@x.com", "pw", SessionMeta{})
			Expect(err).To(MatchError("invalid credentials"))
		})

		It("returns invalid credentials when the password is wrong", func() {
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "a@x.com").
				Return(&ent.User{ID: 1, PasswordHash: string(hash)}, nil).Once()
			tx := dbmocks.NewMockTx(GinkgoT())
			storeMock.Tx(mock.AnythingOfType(ctxType)).Return(tx, nil).Once()
			tx.EXPECT().FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1}, nil).Once()
			tx.EXPECT().UpdateUser(mock.AnythingOfType(ctxType), uint32(1),
				mock.AnythingOfType("db.UpdateUserParams")).
				Return(&ent.User{ID: 1}, nil).Once()
			tx.EXPECT().Commit().Return(nil).Once()
			_, err := svc.Login(ctx, "a@x.com", "wrong", SessionMeta{})
			Expect(err).To(MatchError("invalid credentials"))
		})

		It("issues a token when credentials are valid", func() {
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "a@x.com").
				Return(&ent.User{ID: 1, Email: "a@x.com", PasswordHash: string(hash)}, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			token, err := svc.Login(ctx, "a@x.com", "password", SessionMeta{})
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())
		})

		It("propagates issueToken failures (CreateSession)", func() {
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "a@x.com").
				Return(&ent.User{ID: 1, PasswordHash: string(hash)}, nil).Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(nil, errors.New("session fail")).
				Once()
			_, err := svc.Login(ctx, "a@x.com", "password", SessionMeta{})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("ValidateToken", func() {
		It("rejects tampered tokens", func() {
			_, err := svc.ValidateToken("not.a.real.token")
			Expect(err).To(HaveOccurred())
		})

		It("rejects tokens signed with a non-HMAC algorithm", func() {
			noneToken := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." +
				"eyJzdWIiOiJ4In0."
			_, err := svc.ValidateToken(noneToken)
			Expect(err).To(MatchError(ContainSubstring("unexpected signing method")))
		})

		It("returns claims for a valid token", func() {
			storeMock.CreateUser(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateUserParams")).
				Return(&ent.User{ID: 7, Email: "a@x.com", Role: entuser.RoleMember}, nil).
				Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			_, token, err := svc.Register(
				ctx,
				"a@x.com",
				"password",
				"member",
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())

			claims, err := svc.ValidateToken(token)
			Expect(err).NotTo(HaveOccurred())
			Expect(claims.UserID).To(Equal(uint32(7)))
			Expect(claims.Email).To(Equal("a@x.com"))
		})
	})

	Describe("CreateAPIKey", func() {
		It("persists hash and returns raw key + record", func() {
			storeMock.CreateAPIKey(ctx, mock.MatchedBy(func(p db.CreateAPIKeyParams) bool {
				return p.Name == "cli" && p.OwnerID == 7 && p.KeyHash != ""
			})).
				Return(&ent.ApiKey{ID: 5, Name: "cli"}, nil).
				Once()

			raw, rec, err := svc.CreateAPIKey(ctx, 7, "cli")
			Expect(err).NotTo(HaveOccurred())
			Expect(raw).NotTo(BeEmpty())
			Expect(rec.ID).To(Equal(uint32(5)))
		})

		It("wraps store errors", func() {
			storeMock.CreateAPIKey(ctx, mock.AnythingOfType("db.CreateAPIKeyParams")).
				Return(nil, errors.New("insert fail")).
				Once()
			_, _, err := svc.CreateAPIKey(ctx, 7, "cli")
			Expect(err).To(MatchError(ContainSubstring("create API key")))
		})
	})

	Describe("ValidateAPIKey", func() {
		It("returns the owner when the hash matches", func() {
			owner := &ent.User{ID: 1, Email: "a@x.com"}
			ak := &ent.ApiKey{ID: 1, Edges: ent.ApiKeyEdges{Owner: owner}}
			storeMock.FindAPIKeyByHash(ctx, mock.AnythingOfType("string")).
				Return(ak, nil).
				Once()

			got, err := svc.ValidateAPIKey(ctx, "raw-key")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(owner))
		})

		It("returns invalid when the lookup errors", func() {
			storeMock.FindAPIKeyByHash(ctx, mock.AnythingOfType("string")).
				Return(nil, errors.New("query fail")).Once()
			_, err := svc.ValidateAPIKey(ctx, "raw-key")
			Expect(err).To(MatchError("invalid API key"))
		})
	})

	Describe("ValidateToken malformed claims", func() {
		It("rejects a JWT signed with an unrelated secret", func() {
			// ParseWithClaims returns a non-nil error so the err != nil
			// branch fires. The signature won't match svc.jwtSecret.
			tampered := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9." +
				"eyJzdWIiOiJ4In0.invalid-signature"
			_, err := svc.ValidateToken(tampered)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("GetUserByID", func() {
		It("returns the user when the id resolves", func() {
			u := &ent.User{ID: 1, Email: "u@example.com"}
			storeMock.FindUserByID(ctx, uint32(1)).Return(u, nil).Once()
			got, err := svc.GetUserByID(ctx, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(u))
		})

		It("returns ErrUserNotFound when ent reports NotFound", func() {
			storeMock.FindUserByID(ctx, uint32(1)).
				Return(nil, &ent.NotFoundError{}).
				Once()
			_, err := svc.GetUserByID(ctx, 1)
			Expect(err).To(MatchError(ErrUserNotFound))
		})

		It("propagates non-NotFound errors", func() {
			storeMock.FindUserByID(ctx, uint32(1)).
				Return(nil, errors.New("load fail")).
				Once()
			_, err := svc.GetUserByID(ctx, 1)
			Expect(err).To(MatchError(ContainSubstring("load fail")))
		})
	})
})

var _ = Describe("Login lockout", Label("unit", "auth"), func() {
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
		Expect(err).NotTo(HaveOccurred())
		svc = m.(*auth)
	})

	It(
		"returns ErrAccountLocked without verifying password while LockedUntil is in the future",
		func() {
			future := time.Now().Add(10 * time.Minute)
			storeMock.FindUserByEmail(mock.Anything, "x@example.com").
				Return(&ent.User{
					ID:           1,
					Email:        "x@example.com",
					PasswordHash: "doesntmatter",
					LockedUntil:  &future,
				}, nil).Once()

			_, err := svc.Login(ctx, "x@example.com", "wrong", SessionMeta{})
			var locked ErrAccountLockedT
			Expect(errors.As(err, &locked)).To(BeTrue())
			Expect(locked.LockedUntil).To(BeTemporally("~", future, time.Second))
		},
	)

	It("auto-clears expired lockout on the next login", func() {
		past := time.Now().Add(-1 * time.Minute)
		hash, _ := bcrypt.GenerateFromPassword([]byte("p"), bcrypt.MinCost)
		u := &ent.User{
			ID:               1,
			Email:            "x@example.com",
			PasswordHash:     string(hash),
			LockedUntil:      &past,
			FailedLoginCount: 5,
		}
		storeMock.FindUserByEmail(mock.Anything, "x@example.com").
			Return(u, nil).
			Once()
		storeMock.UpdateUser(mock.Anything, uint32(1),
			mock.MatchedBy(func(p db.UpdateUserParams) bool {
				return p.FailedLoginCount != nil && *p.FailedLoginCount == 0 &&
					p.ClearLastFailedLoginAt && p.ClearLockedUntil
			})).Return(u, nil).Once()
		storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
			Return(&ent.Session{ID: 1}, nil).
			Once()

		_, err := svc.Login(ctx, "x@example.com", "p", SessionMeta{})
		Expect(err).NotTo(HaveOccurred())
	})

	It("locks the account on the threshold-reaching failed attempt", func() {
		// Configure a low threshold for this spec.
		configtest.Setup(map[string]any{
			"auth": map[string]any{
				"session_secret": "test-secret-key-for-jwt",
				"session_ttl":    "1h",
				"lockout": map[string]any{
					"threshold": 3,
					"window":    "15m",
					"duration":  "10m",
				},
			},
		})
		store := dbmocks.NewMockStore(GinkgoT())
		storeMock = store.EXPECT()
		m, err := New(store)
		Expect(err).NotTo(HaveOccurred())
		svc = m.(*auth)

		hash, _ := bcrypt.GenerateFromPassword([]byte("right"), bcrypt.MinCost)
		recent := time.Now().Add(-1 * time.Minute)
		u := &ent.User{
			ID:                1,
			Email:             "x@example.com",
			PasswordHash:      string(hash),
			FailedLoginCount:  2,
			LastFailedLoginAt: &recent,
		}
		storeMock.FindUserByEmail(mock.Anything, "x@example.com").
			Return(u, nil).
			Once()

		tx := dbmocks.NewMockTx(GinkgoT())
		storeMock.Tx(mock.Anything).Return(tx, nil).Once()
		tx.EXPECT().FindUserByID(mock.Anything, uint32(1)).Return(u, nil).Once()
		tx.EXPECT().UpdateUser(mock.Anything, uint32(1),
			mock.MatchedBy(func(p db.UpdateUserParams) bool {
				return p.FailedLoginCount != nil && *p.FailedLoginCount == 3 &&
					p.LockedUntil != nil && p.LastFailedLoginAt != nil
			})).Return(u, nil).Once()
		tx.EXPECT().Commit().Return(nil).Once()

		_, err = svc.Login(ctx, "x@example.com", "wrong", SessionMeta{})
		var locked ErrAccountLockedT
		Expect(errors.As(err, &locked)).To(BeTrue())
	})
})

var _ = Describe("New", Label("unit", "auth"), func() {
	It("returns an error when auth.session_ttl fails to parse", func() {
		configtest.Setup(map[string]any{
			"auth": map[string]any{
				"session_secret": "test-secret",
				"session_ttl":    "not-a-duration",
			},
		})
		_, err := New(dbmocks.NewMockStore(GinkgoT()))
		Expect(err).To(MatchError(ContainSubstring("parse auth.session_ttl")))
	})
})

// AuthService driver-level failure paths.
//
// A real in-memory SQLite succeeds at ent.Create almost unconditionally, so to
// cover the "DB insert blew up" branches we inject failures through a mocked
// driver.
var _ = Describe("AuthService driver-level failures", Label("unit", "auth"), func() {
	var (
		ctx  context.Context
		svc  *auth
		mock sqlmock.Sqlmock
	)

	BeforeEach(func() {
		ctx = context.Background()
		client, m := testutil.MockEntClient()
		mock = m
		DeferCleanup(func() { client.Close() })
		svc = newTestService(client)
		DeferCleanup(func() {
			Expect(mock.ExpectationsWereMet()).To(Succeed())
		})
	})

	It("Register returns create_failed when DB insert errors", func() {
		dbErr := errors.New("insert blew up")
		mock.ExpectQuery(`INSERT INTO .users.`).WillReturnError(dbErr)

		_, _, err := svc.Register(ctx, "a@x.com", "pw", "member", SessionMeta{})
		Expect(err).To(MatchError(ContainSubstring("create user")))
		Expect(err).To(MatchError(dbErr))
	})

	It("Login returns invalid credentials when the user query errors", func() {
		dbErr := errors.New("query blew up")
		mock.ExpectQuery(`SELECT .* FROM .users.`).WillReturnError(dbErr)

		_, err := svc.Login(ctx, "missing@x.com", "pw", SessionMeta{})
		Expect(err).To(MatchError("invalid credentials"))
	})

	It("CreateAPIKey surfaces the DB error", func() {
		dbErr := errors.New("apikey insert blew up")
		mock.ExpectQuery(`INSERT INTO .api_keys.`).WillReturnError(dbErr)

		_, _, err := svc.CreateAPIKey(ctx, 1, "test-key")
		Expect(err).To(MatchError(ContainSubstring("create API key")))
		Expect(err).To(MatchError(dbErr))
	})
})
