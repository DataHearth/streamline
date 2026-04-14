package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/invite"
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
)

var _ = Describe("Invite service unit", Label("unit", "auth"), func() {
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

	Describe("CreateInvite", func() {
		It("hashes the token, lowercases email, and persists", func() {
			storeMock.CreateInvite(ctx, mock.MatchedBy(func(p db.CreateInviteParams) bool {
				return p.Email == "guest@x.com" && p.Role == invite.RoleMember &&
					p.CreatedByID == 7 && p.TokenHash != ""
			})).
				Return(&ent.Invite{ID: 1, Email: "guest@x.com"}, nil).
				Once()

			raw, inv, err := svc.CreateInvite(
				ctx,
				7,
				"Guest@X.com",
				"member",
				time.Hour,
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(raw).NotTo(BeEmpty())
			Expect(inv.Email).To(Equal("guest@x.com"))
		})

		It("wraps store errors", func() {
			storeMock.CreateInvite(ctx, mock.AnythingOfType("db.CreateInviteParams")).
				Return(nil, errors.New("insert fail")).
				Once()
			_, _, err := svc.CreateInvite(ctx, 7, "g@x.com", "member", time.Hour)
			Expect(err).To(MatchError(ContainSubstring("store invite")))
		})
	})

	Describe("LookupInviteForPrefill", func() {
		It("returns the invite when valid", func() {
			inv := &ent.Invite{
				ID:        1,
				Email:     "g@x.com",
				ExpiresAt: time.Now().Add(time.Hour),
			}
			storeMock.FindInviteByTokenHash(ctx, mock.AnythingOfType("string")).
				Return(inv, nil).Once()
			got, err := svc.LookupInviteForPrefill(ctx, "raw")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.ID).To(Equal(uint32(1)))
		})

		It("returns ErrInviteInvalid when token not found", func() {
			storeMock.FindInviteByTokenHash(ctx, mock.AnythingOfType("string")).
				Return(nil, &ent.NotFoundError{}).Once()
			_, err := svc.LookupInviteForPrefill(ctx, "raw")
			Expect(err).To(MatchError(ErrInviteInvalid))
		})

		It("returns ErrInviteInvalid when used", func() {
			now := time.Now()
			inv := &ent.Invite{ID: 1, UsedAt: &now, ExpiresAt: now.Add(time.Hour)}
			storeMock.FindInviteByTokenHash(ctx, mock.AnythingOfType("string")).
				Return(inv, nil).Once()
			_, err := svc.LookupInviteForPrefill(ctx, "raw")
			Expect(err).To(MatchError(ErrInviteInvalid))
		})

		It("returns ErrInviteInvalid when expired", func() {
			inv := &ent.Invite{ID: 1, ExpiresAt: time.Now().Add(-time.Hour)}
			storeMock.FindInviteByTokenHash(ctx, mock.AnythingOfType("string")).
				Return(inv, nil).Once()
			_, err := svc.LookupInviteForPrefill(ctx, "raw")
			Expect(err).To(MatchError(ErrInviteInvalid))
		})

		It("does not check email field", func() {
			inv := &ent.Invite{
				ID:        1,
				Email:     "bound@x.com",
				ExpiresAt: time.Now().Add(time.Hour),
			}
			storeMock.FindInviteByTokenHash(ctx, mock.AnythingOfType("string")).
				Return(inv, nil).Once()
			got, err := svc.LookupInviteForPrefill(ctx, "raw")
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Email).To(Equal("bound@x.com"))
		})
	})

	Describe("ListInvites", func() {
		It("delegates to the store", func() {
			rows := []*ent.Invite{{ID: 1}}
			storeMock.ListInvites(ctx).Return(rows, nil).Once()
			got, err := svc.ListInvites(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(rows))
		})
	})

	Describe("RevokeInvite", func() {
		It("delegates to the store", func() {
			storeMock.RevokeInvite(ctx, uint32(1), mock.AnythingOfType("time.Time")).
				Return(nil).
				Once()
			Expect(svc.RevokeInvite(ctx, 1)).To(Succeed())
		})

		It("propagates store errors", func() {
			storeMock.RevokeInvite(ctx, uint32(1), mock.AnythingOfType("time.Time")).
				Return(errors.New("revoke fail")).Once()
			Expect(svc.RevokeInvite(ctx, 1)).To(MatchError("revoke fail"))
		})
	})

	Describe("RegisterWithInvite", func() {
		const ctxType = "*context.valueCtx"
		validInvite := func() *ent.Invite {
			return &ent.Invite{
				ID: 5, Email: "g@x.com", Role: invite.RoleMember,
				ExpiresAt: time.Now().Add(time.Hour),
			}
		}

		It("creates user, marks invite used, commits, issues token", func() {
			tx := dbmocks.NewMockTx(GinkgoT())
			storeMock.FindInviteByTokenHash(ctx, mock.AnythingOfType("string")).
				Return(validInvite(), nil).Once()
			storeMock.Tx(ctx).Return(tx, nil).Once()
			tx.EXPECT().
				CreateUser(ctx, mock.MatchedBy(func(p db.CreateUserParams) bool {
					return p.Email == "g@x.com"
				})).
				Return(&ent.User{ID: 1, Email: "g@x.com"}, nil).
				Once()
			tx.EXPECT().
				MarkInviteUsedWithUser(ctx, uint32(5), uint32(1), mock.AnythingOfType("time.Time")).
				Return(&ent.Invite{ID: 5}, nil).
				Once()
			tx.EXPECT().Commit().Return(nil).Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			u, tok, err := svc.RegisterWithInvite(
				ctx,
				"raw",
				"g@x.com",
				"password",
				"Guest",
				SessionMeta{},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(u.Email).To(Equal("g@x.com"))
			Expect(tok).NotTo(BeEmpty())
		})

		It("returns ErrInviteInvalid when validation fails", func() {
			storeMock.FindInviteByTokenHash(ctx, mock.AnythingOfType("string")).
				Return(nil, &ent.NotFoundError{}).Once()
			_, _, err := svc.RegisterWithInvite(
				ctx,
				"raw",
				"g@x.com",
				"pw",
				"",
				SessionMeta{},
			)
			Expect(err).To(MatchError(ErrInviteInvalid))
		})

		It("wraps bcrypt failures", func() {
			storeMock.FindInviteByTokenHash(ctx, mock.AnythingOfType("string")).
				Return(validInvite(), nil).Once()
			_, _, err := svc.RegisterWithInvite(
				ctx,
				"raw",
				"g@x.com",
				strings.Repeat("p", 80),
				"",
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("hash password")))
		})

		It("wraps tx.Begin failures", func() {
			storeMock.FindInviteByTokenHash(ctx, mock.AnythingOfType("string")).
				Return(validInvite(), nil).Once()
			storeMock.Tx(ctx).Return(nil, errors.New("begin fail")).Once()
			_, _, err := svc.RegisterWithInvite(
				ctx,
				"raw",
				"g@x.com",
				"password",
				"",
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("begin tx")))
		})

		It("rolls back on CreateUser failure", func() {
			tx := dbmocks.NewMockTx(GinkgoT())
			storeMock.FindInviteByTokenHash(ctx, mock.AnythingOfType("string")).
				Return(validInvite(), nil).Once()
			storeMock.Tx(ctx).Return(tx, nil).Once()
			tx.EXPECT().CreateUser(ctx, mock.AnythingOfType("db.CreateUserParams")).
				Return(nil, errors.New("create fail")).Once()
			tx.EXPECT().Rollback().Return(nil).Once()
			_, _, err := svc.RegisterWithInvite(
				ctx,
				"raw",
				"g@x.com",
				"password",
				"",
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("create user")))
		})

		It("rolls back on MarkInviteUsedWithUser failure", func() {
			tx := dbmocks.NewMockTx(GinkgoT())
			storeMock.FindInviteByTokenHash(ctx, mock.AnythingOfType("string")).
				Return(validInvite(), nil).Once()
			storeMock.Tx(ctx).Return(tx, nil).Once()
			tx.EXPECT().CreateUser(ctx, mock.AnythingOfType("db.CreateUserParams")).
				Return(&ent.User{ID: 1}, nil).Once()
			tx.EXPECT().
				MarkInviteUsedWithUser(ctx, uint32(5), uint32(1), mock.AnythingOfType("time.Time")).
				Return(nil, errors.New("mark fail")).
				Once()
			tx.EXPECT().Rollback().Return(nil).Once()
			_, _, err := svc.RegisterWithInvite(
				ctx,
				"raw",
				"g@x.com",
				"password",
				"",
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("mark invite used")))
		})

		It("wraps Commit failures", func() {
			tx := dbmocks.NewMockTx(GinkgoT())
			storeMock.FindInviteByTokenHash(ctx, mock.AnythingOfType("string")).
				Return(validInvite(), nil).Once()
			storeMock.Tx(ctx).Return(tx, nil).Once()
			tx.EXPECT().CreateUser(ctx, mock.AnythingOfType("db.CreateUserParams")).
				Return(&ent.User{ID: 1}, nil).Once()
			tx.EXPECT().
				MarkInviteUsedWithUser(ctx, uint32(5), uint32(1), mock.AnythingOfType("time.Time")).
				Return(&ent.Invite{ID: 5}, nil).
				Once()
			tx.EXPECT().Commit().Return(errors.New("commit fail")).Once()
			_, _, err := svc.RegisterWithInvite(
				ctx,
				"raw",
				"g@x.com",
				"password",
				"",
				SessionMeta{},
			)
			Expect(err).To(MatchError(ContainSubstring("commit tx")))
		})
	})
})
