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
	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
)

var _ = Describe("Session service unit", Label("unit", "auth"), func() {
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

	Describe("CreateSession", func() {
		It("forwards params to the store with adjusted ExpiresAt", func() {
			row := &ent.Session{ID: 1, Jti: "j1"}
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.MatchedBy(func(p db.CreateSessionParams) bool {
				return p.JTI == "j1" && p.UserID == 7 && p.IP == "1.2.3.4" &&
					p.UserAgent == "ua" && p.ExpiresAt.After(time.Now())
			})).
				Return(row, nil).
				Once()

			got, err := svc.CreateSession(
				ctx,
				7,
				"j1",
				time.Hour,
				SessionMeta{IP: "1.2.3.4", UserAgent: "ua"},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(row))
		})

		It("truncates oversized user agent to maxUserAgentLen", func() {
			big := strings.Repeat("x", maxUserAgentLen+10)
			row := &ent.Session{ID: 1}
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.MatchedBy(func(p db.CreateSessionParams) bool {
				return len(p.UserAgent) == maxUserAgentLen
			})).
				Return(row, nil).
				Once()

			_, err := svc.CreateSession(
				ctx,
				7,
				"j1",
				time.Hour,
				SessionMeta{UserAgent: big},
			)
			Expect(err).NotTo(HaveOccurred())
		})

		It("wraps store errors", func() {
			storeMock.CreateSession(mock.AnythingOfType(ctxType), mock.AnythingOfType("db.CreateSessionParams")).
				Return(nil, errors.New("insert fail")).
				Once()
			_, err := svc.CreateSession(ctx, 1, "j1", time.Hour, SessionMeta{})
			Expect(err).To(MatchError(ContainSubstring("create session")))
		})
	})

	Describe("ValidateSession", func() {
		It("returns nil when the session is active", func() {
			storeMock.FindSessionByJTI(mock.AnythingOfType(ctxType), "j1").
				Return(&ent.Session{ExpiresAt: time.Now().Add(time.Hour)}, nil).
				Once()
			Expect(svc.ValidateSession(ctx, "j1")).To(Succeed())
		})

		It("returns ErrSessionNotFound when ent reports NotFound", func() {
			storeMock.FindSessionByJTI(mock.AnythingOfType(ctxType), "j1").
				Return(nil, &ent.NotFoundError{}).Once()
			Expect(svc.ValidateSession(ctx, "j1")).To(MatchError(ErrSessionNotFound))
		})

		It("wraps non-NotFound query errors", func() {
			storeMock.FindSessionByJTI(mock.AnythingOfType(ctxType), "j1").
				Return(nil, errors.New("query fail")).Once()
			Expect(
				svc.ValidateSession(ctx, "j1"),
			).To(MatchError(ContainSubstring("query session")))
		})

		It("returns ErrSessionRevoked when revoked_at is set", func() {
			now := time.Now()
			storeMock.FindSessionByJTI(mock.AnythingOfType(ctxType), "j1").
				Return(&ent.Session{RevokedAt: &now, ExpiresAt: now.Add(time.Hour)}, nil).
				Once()
			Expect(svc.ValidateSession(ctx, "j1")).To(MatchError(ErrSessionRevoked))
		})

		It("returns ErrSessionExpired when expires_at is past", func() {
			storeMock.FindSessionByJTI(mock.AnythingOfType(ctxType), "j1").
				Return(&ent.Session{ExpiresAt: time.Now().Add(-time.Hour)}, nil).
				Once()
			Expect(svc.ValidateSession(ctx, "j1")).To(MatchError(ErrSessionExpired))
		})
	})

	Describe("TouchSession", func() {
		It("delegates to the store", func() {
			when := time.Now()
			storeMock.TouchSession(ctx, "j1", when).Return(nil).Once()
			Expect(svc.TouchSession(ctx, "j1", when)).To(Succeed())
		})

		It("wraps store errors", func() {
			when := time.Now()
			storeMock.TouchSession(ctx, "j1", when).
				Return(errors.New("touch fail")).
				Once()
			Expect(svc.TouchSession(ctx, "j1", when)).
				To(MatchError(ContainSubstring("touch session")))
		})
	})

	Describe("RevokeSession", func() {
		It("delegates to RevokeSessionByJTI", func() {
			storeMock.RevokeSessionByJTI(mock.AnythingOfType(ctxType), "j1", mock.AnythingOfType("time.Time")).
				Return(nil).
				Once()
			Expect(svc.RevokeSession(ctx, "j1")).To(Succeed())
		})

		It("wraps store errors", func() {
			storeMock.RevokeSessionByJTI(mock.AnythingOfType(ctxType), "j1", mock.AnythingOfType("time.Time")).
				Return(errors.New("rev fail")).
				Once()
			Expect(
				svc.RevokeSession(ctx, "j1"),
			).To(MatchError(ContainSubstring("revoke session")))
		})
	})

	Describe("RevokeSessionByID", func() {
		It("returns nil when one row was revoked", func() {
			storeMock.RevokeUserSessionByID(mock.AnythingOfType(ctxType), uint32(1), uint32(2), mock.AnythingOfType("time.Time")).
				Return(1, nil).
				Once()
			Expect(svc.RevokeSessionByID(ctx, 1, 2)).To(Succeed())
		})

		It(
			"returns nil when zero rows revoked but the session still exists for the user",
			func() {
				storeMock.RevokeUserSessionByID(mock.AnythingOfType(ctxType), uint32(1), uint32(2), mock.AnythingOfType("time.Time")).
					Return(0, nil).
					Once()
				storeMock.UserSessionExists(mock.AnythingOfType(ctxType), uint32(1), uint32(2)).
					Return(true, nil).
					Once()
				Expect(svc.RevokeSessionByID(ctx, 1, 2)).To(Succeed())
			},
		)

		It(
			"returns ErrSessionNotFound when the session does not belong to the user",
			func() {
				storeMock.RevokeUserSessionByID(mock.AnythingOfType(ctxType), uint32(1), uint32(2), mock.AnythingOfType("time.Time")).
					Return(0, nil).
					Once()
				storeMock.UserSessionExists(mock.AnythingOfType(ctxType), uint32(1), uint32(2)).
					Return(false, nil).
					Once()
				Expect(
					svc.RevokeSessionByID(ctx, 1, 2),
				).To(MatchError(ErrSessionNotFound))
			},
		)

		It("wraps store errors from RevokeUserSessionByID", func() {
			storeMock.RevokeUserSessionByID(mock.AnythingOfType(ctxType), uint32(1), uint32(2), mock.AnythingOfType("time.Time")).
				Return(0, errors.New("revoke fail")).
				Once()
			Expect(svc.RevokeSessionByID(ctx, 1, 2)).
				To(MatchError(ContainSubstring("revoke session by id")))
		})

		It("wraps store errors from UserSessionExists", func() {
			storeMock.RevokeUserSessionByID(mock.AnythingOfType(ctxType), uint32(1), uint32(2), mock.AnythingOfType("time.Time")).
				Return(0, nil).
				Once()
			storeMock.UserSessionExists(mock.AnythingOfType(ctxType), uint32(1), uint32(2)).
				Return(false, errors.New("check fail")).
				Once()
			Expect(svc.RevokeSessionByID(ctx, 1, 2)).
				To(MatchError(ContainSubstring("check session")))
		})
	})

	Describe("RevokeAllUserSessions", func() {
		It("delegates to the store", func() {
			storeMock.RevokeAllUserSessions(mock.AnythingOfType(ctxType), uint32(1), mock.AnythingOfType("time.Time")).
				Return(nil).
				Once()
			Expect(svc.RevokeAllUserSessions(ctx, 1)).To(Succeed())
		})

		It("wraps store errors", func() {
			storeMock.RevokeAllUserSessions(mock.AnythingOfType(ctxType), uint32(1), mock.AnythingOfType("time.Time")).
				Return(errors.New("rev fail")).
				Once()
			Expect(svc.RevokeAllUserSessions(ctx, 1)).
				To(MatchError(ContainSubstring("revoke all user sessions")))
		})
	})

	Describe("RevokeOtherSessions", func() {
		It("delegates to the store", func() {
			storeMock.RevokeOtherUserSessions(mock.AnythingOfType(ctxType), uint32(1), "keep", mock.AnythingOfType("time.Time")).
				Return(nil).
				Once()
			Expect(svc.RevokeOtherSessions(ctx, 1, "keep")).To(Succeed())
		})

		It("wraps store errors", func() {
			storeMock.RevokeOtherUserSessions(mock.AnythingOfType(ctxType), uint32(1), "keep", mock.AnythingOfType("time.Time")).
				Return(errors.New("rev fail")).
				Once()
			Expect(svc.RevokeOtherSessions(ctx, 1, "keep")).
				To(MatchError(ContainSubstring("revoke other sessions")))
		})
	})

	Describe("ListUserSessions", func() {
		It("delegates to the store", func() {
			rows := []*ent.Session{{ID: 1}}
			storeMock.ListUserSessions(ctx, uint32(1)).Return(rows, nil).Once()
			got, err := svc.ListUserSessions(ctx, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(rows))
		})
	})

	Describe("PurgeExpiredSessions", func() {
		It("returns the row count from the store", func() {
			before := time.Now()
			storeMock.PurgeExpiredSessions(mock.AnythingOfType(ctxType), before).
				Return(3, nil).
				Once()
			n, err := svc.PurgeExpiredSessions(ctx, before)
			Expect(err).NotTo(HaveOccurred())
			Expect(n).To(Equal(3))
		})

		It("wraps store errors", func() {
			before := time.Now()
			storeMock.PurgeExpiredSessions(mock.AnythingOfType(ctxType), before).
				Return(0, errors.New("purge fail")).Once()
			_, err := svc.PurgeExpiredSessions(ctx, before)
			Expect(err).To(MatchError(ContainSubstring("purge expired sessions")))
		})
	})

	Describe("TouchSessionAsync", func() {
		It("calls TouchSession in the background", func() {
			done := make(chan struct{})
			storeMock.TouchSession(mock.AnythingOfType("*context.timerCtx"), "j1", mock.AnythingOfType("time.Time")).
				RunAndReturn(func(context.Context, string, time.Time) error {
					close(done)
					return nil
				}).
				Once()

			svc.TouchSessionAsync("j1")
			Eventually(done, "2s").Should(BeClosed())
			Expect(svc.Shutdown(ctx)).To(Succeed())
		})

		It("logs and continues when TouchSession errors", func() {
			done := make(chan struct{})
			storeMock.TouchSession(mock.AnythingOfType("*context.timerCtx"), "j1", mock.AnythingOfType("time.Time")).
				RunAndReturn(func(context.Context, string, time.Time) error {
					close(done)
					return errors.New("touch fail")
				}).
				Once()

			svc.TouchSessionAsync("j1")
			Eventually(done, "2s").Should(BeClosed())
			Expect(svc.Shutdown(ctx)).To(Succeed())
		})
	})

	Describe("Shutdown", func() {
		It("returns nil when no goroutines are pending", func() {
			Expect(svc.Shutdown(ctx)).To(Succeed())
		})

		It("waits for in-flight TouchSessionAsync goroutines", func() {
			started := make(chan struct{})
			storeMock.TouchSession(mock.AnythingOfType("*context.timerCtx"), "j1", mock.AnythingOfType("time.Time")).
				RunAndReturn(func(context.Context, string, time.Time) error {
					close(started)
					time.Sleep(50 * time.Millisecond)
					return nil
				}).
				Once()

			svc.TouchSessionAsync("j1")
			<-started
			Expect(svc.Shutdown(ctx)).To(Succeed())
		})

		It("returns ctx.Err when ctx is canceled before goroutines finish", func() {
			block := make(chan struct{})
			done := make(chan struct{})
			storeMock.TouchSession(mock.AnythingOfType("*context.timerCtx"), "j1", mock.AnythingOfType("time.Time")).
				RunAndReturn(func(context.Context, string, time.Time) error {
					<-block
					close(done)
					return nil
				}).
				Once()

			svc.TouchSessionAsync("j1")

			cancelCtx, cancel := context.WithCancel(ctx)
			cancel()
			err := svc.Shutdown(cancelCtx)
			Expect(err).To(MatchError(context.Canceled))

			close(block)
			Eventually(done, "1s").Should(BeClosed())
		})
	})
})
