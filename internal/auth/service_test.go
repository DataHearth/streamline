package auth

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/datahearth/streamline/internal/otelx"
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
					p.Role.String() == string(entuser.RoleAdmin) &&
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
		It(
			"returns the owner and stamps last_used_at when the hash matches",
			func() {
				owner := &ent.User{ID: 1, Email: "a@x.com"}
				ak := &ent.ApiKey{ID: 7, Edges: ent.ApiKeyEdges{Owner: owner}}
				storeMock.FindAPIKeyByHash(ctx, mock.AnythingOfType("string")).
					Return(ak, nil).
					Once()
				storeMock.TouchAPIKey(
					ctx,
					uint32(7),
					mock.AnythingOfType("time.Time"),
				).Return(nil).Once()

				got, err := svc.ValidateAPIKey(ctx, "raw-key")
				Expect(err).NotTo(HaveOccurred())
				Expect(got).To(Equal(owner))
			},
		)

		It("still authenticates when the usage stamp fails", func() {
			owner := &ent.User{ID: 1, Email: "a@x.com"}
			ak := &ent.ApiKey{ID: 7, Edges: ent.ApiKeyEdges{Owner: owner}}
			storeMock.FindAPIKeyByHash(ctx, mock.AnythingOfType("string")).
				Return(ak, nil).
				Once()
			storeMock.TouchAPIKey(
				ctx,
				uint32(7),
				mock.AnythingOfType("time.Time"),
			).Return(errors.New("db down")).Once()

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

var _ = Describe("Login enumeration resistance", Label("unit", "auth"), func() {
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

	// bcryptCost times one real comparison so the assertions below scale with
	// whatever cost the build uses instead of a hardcoded floor. Every bound is
	// a lower one: a loaded machine only ever makes bcrypt slower.
	bcryptCost := func() time.Duration {
		start := time.Now()
		_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte("nope"))
		return time.Since(start)
	}

	It("spends a password comparison on an email that does not exist", func() {
		reference := bcryptCost()
		storeMock.FindUserByEmail(mock.Anything, "ghost@example.com").
			Return(nil, &ent.NotFoundError{}).Once()

		start := time.Now()
		_, err := svc.Login(ctx, "ghost@example.com", "hunter2", SessionMeta{})
		elapsed := time.Since(start)

		Expect(err).To(MatchError("invalid credentials"))
		Expect(elapsed).To(BeNumerically(">", reference/2))
	})

	It("spends a password comparison on a locked account", func() {
		reference := bcryptCost()
		future := time.Now().Add(10 * time.Minute)
		storeMock.FindUserByEmail(mock.Anything, "locked@example.com").
			Return(&ent.User{
				ID:           1,
				Email:        "locked@example.com",
				PasswordHash: "irrelevant",
				LockedUntil:  &future,
			}, nil).Once()

		start := time.Now()
		_, err := svc.Login(ctx, "locked@example.com", "hunter2", SessionMeta{})
		elapsed := time.Since(start)

		var locked ErrAccountLockedT
		Expect(errors.As(err, &locked)).To(BeTrue())
		Expect(elapsed).To(BeNumerically(">", reference/2))
	})

	// expectFailedAttempt mocks the transaction Login runs after a mismatch, so
	// the specs below cost what a wrong password costs end to end.
	expectFailedAttempt := func(id uint32) {
		GinkgoHelper()
		tx := dbmocks.NewMockTx(GinkgoT())
		storeMock.Tx(mock.Anything).Return(tx, nil).Once()
		tx.EXPECT().FindUserByID(mock.Anything, id).
			Return(&ent.User{ID: id}, nil).Once()
		tx.EXPECT().UpdateUser(mock.Anything, id,
			mock.AnythingOfType("db.UpdateUserParams")).
			Return(&ent.User{ID: id}, nil).Once()
		tx.EXPECT().Commit().Return(nil).Once()
	}

	// wellFormed is a genuine default-cost hash, spliced below into the shapes
	// bcrypt parses but cannot finish.
	wellFormed := string(otelx.Must(bcrypt.GenerateFromPassword(
		[]byte("some other password"), bcrypt.DefaultCost,
	)))

	// encodedSaltLen is the base64 salt window bcrypt writes between the cost
	// digits and the hash tail. x/crypto keeps the constant unexported.
	const encodedSaltLen = 22

	// corruptSalt puts a byte outside bcrypt's ./A-Za-z0-9 salt alphabet at
	// index idx of the salt window, which starts at offset 7,
	// right after "$2a$10$". bcrypt.Cost never looks at the salt, so the hash
	// still reports its cost; base64Decode inside expensiveBlowfishSetup
	// rejects it before the 1<<cost rounds run.
	corruptSalt := func(hash string, idx int) string {
		GinkgoHelper()
		Expect(len(hash)).To(BeNumerically(">", 7+idx))
		return hash[:7+idx] + "!" + hash[8+idx:]
	}

	// atCost rewrites the two cost digits and leaves the salt intact, so the
	// compare really would run 1<<c key expansions. Generating a cost-14 hash
	// for real costs 0.7s and a cost-31 one is not reachable in a test's
	// lifetime; only the expansions the compare is asked for matter here, and
	// the tail no longer matching its own salt does not change how many.
	atCost := func(hash string, c int) string {
		return hash[:4] + fmt.Sprintf("%02d", c) + hash[6:]
	}

	// FlakeAttempts because both bounds below are wall-clock: a login that loses
	// its core to something else on the machine reads slow through no fault of
	// the code, and one such reading was seen while writing this. Retrying
	// re-times the reference too, so a busy machine costs a repeat rather than a
	// wider band. The regressions this is here for are 16x and up and fail every
	// attempt.
	DescribeTable(
		"answers for one default-cost comparison whatever the stored hash costs",
		FlakeAttempts(3),
		func(stored string) {
			reference := bcryptCost()
			storeMock.FindUserByEmail(mock.Anything, "sso@example.com").
				Return(&ent.User{
					ID:           7,
					Email:        "sso@example.com",
					PasswordHash: stored,
				}, nil).Once()
			expectFailedAttempt(7)

			start := time.Now()
			_, err := svc.Login(ctx, "sso@example.com", "hunter2", SessionMeta{})
			elapsed := time.Since(start)

			Expect(err).To(MatchError("invalid credentials"))
			Expect(elapsed).To(BeNumerically(">", reference/2))
			// The ceiling half: a hash the app cannot pad down to the floor has
			// to be refused, not run. Four times the reference is the widest
			// band that still catches cost 14 and cost 31. It is too wide to
			// catch the boundary, which is only twice the floor — that one is
			// pinned by the refusal spec below, which needs no clock.
			Expect(elapsed).To(BeNumerically("<", 4*reference))
		},
		// An SSO-only account: ent's password_hash is Optional and the OIDC
		// login path creates users without one.
		Entry("no password at all", ""),
		Entry("truncated bcrypt hash", "$2a$10$abcdefghijklmnop"),
		Entry("not bcrypt at all", "plaintext-password"),
		Entry(
			"bcrypt hash cheaper than the default cost",
			string(otelx.Must(bcrypt.GenerateFromPassword(
				[]byte("some other password"), bcrypt.MinCost,
			))),
		),
		// The three below all satisfy bcrypt.Cost and fail inside the compare,
		// so nothing gated on the hash prefix sees them coming.
		Entry(
			"well-formed prefix over an unparseable salt",
			corruptSalt(wellFormed, 0),
		),
		Entry(
			"unparseable byte at the very end of the salt window",
			corruptSalt(wellFormed, encodedSaltLen-1),
		),
		Entry(
			"cost 31 over an unparseable salt",
			"$2a$31$"+corruptSalt(wellFormed, 0)[7:],
		),
		// The three below have valid salts, so the compare would run every one
		// of the expansions their header asks for. Nothing tops those down.
		Entry(
			"one cost above the ceiling, over a valid salt",
			atCost(wellFormed, maxUsableHashCost+1),
		),
		Entry(
			"cost 14 over a valid salt",
			atCost(wellFormed, 14),
		),
		// 2^31 expansions is days of CPU inside one unauthenticated request.
		Entry(
			"cost 31 over a valid salt",
			atCost(wellFormed, bcrypt.MaxCost),
		),
	)

	// The ceiling is a refusal, not a slow path: the account stops
	// authenticating even for the password its hash was made from, exactly like
	// an SSO account with no hash at all.
	It("refuses a hash costlier than the ceiling, right password included", func() {
		hash := string(otelx.Must(bcrypt.GenerateFromPassword(
			[]byte("legacy"), maxUsableHashCost+1,
		)))

		Expect(comparePassword(ctx, hash, "legacy")).
			To(MatchError(errHashCostAboveCeiling))
	})

	It("still authenticates a hash written at a lower cost", func() {
		hash := otelx.Must(
			bcrypt.GenerateFromPassword([]byte("legacy"), bcrypt.MinCost),
		)
		storeMock.FindUserByEmail(mock.Anything, "old@example.com").
			Return(&ent.User{
				ID:           8,
				Email:        "old@example.com",
				PasswordHash: string(hash),
			}, nil).Once()
		storeMock.CreateSession(mock.Anything,
			mock.AnythingOfType("db.CreateSessionParams")).
			Return(&ent.Session{ID: 1}, nil).Once()

		token, err := svc.Login(ctx, "old@example.com", "legacy", SessionMeta{})
		Expect(err).NotTo(HaveOccurred())
		Expect(token).NotTo(BeEmpty())
	})

	It("hashes the dummy at the same cost a stored password uses", func() {
		cost, err := bcrypt.Cost(dummyPasswordHash)
		Expect(err).NotTo(HaveOccurred())
		Expect(cost).To(Equal(bcrypt.DefaultCost))
	})

	// spendCostShortfall's arithmetic only holds if each spliced dummy really
	// runs the cost it is filed under. bcrypt.Cost alone cannot say so — it
	// reports the header of a hash that dies in base64Decode before a single
	// round, which is the hole this whole area was fixed for. Only a compare
	// that comes back ErrMismatchedHashAndPassword proves the rounds ran, and
	// only then does the header it ran under mean anything.
	It("files a lowered-cost dummy under the cost it actually runs", func() {
		for c := bcrypt.MinCost; c < bcrypt.DefaultCost; c++ {
			Expect(bcrypt.CompareHashAndPassword(
				loweredCostHashes[c], []byte("nope"),
			)).To(MatchError(bcrypt.ErrMismatchedHashAndPassword))
			Expect(bcrypt.Cost(loweredCostHashes[c])).To(Equal(c))
		}
	})

	It("cannot be matched by a supplied password", func() {
		Expect(
			bcrypt.CompareHashAndPassword(dummyPasswordHash, nil),
		).To(HaveOccurred())
		Expect(bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(""))).
			To(HaveOccurred())
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
