package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
	"golang.org/x/crypto/bcrypt"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/otelx"
)

var _ = Describe("Account service unit", Label("unit", "auth"), func() {
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

	Describe("UpdateProfile", func() {
		It("trims display name and forwards to UpdateUser", func() {
			row := &ent.User{ID: 1, DisplayName: "Alice"}
			storeMock.UpdateUser(mock.AnythingOfType(ctxType), uint32(1), mock.MatchedBy(func(p db.UpdateUserParams) bool {
				return p.DisplayName != nil && *p.DisplayName == "Alice"
			})).
				Return(row, nil).
				Once()

			got, err := svc.UpdateProfile(ctx, 1, "  Alice  ")
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(row))
		})

		It("wraps store errors", func() {
			storeMock.UpdateUser(mock.AnythingOfType(ctxType), uint32(1), mock.AnythingOfType("db.UpdateUserParams")).
				Return(nil, errors.New("update fail")).
				Once()
			_, err := svc.UpdateProfile(ctx, 1, "x")
			Expect(err).To(MatchError(ContainSubstring("update profile")))
		})
	})

	Describe("ChangePassword", func() {
		hash, _ := bcrypt.GenerateFromPassword([]byte("oldpassword"), bcrypt.MinCost)

		It(
			"updates password, revokes other sessions and deletes API keys on success",
			func() {
				storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
					Return(&ent.User{ID: 1, PasswordHash: string(hash)}, nil).Once()
				storeMock.UpdateUserPassword(mock.AnythingOfType(ctxType), uint32(1), mock.AnythingOfType("string")).
					Return(nil).
					Once()
				storeMock.RevokeOtherUserSessions(mock.AnythingOfType(ctxType), uint32(1), "keep", mock.AnythingOfType("time.Time")).
					Return(nil).
					Once()
				storeMock.DeleteAPIKeysByUser(mock.AnythingOfType(ctxType), uint32(1)).
					Return(2, nil).
					Once()

				Expect(
					svc.ChangePassword(
						ctx,
						1,
						"oldpassword",
						"newpassw0rd!",
						"keep",
					),
				).
					To(Succeed())
			},
		)

		It("succeeds when the API-key delete fails (best-effort)", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1, PasswordHash: string(hash)}, nil).Once()
			storeMock.UpdateUserPassword(mock.AnythingOfType(ctxType), uint32(1), mock.AnythingOfType("string")).
				Return(nil).
				Once()
			storeMock.RevokeOtherUserSessions(mock.AnythingOfType(ctxType), uint32(1), "keep", mock.AnythingOfType("time.Time")).
				Return(nil).
				Once()
			storeMock.DeleteAPIKeysByUser(mock.AnythingOfType(ctxType), uint32(1)).
				Return(0, errors.New("del fail")).
				Once()

			Expect(
				svc.ChangePassword(ctx, 1, "oldpassword", "newpassw0rd!", "keep"),
			).
				To(Succeed())
		})

		It("returns ErrPasswordInvalid for OIDC-only accounts", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1, PasswordHash: ""}, nil).Once()
			Expect(svc.ChangePassword(ctx, 1, "x", "newpassw0rd!", "keep")).
				To(MatchError(ErrPasswordInvalid))
		})

		It("returns ErrPasswordInvalid when the current password is wrong", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1, PasswordHash: string(hash)}, nil).Once()
			Expect(svc.ChangePassword(ctx, 1, "wrong", "newpassw0rd!", "keep")).
				To(MatchError(ErrPasswordInvalid))
		})

		It("returns ErrPasswordWeak when the new password is too short", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1, PasswordHash: string(hash)}, nil).Once()
			Expect(svc.ChangePassword(ctx, 1, "oldpassword", "short", "keep")).
				To(MatchError(ErrPasswordWeak))
		})

		It("wraps store errors from FindUserByID", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(nil, errors.New("find fail")).Once()
			Expect(svc.ChangePassword(ctx, 1, "x", "newpassw0rd!", "keep")).
				To(MatchError(ContainSubstring("load user")))
		})

		It("wraps store errors from UpdateUserPassword", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1, PasswordHash: string(hash)}, nil).Once()
			storeMock.UpdateUserPassword(mock.AnythingOfType(ctxType), uint32(1), mock.AnythingOfType("string")).
				Return(errors.New("upd fail")).
				Once()
			Expect(
				svc.ChangePassword(ctx, 1, "oldpassword", "newpassw0rd!", "keep"),
			).
				To(MatchError(ContainSubstring("update password")))
		})

		// The ceiling belongs to the comparison, not to POST /auth/login. This
		// endpoint authenticates on a session or an API key, and the restored
		// backup that carries a costlier hash carries those with it, so the
		// same stored hash arrives here — with no per-IP limiter in front of
		// /api/v1 to slow a repeat.
		Describe("the maxUsableHashCost ceiling", func() {
			// atCost rewrites the two cost digits and leaves the salt intact,
			// so the compare really would run 1<<c key expansions. Generating a
			// cost-31 hash is not reachable in a test's lifetime, and only the
			// expansions the compare is asked for matter here.
			atCost := func(hash string, c int) string {
				return hash[:4] + fmt.Sprintf("%02d", c) + hash[6:]
			}

			wellFormed := string(otelx.Must(bcrypt.GenerateFromPassword(
				[]byte("legacy"), bcrypt.DefaultCost,
			)))

			// A costlier hash must fail for the password it was actually made
			// from, or the account walks the ceiling off by rotating its own
			// password: before this comparison went through comparePassword,
			// this call returned 204 and rewrote the row at the default cost,
			// which is the one thing the ceiling exists to prevent. Leaving
			// UpdateUserPassword unmocked is the assertion that it never runs.
			It(
				"refuses a hash costlier than the ceiling, right password included",
				func() {
					hash := string(otelx.Must(bcrypt.GenerateFromPassword(
						[]byte("legacy"), maxUsableHashCost+1,
					)))
					storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
						Return(&ent.User{ID: 1, PasswordHash: hash}, nil).Once()

					Expect(
						svc.ChangePassword(ctx, 1, "legacy", "newpassw0rd!", "keep"),
					).
						To(MatchError(ErrPasswordInvalid))
				},
			)

			// FlakeAttempts for the same reason the login table carries it: the
			// bound is wall-clock, and a spec that loses its core to something
			// else on the machine reads slow through no fault of the code. The
			// regression this catches is 15x and up.
			DescribeTable(
				"answers in one default-cost comparison whatever the stored hash costs",
				FlakeAttempts(3),
				func(stored string) {
					start := time.Now()
					_ = bcrypt.CompareHashAndPassword(
						dummyPasswordHash,
						[]byte("nope"),
					)
					reference := time.Since(start)

					storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
						Return(&ent.User{ID: 1, PasswordHash: stored}, nil).Once()

					start = time.Now()
					err := svc.ChangePassword(
						ctx,
						1,
						"legacy",
						"newpassw0rd!",
						"keep",
					)
					elapsed := time.Since(start)

					Expect(err).To(MatchError(ErrPasswordInvalid))
					Expect(elapsed).To(BeNumerically("<", 4*reference))
				},
				// 15x the reference before the fix.
				Entry("cost 14 over a valid salt", atCost(wellFormed, 14)),
				// 2^31 expansions: this one never answered at all, and held a
				// core at 100% long after the client had given up.
				Entry(
					"cost 31 over a valid salt",
					atCost(wellFormed, bcrypt.MaxCost),
				),
			)
		})

		It("succeeds when RevokeOtherSessions fails (best-effort)", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1, PasswordHash: string(hash)}, nil).Once()
			storeMock.UpdateUserPassword(mock.AnythingOfType(ctxType), uint32(1), mock.AnythingOfType("string")).
				Return(nil).
				Once()
			storeMock.RevokeOtherUserSessions(mock.AnythingOfType(ctxType), uint32(1), "keep", mock.AnythingOfType("time.Time")).
				Return(errors.New("rev fail")).
				Once()
			storeMock.DeleteAPIKeysByUser(mock.AnythingOfType(ctxType), uint32(1)).
				Return(0, nil).
				Once()

			Expect(
				svc.ChangePassword(ctx, 1, "oldpassword", "newpassw0rd!", "keep"),
			).
				To(Succeed())
		})
	})

	Describe("ListAPIKeys", func() {
		It("delegates to the store", func() {
			rows := []*ent.ApiKey{{ID: 1}}
			storeMock.ListAPIKeysByUser(ctx, uint32(1)).Return(rows, nil).Once()
			got, err := svc.ListAPIKeys(ctx, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(rows))
		})
	})

	Describe("RevokeAPIKeyByID", func() {
		It("returns nil when one row was deleted", func() {
			storeMock.DeleteAPIKeyByID(mock.AnythingOfType(ctxType), uint32(1), uint32(2)).
				Return(1, nil).
				Once()
			Expect(svc.RevokeAPIKeyByID(ctx, 1, 2)).To(Succeed())
		})

		It("returns ErrAPIKeyNotFound when no row was deleted", func() {
			storeMock.DeleteAPIKeyByID(mock.AnythingOfType(ctxType), uint32(1), uint32(2)).
				Return(0, nil).
				Once()
			Expect(svc.RevokeAPIKeyByID(ctx, 1, 2)).
				To(MatchError(ErrAPIKeyNotFound))
		})

		It("wraps store errors", func() {
			storeMock.DeleteAPIKeyByID(mock.AnythingOfType(ctxType), uint32(1), uint32(2)).
				Return(0, errors.New("delete fail")).
				Once()
			Expect(svc.RevokeAPIKeyByID(ctx, 1, 2)).
				To(MatchError(ContainSubstring("revoke api key")))
		})
	})
})
