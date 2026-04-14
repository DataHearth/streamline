package auth

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"
	"golang.org/x/crypto/bcrypt"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
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

		It("updates password and revokes other sessions on success", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1, PasswordHash: string(hash)}, nil).Once()
			storeMock.UpdateUserPassword(mock.AnythingOfType(ctxType), uint32(1), mock.AnythingOfType("string")).
				Return(nil).
				Once()
			storeMock.RevokeOtherUserSessions(mock.AnythingOfType(ctxType), uint32(1), "keep", mock.AnythingOfType("time.Time")).
				Return(nil).
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

		It("succeeds when RevokeOtherSessions fails (best-effort)", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1, PasswordHash: string(hash)}, nil).Once()
			storeMock.UpdateUserPassword(mock.AnythingOfType(ctxType), uint32(1), mock.AnythingOfType("string")).
				Return(nil).
				Once()
			storeMock.RevokeOtherUserSessions(mock.AnythingOfType(ctxType), uint32(1), "keep", mock.AnythingOfType("time.Time")).
				Return(errors.New("rev fail")).
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
