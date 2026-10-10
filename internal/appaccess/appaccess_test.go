package appaccess

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/testutil/dbtest"
)

var _ = Describe("Tracker", Label("unit"), func() {
	var (
		ctx     context.Context
		client  *ent.Client
		tracker *Tracker
		userID  uint32
		since   time.Time
	)

	BeforeEach(func() {
		ctx = context.Background()
		client = dbtest.SetupTestDB(ctx)
		DeferCleanup(client.Close)
		tracker = NewTracker(client)
		since = time.Now().UTC().Add(-time.Hour)
		userID = client.User.Create().
			SetEmail("a@example.com").
			SetSubsonicPassword("pw").
			SetSubsonicCreatedAt(since).
			SaveX(ctx).ID
	})

	lastClient := func() string {
		GinkgoHelper()
		return client.User.GetX(ctx, userID).SubsonicLastClient
	}

	It("records the client and time off the caller's path", func() {
		tracker.Touch(ctx, userID, Subsonic, since, "Symfonium")
		Eventually(lastClient).Should(Equal("Symfonium"))
		Expect(client.User.GetX(ctx, userID).SubsonicLastUsedAt).NotTo(BeNil())
	})

	It("writes once for two calls inside the window", func() {
		tracker.Touch(ctx, userID, Subsonic, since, "First")
		Eventually(lastClient).Should(Equal("First"))
		tracker.Touch(ctx, userID, Subsonic, since, "Second")
		Consistently(lastClient, "200ms").Should(Equal("First"))
	})

	It("writes again when the credential changed inside the window", func() {
		tracker.Touch(ctx, userID, Subsonic, since, "Old")
		Eventually(lastClient).Should(Equal("Old"))

		rotated := since.Add(time.Minute)
		client.User.UpdateOneID(userID).
			SetSubsonicCreatedAt(rotated).
			ClearSubsonicLastClient().
			ClearSubsonicLastUsedAt().
			ExecX(ctx)
		tracker.Touch(ctx, userID, Subsonic, rotated, "New")
		Eventually(lastClient).Should(Equal("New"))
	})

	It("does not stamp a credential rotated since the touch was queued", func() {
		client.User.UpdateOneID(userID).
			SetSubsonicCreatedAt(since.Add(time.Minute)).
			ExecX(ctx)
		tracker.Touch(ctx, userID, Subsonic, since, "Stale")
		Consistently(lastClient, "200ms").Should(BeEmpty())
	})

	It("does not resurrect a deleted credential", func() {
		client.User.UpdateOneID(userID).
			ClearSubsonicPassword().
			ClearSubsonicCreatedAt().
			ExecX(ctx)
		tracker.Touch(ctx, userID, Subsonic, since, "Late")
		Consistently(lastClient, "200ms").Should(BeEmpty())
	})

	It("keeps the two credentials apart", func() {
		tracker.Touch(ctx, userID, OPDS, time.Time{}, "KOReader")
		Eventually(func() string {
			return client.User.GetX(ctx, userID).OpdsLastClient
		}).Should(Equal("KOReader"))
		Expect(lastClient()).To(BeEmpty())
	})

	DescribeTable(
		"sanitises the client string",
		func(in, want string) {
			Expect(sanitizeClient(in)).To(Equal(want))
		},
		Entry("trims", "  DSub ", "DSub"),
		Entry("strips control characters", "Sym\x00fo\nnium", "Symfonium"),
		Entry("empty stays empty", "", ""),
		Entry(
			"cuts at the limit",
			strings.Repeat("a", 100),
			strings.Repeat("a", clientMaxLen),
		),
	)
})
