package auth

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
)

var _ = Describe("Unlock", Label("unit", "auth"), func() {
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

	Describe("Unlock by email", func() {
		It("clears lockout state on the matched user", func() {
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(&ent.User{ID: 7, Email: "u@x.com"}, nil).Once()
			storeMock.UpdateUser(mock.AnythingOfType(ctxType), uint32(7),
				mock.MatchedBy(func(p db.UpdateUserParams) bool {
					return p.FailedLoginCount != nil && *p.FailedLoginCount == 0 &&
						p.ClearLastFailedLoginAt && p.ClearLockedUntil
				})).
				Return(&ent.User{ID: 7}, nil).Once()

			Expect(svc.Unlock(ctx, "u@x.com", UnlockModeCLI)).To(Succeed())
		})

		It("returns ErrUserNotFound when the email is unknown", func() {
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "missing@x.com").
				Return(nil, &ent.NotFoundError{}).Once()

			err := svc.Unlock(ctx, "missing@x.com", UnlockModeCLI)
			Expect(err).To(MatchError(ErrUserNotFound))
		})

		It("wraps lookup errors", func() {
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "u@x.com").
				Return(nil, errors.New("boom")).Once()

			err := svc.Unlock(ctx, "u@x.com", UnlockModeCLI)
			Expect(err).To(MatchError(ContainSubstring("lookup user")))
		})
	})

	Describe("AdminUnlock by id", func() {
		It("clears lockout state without an email lookup", func() {
			storeMock.UpdateUser(mock.AnythingOfType(ctxType), uint32(42),
				mock.MatchedBy(func(p db.UpdateUserParams) bool {
					return p.FailedLoginCount != nil && *p.FailedLoginCount == 0 &&
						p.ClearLastFailedLoginAt && p.ClearLockedUntil
				})).
				Return(&ent.User{ID: 42}, nil).Once()

			Expect(svc.AdminUnlock(ctx, 42)).To(Succeed())
		})

		It("returns ErrUserNotFound when the id is unknown", func() {
			storeMock.UpdateUser(mock.AnythingOfType(ctxType), uint32(99),
				mock.AnythingOfType("db.UpdateUserParams")).
				Return(nil, &ent.NotFoundError{}).Once()

			err := svc.AdminUnlock(ctx, 99)
			Expect(err).To(MatchError(ErrUserNotFound))
		})
	})
})
