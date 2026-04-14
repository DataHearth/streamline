package auth

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
)

var _ = Describe("Admin service unit", Label("unit", "auth"), func() {
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

	Describe("ListUsers", func() {
		It("forwards filter and returns rows + total", func() {
			rows := []*ent.User{{ID: 1}}
			storeMock.ListUsers(mock.AnythingOfType(ctxType), mock.MatchedBy(func(p db.ListUsersParams) bool {
				return p.Q == "alice" && p.Role == user.RoleMember &&
					p.Limit == 50 && p.Offset == 10
			})).
				Return(rows, 7, nil).
				Once()

			got, total, err := svc.ListUsers(ctx, UserFilter{
				Q: "alice", Role: "member", Limit: 50, Offset: 10,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(rows))
			Expect(total).To(Equal(7))
		})

		It("wraps store errors", func() {
			storeMock.ListUsers(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.ListUsersParams")).
				Return(nil, 0, errors.New("list fail")).
				Once()
			_, _, err := svc.ListUsers(ctx, UserFilter{})
			Expect(err).To(MatchError(ContainSubstring("list users")))
		})
	})

	Describe("CreateUserDirect", func() {
		It("rejects weak passwords before any store call", func() {
			_, err := svc.CreateUserDirect(ctx, "a@x.com", "short", "member", "")
			Expect(err).To(MatchError(ErrPasswordWeak))
		})

		It("returns ErrUserEmailExists when the email is already taken", func() {
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "a@x.com").
				Return(&ent.User{ID: 1}, nil).Once()
			_, err := svc.CreateUserDirect(
				ctx,
				"A@X.COM",
				"password123",
				"member",
				"",
			)
			Expect(err).To(MatchError(ErrUserEmailExists))
		})

		It("creates the user when the email is free", func() {
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "a@x.com").
				Return(nil, &ent.NotFoundError{}).Once()
			storeMock.CreateUser(mock.AnythingOfType(ctxType), mock.MatchedBy(func(p db.CreateUserParams) bool {
				return p.Email == "a@x.com" && p.DisplayName == "Alice" &&
					p.Role == user.RoleMember && p.AuthMethod == user.AuthMethodLocal
			})).
				Return(&ent.User{ID: 7, Email: "a@x.com"}, nil).
				Once()

			got, err := svc.CreateUserDirect(
				ctx,
				"a@x.com",
				"password123",
				"member",
				"Alice",
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ID).To(Equal(uint32(7)))
		})

		It("wraps non-NotFound lookup errors", func() {
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "a@x.com").
				Return(nil, errors.New("lookup fail")).Once()
			_, err := svc.CreateUserDirect(
				ctx,
				"a@x.com",
				"password123",
				"member",
				"",
			)
			Expect(err).To(MatchError(ContainSubstring("lookup email")))
		})

		It("wraps create errors", func() {
			storeMock.FindUserByEmail(mock.AnythingOfType(ctxType), "a@x.com").
				Return(nil, &ent.NotFoundError{}).Once()
			storeMock.CreateUser(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateUserParams")).
				Return(nil, errors.New("create fail")).
				Once()
			_, err := svc.CreateUserDirect(
				ctx,
				"a@x.com",
				"password123",
				"member",
				"",
			)
			Expect(err).To(MatchError(ContainSubstring("create user")))
		})
	})

	Describe("GetUserDetail", func() {
		It("aggregates user, keys, and sessions", func() {
			u := &ent.User{ID: 1}
			keys := []*ent.ApiKey{{ID: 1}}
			sessions := []*ent.Session{{ID: 1}}
			storeMock.FindUserByID(ctx, uint32(1)).Return(u, nil).Once()
			storeMock.ListAPIKeysByUser(ctx, uint32(1)).Return(keys, nil).Once()
			storeMock.ListUserSessions(ctx, uint32(1)).Return(sessions, nil).Once()

			gotU, gotKeys, gotSessions, err := svc.GetUserDetail(ctx, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(gotU).To(Equal(u))
			Expect(gotKeys).To(Equal(keys))
			Expect(gotSessions).To(Equal(sessions))
		})

		It("returns ErrUserNotFound when ent reports NotFound", func() {
			storeMock.FindUserByID(ctx, uint32(1)).
				Return(nil, &ent.NotFoundError{}).
				Once()
			_, _, _, err := svc.GetUserDetail(ctx, 1)
			Expect(err).To(MatchError(ErrUserNotFound))
		})

		It("propagates non-NotFound load errors", func() {
			storeMock.FindUserByID(ctx, uint32(1)).
				Return(nil, errors.New("load fail")).
				Once()
			_, _, _, err := svc.GetUserDetail(ctx, 1)
			Expect(err).To(MatchError(ContainSubstring("load fail")))
		})

		It("propagates ListAPIKeysByUser errors", func() {
			storeMock.FindUserByID(ctx, uint32(1)).
				Return(&ent.User{ID: 1}, nil).
				Once()
			storeMock.ListAPIKeysByUser(ctx, uint32(1)).
				Return(nil, errors.New("keys fail")).
				Once()
			_, _, _, err := svc.GetUserDetail(ctx, 1)
			Expect(err).To(MatchError("keys fail"))
		})

		It("propagates ListUserSessions errors", func() {
			storeMock.FindUserByID(ctx, uint32(1)).
				Return(&ent.User{ID: 1}, nil).
				Once()
			storeMock.ListAPIKeysByUser(ctx, uint32(1)).Return(nil, nil).Once()
			storeMock.ListUserSessions(ctx, uint32(1)).
				Return(nil, errors.New("sess fail")).
				Once()
			_, _, _, err := svc.GetUserDetail(ctx, 1)
			Expect(err).To(MatchError("sess fail"))
		})
	})

	Describe("UpdateUser", func() {
		It("returns ErrUserNotFound when the id does not resolve", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(nil, &ent.NotFoundError{}).Once()
			err := svc.UpdateUser(ctx, 1, UserPatch{})
			Expect(err).To(MatchError(ErrUserNotFound))
		})

		It("wraps non-NotFound load errors", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(nil, errors.New("load fail")).Once()
			err := svc.UpdateUser(ctx, 1, UserPatch{})
			Expect(err).To(MatchError(ContainSubstring("load user")))
		})

		It("returns ErrLastAdmin when demoting the only admin", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1, Role: user.RoleAdmin}, nil).Once()
			storeMock.CountUsersByRole(mock.AnythingOfType(ctxType), user.RoleAdmin).
				Return(1, nil).Once()

			memberRole := "member"
			err := svc.UpdateUser(ctx, 1, UserPatch{Role: &memberRole})
			Expect(err).To(MatchError(ErrLastAdmin))
		})

		It("wraps CountUsersByRole errors", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1, Role: user.RoleAdmin}, nil).Once()
			storeMock.CountUsersByRole(mock.AnythingOfType(ctxType), user.RoleAdmin).
				Return(0, errors.New("count fail")).Once()

			memberRole := "member"
			err := svc.UpdateUser(ctx, 1, UserPatch{Role: &memberRole})
			Expect(err).To(MatchError(ContainSubstring("count admins")))
		})

		It("permits demotion when other admins exist", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1, Role: user.RoleAdmin}, nil).Once()
			storeMock.CountUsersByRole(mock.AnythingOfType(ctxType), user.RoleAdmin).
				Return(2, nil).Once()
			storeMock.UpdateUser(mock.AnythingOfType(ctxType), uint32(1), mock.MatchedBy(func(p db.UpdateUserParams) bool {
				return p.Role != nil && *p.Role == user.RoleMember
			})).
				Return(&ent.User{ID: 1, Role: user.RoleMember}, nil).
				Once()

			memberRole := "member"
			err := svc.UpdateUser(ctx, 1, UserPatch{Role: &memberRole})
			Expect(err).NotTo(HaveOccurred())
		})

		It("applies every non-nil patch field", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1, Role: user.RoleMember}, nil).Once()
			storeMock.UpdateUser(mock.AnythingOfType(ctxType), uint32(1), mock.MatchedBy(func(p db.UpdateUserParams) bool {
				return p.AuthMethod != nil && *p.AuthMethod == user.AuthMethodBoth &&
					p.DisplayName != nil && *p.DisplayName == "Alice"
			})).
				Return(&ent.User{ID: 1}, nil).
				Once()

			authMethod := "both"
			displayName := "  Alice  "
			err := svc.UpdateUser(ctx, 1, UserPatch{
				AuthMethod:  &authMethod,
				DisplayName: &displayName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("wraps UpdateUser store errors", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1, Role: user.RoleMember}, nil).Once()
			storeMock.UpdateUser(mock.AnythingOfType(ctxType), uint32(1), mock.AnythingOfType("db.UpdateUserParams")).
				Return(nil, errors.New("update fail")).
				Once()
			err := svc.UpdateUser(ctx, 1, UserPatch{})
			Expect(err).To(MatchError(ContainSubstring("update user")))
		})
	})

	Describe("DeleteUser", func() {
		It("returns ErrSelfDeleteForbidden when id == requesterID", func() {
			Expect(svc.DeleteUser(ctx, 1, 1)).To(MatchError(ErrSelfDeleteForbidden))
		})

		It("returns ErrUserNotFound when the id does not resolve", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(nil, &ent.NotFoundError{}).Once()
			Expect(svc.DeleteUser(ctx, 1, 2)).To(MatchError(ErrUserNotFound))
		})

		It("wraps non-NotFound load errors", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(nil, errors.New("load fail")).Once()
			Expect(
				svc.DeleteUser(ctx, 1, 2),
			).To(MatchError(ContainSubstring("load user")))
		})

		It("returns ErrLastAdmin when deleting the only admin", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1, Role: user.RoleAdmin}, nil).Once()
			storeMock.CountUsersByRole(mock.AnythingOfType(ctxType), user.RoleAdmin).
				Return(1, nil).Once()
			Expect(svc.DeleteUser(ctx, 1, 2)).To(MatchError(ErrLastAdmin))
		})

		It("wraps CountUsersByRole errors", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1, Role: user.RoleAdmin}, nil).Once()
			storeMock.CountUsersByRole(mock.AnythingOfType(ctxType), user.RoleAdmin).
				Return(0, errors.New("count fail")).Once()
			Expect(
				svc.DeleteUser(ctx, 1, 2),
			).To(MatchError(ContainSubstring("count admins")))
		})

		It("deletes the user when no invariants block", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1, Role: user.RoleMember}, nil).Once()
			storeMock.DeleteUser(mock.AnythingOfType(ctxType), uint32(1)).
				Return(nil).
				Once()
			Expect(svc.DeleteUser(ctx, 1, 2)).To(Succeed())
		})

		It("wraps store delete errors", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1, Role: user.RoleMember}, nil).Once()
			storeMock.DeleteUser(mock.AnythingOfType(ctxType), uint32(1)).
				Return(errors.New("delete fail")).Once()
			Expect(
				svc.DeleteUser(ctx, 1, 2),
			).To(MatchError(ContainSubstring("delete user")))
		})
	})

	Describe("AdminResetPassword", func() {
		It("rejects weak passwords before any store call", func() {
			Expect(
				svc.AdminResetPassword(ctx, 1, "short"),
			).To(MatchError(ErrPasswordWeak))
		})

		It("returns ErrUserNotFound when the id does not resolve", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(nil, &ent.NotFoundError{}).Once()
			Expect(svc.AdminResetPassword(ctx, 1, "newpassword123")).
				To(MatchError(ErrUserNotFound))
		})

		It("wraps non-NotFound load errors", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(nil, errors.New("load fail")).Once()
			Expect(svc.AdminResetPassword(ctx, 1, "newpassword123")).
				To(MatchError(ContainSubstring("load user")))
		})

		It("rotates the password and revokes sessions on success", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1}, nil).Once()
			storeMock.UpdateUserPassword(mock.AnythingOfType(ctxType), uint32(1), mock.AnythingOfType("string")).
				Return(nil).
				Once()
			storeMock.RevokeAllUserSessions(mock.AnythingOfType(ctxType), uint32(1), mock.AnythingOfType("time.Time")).
				Return(nil).
				Once()
			Expect(svc.AdminResetPassword(ctx, 1, "newpassword123")).To(Succeed())
		})

		It("wraps UpdateUserPassword errors", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1}, nil).Once()
			storeMock.UpdateUserPassword(mock.AnythingOfType(ctxType), uint32(1), mock.AnythingOfType("string")).
				Return(errors.New("upd fail")).
				Once()
			Expect(svc.AdminResetPassword(ctx, 1, "newpassword123")).
				To(MatchError(ContainSubstring("update password")))
		})

		It("succeeds when RevokeAllUserSessions fails (best-effort)", func() {
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1}, nil).Once()
			storeMock.UpdateUserPassword(mock.AnythingOfType(ctxType), uint32(1), mock.AnythingOfType("string")).
				Return(nil).
				Once()
			storeMock.RevokeAllUserSessions(mock.AnythingOfType(ctxType), uint32(1), mock.AnythingOfType("time.Time")).
				Return(errors.New("rev fail")).
				Once()
			Expect(svc.AdminResetPassword(ctx, 1, "newpassword123")).To(Succeed())
		})
	})

	Describe("AdminRevokeAPIKey", func() {
		It("delegates to RevokeAPIKeyByID", func() {
			storeMock.DeleteAPIKeyByID(mock.AnythingOfType(ctxType), uint32(1), uint32(2)).
				Return(1, nil).
				Once()
			Expect(svc.AdminRevokeAPIKey(ctx, 1, 2)).To(Succeed())
		})
	})

	Describe("AdminRevokeSession", func() {
		It("delegates to RevokeSessionByID", func() {
			storeMock.RevokeUserSessionByID(mock.AnythingOfType(ctxType), uint32(1), uint32(2), mock.AnythingOfType("time.Time")).
				Return(1, nil).
				Once()
			Expect(svc.AdminRevokeSession(ctx, 1, 2)).To(Succeed())
		})
	})
})
