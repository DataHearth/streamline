package db

import (
	"context"

	"github.com/datahearth/streamline/internal/role"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/request"
	"github.com/datahearth/streamline/ent/user"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Request store", Label("unit", "db"), func() {
	var (
		client  *ent.Client
		store   Store
		ctx     context.Context
		userID  uint32
		adminID uint32
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })
		store = New(client)

		u, err := store.CreateUser(ctx, CreateUserParams{
			Email:      "u@x.io",
			Role:       role.Seed(user.RoleMember),
			AuthMethod: user.AuthMethodLocal,
		})
		Expect(err).NotTo(HaveOccurred())
		userID = u.ID
		a, err := store.CreateUser(ctx, CreateUserParams{
			Email:      "a@x.io",
			Role:       role.Seed(user.RoleAdmin),
			AuthMethod: user.AuthMethodLocal,
		})
		Expect(err).NotTo(HaveOccurred())
		adminID = a.ID
	})

	It("creates a request with the requester edge", func() {
		r, err := store.CreateRequest(ctx, CreateRequestParams{
			MediaType: "movie", MediaID: 42, Title: "Flick", RequesterID: userID,
		})
		Expect(err).NotTo(HaveOccurred())
		got, err := store.GetRequest(ctx, r.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Edges.Requester.ID).To(Equal(userID))
		Expect(got.Status).To(Equal(request.StatusPending))
	})

	It("dedups via FindActiveRequest (pending/approved/available only)", func() {
		none, err := store.FindActiveRequest(ctx, "movie", 42)
		Expect(err).NotTo(HaveOccurred())
		Expect(none).To(BeNil())

		_, err = store.CreateRequest(ctx, CreateRequestParams{
			MediaType: "movie", MediaID: 42, Title: "Flick", RequesterID: userID,
		})
		Expect(err).NotTo(HaveOccurred())
		found, err := store.FindActiveRequest(ctx, "movie", 42)
		Expect(err).NotTo(HaveOccurred())
		Expect(found).NotTo(BeNil())
	})

	It(
		"denied requests are not considered active (dedup allows re-request)",
		func() {
			r, err := store.CreateRequest(ctx, CreateRequestParams{
				MediaType: "tvshow", MediaID: 7, Title: "Show", RequesterID: userID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(store.DenyRequest(ctx, r.ID, adminID, "no")).To(Succeed())

			found, err := store.FindActiveRequest(ctx, "tvshow", 7)
			Expect(err).NotTo(HaveOccurred())
			Expect(found).To(BeNil())
		},
	)

	It("approve sets approved_by; deny sets reason; reopen resets", func() {
		r, err := store.CreateRequest(ctx, CreateRequestParams{
			MediaType: "movie", MediaID: 1, Title: "M", RequesterID: userID,
		})
		Expect(err).NotTo(HaveOccurred())

		Expect(store.ApproveRequest(ctx, r.ID, adminID)).To(Succeed())
		got, _ := store.GetRequest(ctx, r.ID)
		Expect(got.Status).To(Equal(request.StatusApproved))
		Expect(got.Edges.ApprovedBy.ID).To(Equal(adminID))

		Expect(store.DenyRequest(ctx, r.ID, adminID, "low quality")).To(Succeed())
		got, _ = store.GetRequest(ctx, r.ID)
		Expect(got.Status).To(Equal(request.StatusDenied))
		Expect(got.Reason).To(Equal("low quality"))

		Expect(store.ReopenRequest(ctx, r.ID)).To(Succeed())
		got, _ = store.GetRequest(ctx, r.ID)
		Expect(got.Status).To(Equal(request.StatusPending))
		Expect(got.Reason).To(BeEmpty())
		Expect(got.Edges.ApprovedBy).To(BeNil())
	})

	It("rejects a second active row but allows a re-request after denial", func() {
		first, err := store.CreateRequest(ctx, CreateRequestParams{
			MediaType: "movie", MediaID: 42, Title: "Flick", RequesterID: userID,
		})
		Expect(err).NotTo(HaveOccurred())

		_, err = store.CreateRequest(ctx, CreateRequestParams{
			MediaType: "movie", MediaID: 42, Title: "Flick", RequesterID: userID,
		})
		Expect(ent.IsConstraintError(err)).To(BeTrue())

		Expect(store.DenyRequest(ctx, first.ID, adminID, "no")).To(Succeed())
		_, err = store.CreateRequest(ctx, CreateRequestParams{
			MediaType: "movie", MediaID: 42, Title: "Flick", RequesterID: userID,
		})
		Expect(err).NotTo(HaveOccurred())
	})

	It("MarkRequestsAvailable flips approved → available", func() {
		r, err := store.CreateRequest(ctx, CreateRequestParams{
			MediaType: "movie", MediaID: 9, Title: "M", RequesterID: userID,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(store.ApproveRequest(ctx, r.ID, adminID)).To(Succeed())

		Expect(store.MarkRequestsAvailable(ctx, "movie", 9)).To(Succeed())
		got, _ := store.GetRequest(ctx, r.ID)
		Expect(got.Status).To(Equal(request.StatusAvailable))
	})

	It("lists requests filtered by status and requester", func() {
		_, _ = store.CreateRequest(ctx, CreateRequestParams{
			MediaType: "movie", MediaID: 1, Title: "A", RequesterID: userID,
		})
		other, _ := store.CreateUser(ctx, CreateUserParams{
			Email:      "o@x.io",
			Role:       role.Seed(user.RoleMember),
			AuthMethod: user.AuthMethodLocal,
		})
		_, _ = store.CreateRequest(ctx, CreateRequestParams{
			MediaType: "movie", MediaID: 2, Title: "B", RequesterID: other.ID,
		})

		all, total, err := store.ListRequests(ctx, ListRequestsParams{Limit: 50})
		Expect(err).NotTo(HaveOccurred())
		Expect(total).To(Equal(2))
		Expect(all).To(HaveLen(2))

		mine, total, err := store.ListRequests(
			ctx,
			ListRequestsParams{RequesterID: userID, Limit: 50},
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(total).To(Equal(1))
		Expect(mine).To(HaveLen(1))

		n, err := store.CountRequestsByStatus(ctx, request.StatusPending, 0)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(2))

		n, err = store.CountRequestsByStatus(ctx, request.StatusPending, userID)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(1))
	})

	Describe("music and book media types", func() {
		const (
			mbidA = "11111111-1111-1111-1111-111111111111"
			mbidB = "22222222-2222-2222-2222-222222222222"
		)

		create := func(t request.MediaType, mut func(*ent.RequestCreate)) error {
			c := client.Request.Create().
				SetMediaType(t).
				SetTitle("T").
				SetRequesterID(userID)
			mut(c)
			_, err := c.Save(ctx)
			return err
		}
		artist := func(mbid string) error {
			return create(request.MediaTypeArtist, func(c *ent.RequestCreate) {
				c.SetMediaMbid(mbid)
			})
		}
		series := func(id uint32) error {
			return create(request.MediaTypeBookSeries, func(c *ent.RequestCreate) {
				c.SetMediaID(id)
			})
		}
		book := func(id uint32) error {
			return create(request.MediaTypeBook, func(c *ent.RequestCreate) {
				c.SetMediaID(id)
			})
		}

		It("saves an artist request with an MBID and no media_id", func() {
			r, err := client.Request.Create().
				SetMediaType(request.MediaTypeArtist).
				SetMediaMbid(mbidA).
				SetTitle("Artist").
				SetRequesterID(userID).
				Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(r.MediaID).To(BeZero())
			Expect(r.MediaMbid).To(Equal(mbidA))
		})

		It("saves a book request", func() {
			r, err := client.Request.Create().
				SetMediaType(request.MediaTypeBook).
				SetMediaID(12).
				SetTitle("Book").
				SetRequesterID(userID).
				Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(r.MediaID).To(Equal(uint32(12)))
		})

		It("persists the MBID through the store", func() {
			a, err := store.CreateRequest(ctx, CreateRequestParams{
				MediaType:   "artist",
				MediaMBID:   mbidA,
				Title:       "A",
				RequesterID: userID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(a.MediaMbid).To(Equal(mbidA))
			Expect(a.MediaID).To(BeZero())

			b, err := store.CreateRequest(ctx, CreateRequestParams{
				MediaType:   "book",
				MediaID:     4,
				Title:       "B",
				RequesterID: userID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(b.MediaID).To(Equal(uint32(4)))
		})

		It("finds an active request by MBID and ignores a denied one", func() {
			row, err := store.CreateRequest(ctx, CreateRequestParams{
				MediaType:   "artist",
				MediaMBID:   mbidA,
				Title:       "A",
				RequesterID: userID,
			})
			Expect(err).NotTo(HaveOccurred())

			found, err := store.FindActiveRequestByMBID(ctx, "artist", mbidA)
			Expect(err).NotTo(HaveOccurred())
			Expect(found.ID).To(Equal(row.ID))

			none, err := store.FindActiveRequestByMBID(ctx, "book", mbidA)
			Expect(err).NotTo(HaveOccurred())
			Expect(none).To(BeNil())

			Expect(store.DenyRequest(ctx, row.ID, userID, "no")).To(Succeed())
			none, err = store.FindActiveRequestByMBID(ctx, "artist", mbidA)
			Expect(err).NotTo(HaveOccurred())
			Expect(none).To(BeNil())
		})

		It("marks one request available by id", func() {
			row, err := store.CreateRequest(ctx, CreateRequestParams{
				MediaType:   "artist",
				MediaMBID:   mbidA,
				Title:       "A",
				RequesterID: userID,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(store.MarkRequestAvailable(ctx, row.ID)).To(Succeed())
			got, err := store.GetRequest(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Status).To(Equal(request.StatusAvailable))
		})

		It("allows active artist requests with different MBIDs", func() {
			Expect(artist(mbidA)).To(Succeed())
			Expect(artist(mbidB)).To(Succeed())
		})

		It("rejects a second active artist request with the same MBID", func() {
			Expect(artist(mbidA)).To(Succeed())
			Expect(ent.IsConstraintError(artist(mbidA))).To(BeTrue())
		})

		It("dedups a book on media_id within its type", func() {
			Expect(book(1)).To(Succeed())
			Expect(book(2)).To(Succeed())
			Expect(ent.IsConstraintError(book(1))).To(BeTrue())
		})

		It("keeps books and series in separate id spaces", func() {
			Expect(book(1)).To(Succeed())
			Expect(series(1)).To(Succeed())
			Expect(ent.IsConstraintError(series(1))).To(BeTrue())
		})

		It("flips an approved artist request by MBID on the first import", func() {
			row, err := store.CreateRequest(ctx, CreateRequestParams{
				MediaType:   "artist",
				MediaMBID:   mbidA,
				Title:       "A",
				RequesterID: userID,
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(
				store.MarkRequestsAvailableByMBID(ctx, "artist", mbidA),
			).To(Succeed())
			got, err := store.GetRequest(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Status).To(Equal(request.StatusPending))

			Expect(store.ApproveRequest(ctx, row.ID, userID)).To(Succeed())
			Expect(
				store.MarkRequestsAvailableByMBID(ctx, "artist", mbidB),
			).To(Succeed())
			got, err = store.GetRequest(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Status).To(Equal(request.StatusApproved))

			Expect(
				store.MarkRequestsAvailableByMBID(ctx, "artist", mbidA),
			).To(Succeed())
			got, err = store.GetRequest(ctx, row.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(got.Status).To(Equal(request.StatusAvailable))
		})

		It("filters the list by a set of media types", func() {
			Expect(artist(mbidA)).To(Succeed())
			Expect(book(1)).To(Succeed())
			Expect(series(1)).To(Succeed())

			rows, total, err := store.ListRequests(ctx, ListRequestsParams{
				MediaTypes: []string{"book", "book_series"}, Limit: 50,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(total).To(Equal(2))
			Expect(rows).To(HaveLen(2))
			for _, r := range rows {
				Expect(r.MediaType).NotTo(Equal(request.MediaTypeArtist))
			}
		})

		It("lets a denied music request be re-requested", func() {
			Expect(artist(mbidA)).To(Succeed())
			_, err := client.Request.Update().
				Where(request.MediaMbid(mbidA)).
				SetStatus(request.StatusDenied).
				Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(artist(mbidA)).To(Succeed())
		})
	})
})
