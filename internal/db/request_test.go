package db

import (
	"context"

	"github.com/datahearth/streamline/ent/request"
	"github.com/datahearth/streamline/ent/user"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Request store", Label("unit", "db"), func() {
	var (
		store   Store
		ctx     context.Context
		userID  uint32
		adminID uint32
	)

	BeforeEach(func() {
		ctx = context.Background()
		client, err := Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })
		store = New(client)

		u, err := store.CreateUser(ctx, CreateUserParams{
			Email: "u@x.io", Role: user.RoleMember, AuthMethod: user.AuthMethodLocal,
		})
		Expect(err).NotTo(HaveOccurred())
		userID = u.ID
		a, err := store.CreateUser(ctx, CreateUserParams{
			Email: "a@x.io", Role: user.RoleAdmin, AuthMethod: user.AuthMethodLocal,
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
			Email: "o@x.io", Role: user.RoleMember, AuthMethod: user.AuthMethodLocal,
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

		n, err := store.CountRequestsByStatus(ctx, request.StatusPending)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(2))
	})
})
