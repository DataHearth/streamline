package auth

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
)

var _ = Describe("RotateJWTSecret", Label("unit", "auth"), func() {
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
		"swaps the in-memory secret, truncates sessions, and re-issues a token",
		func() {
			oldSecret := *svc.jwtSecret.Load()

			storeMock.TruncateSessions(mock.AnythingOfType(ctxType)).
				Return(nil).Once()
			storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
				Return(&ent.User{ID: 1, Email: "admin@x.com"}, nil).Once()
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(&ent.Session{ID: 1}, nil).
				Once()

			tok, err := svc.RotateJWTSecret(ctx, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(tok).NotTo(BeEmpty())

			newSecret := *svc.jwtSecret.Load()
			Expect(newSecret).NotTo(Equal(oldSecret))
		},
	)

	It("logs but does not fail when session truncation errors", func() {
		storeMock.TruncateSessions(mock.AnythingOfType(ctxType)).
			Return(errors.New("truncate fail")).Once()
		storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
			Return(&ent.User{ID: 1, Email: "admin@x.com"}, nil).Once()
		storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
			Return(&ent.Session{ID: 1}, nil).
			Once()

		tok, err := svc.RotateJWTSecret(ctx, 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(tok).NotTo(BeEmpty())
	})

	It("returns an error when caller reload fails", func() {
		storeMock.TruncateSessions(mock.AnythingOfType(ctxType)).
			Return(nil).Once()
		storeMock.FindUserByID(mock.AnythingOfType(ctxType), uint32(1)).
			Return(nil, errors.New("not found")).Once()

		_, err := svc.RotateJWTSecret(ctx, 1)
		Expect(err).To(MatchError(ContainSubstring("reload caller")))
	})
})
