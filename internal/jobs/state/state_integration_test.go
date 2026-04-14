package state

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/scheduledjob"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/scheduler"
)

var _ = Describe("DB StateHook", Label("integration", "scheduled_job"), func() {
	var (
		client *ent.Client
		hook   *Hook
		ctx    context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = db.Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
		hook = NewHook(client)
	})

	It("OnStart writes last_started_at", func() {
		seedRow(ctx, client, "rss-sync")
		now := time.Now()
		hook.OnStart(ctx, "rss-sync", now)

		row, err := client.ScheduledJob.Query().
			Where(scheduledjob.Name("rss-sync")).
			Only(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(row.LastStartedAt).NotTo(BeNil())
		Expect(*row.LastStartedAt).To(BeTemporally("~", now, time.Second))
	})

	It(
		"OnEnd success path writes finished/status/duration and clears last_error",
		func() {
			seedRow(ctx, client, "ok")
			_, err := client.ScheduledJob.Update().
				Where(scheduledjob.Name("ok")).
				SetLastError("stale").
				Save(ctx)
			Expect(err).NotTo(HaveOccurred())

			end := time.Now()
			hook.OnEnd(ctx, "ok", end, "success", nil, 750*time.Millisecond)

			row, err := client.ScheduledJob.Query().
				Where(scheduledjob.Name("ok")).
				Only(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(row.LastFinishedAt).NotTo(BeNil())
			Expect(*row.LastFinishedAt).To(BeTemporally("~", end, time.Second))
			Expect(string(row.LastStatus)).To(Equal("success"))
			Expect(row.LastError).To(BeEmpty())
			Expect(row.LastDurationMs).To(Equal(uint32(750)))
		},
	)

	It("OnEnd error path stores the error message", func() {
		seedRow(ctx, client, "bad")
		hook.OnEnd(ctx, "bad", time.Now(), "error", errors.New("boom"), time.Second)

		row, err := client.ScheduledJob.Query().
			Where(scheduledjob.Name("bad")).
			Only(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(row.LastStatus)).To(Equal("error"))
		Expect(row.LastError).To(Equal("boom"))
	})

	It(
		"OnEnd skipped path leaves last_started_at/last_finished_at untouched",
		func() {
			seedRow(ctx, client, "skipped")
			hook.OnEnd(ctx, "skipped", time.Now(), "skipped", nil, 0)

			row, err := client.ScheduledJob.Query().
				Where(scheduledjob.Name("skipped")).
				Only(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(row.LastStatus)).To(Equal("skipped"))
			Expect(row.LastStartedAt).To(BeNil())
			Expect(row.LastFinishedAt).To(BeNil())
		},
	)

	It("missing row is treated as a no-op (logged, not panicked)", func() {
		Expect(func() { hook.OnStart(ctx, "ghost", time.Now()) }).NotTo(Panic())
		Expect(
			func() { hook.OnEnd(ctx, "ghost", time.Now(), "success", nil, 0) },
		).NotTo(Panic())
	})
})

var _ = Describe("Seed", Label("integration", "scheduled_job"), func() {
	var (
		ctx    context.Context
		client *ent.Client
	)
	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = db.Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
	})

	It("inserts rows for unseen names and leaves existing rows alone", func() {
		_, err := client.ScheduledJob.Create().
			SetName("rss-sync").
			SetPaused(true).
			Save(ctx)
		Expect(err).NotTo(HaveOccurred())

		Expect(Seed(ctx, client, []scheduler.JobInfo{
			{Name: "rss-sync"},
			{Name: "cleanup"},
		})).To(Succeed())

		all, err := client.ScheduledJob.Query().
			Order(ent.Asc(scheduledjob.FieldName)).
			All(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(all).To(HaveLen(2))
		Expect(all[0].Name).To(Equal("cleanup"))
		Expect(all[1].Name).To(Equal("rss-sync"))
		Expect(all[1].Paused).To(BeTrue(), "existing row's Paused must be preserved")
	})
})

var _ = Describe("PausedNames", Label("integration", "scheduled_job"), func() {
	var (
		ctx    context.Context
		client *ent.Client
	)
	BeforeEach(func() {
		ctx = context.Background()
		var err error
		client, err = db.Open(ctx, ":memory:")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })
	})

	It("returns the sorted set of paused job names", func() {
		_, err := client.ScheduledJob.Create().SetName("a").SetPaused(true).Save(ctx)
		Expect(err).NotTo(HaveOccurred())
		_, err = client.ScheduledJob.Create().SetName("b").SetPaused(false).Save(ctx)
		Expect(err).NotTo(HaveOccurred())
		_, err = client.ScheduledJob.Create().SetName("c").SetPaused(true).Save(ctx)
		Expect(err).NotTo(HaveOccurred())

		names, err := PausedNames(ctx, client)
		Expect(err).NotTo(HaveOccurred())
		Expect(names).To(Equal([]string{"a", "c"}))
	})
})

func seedRow(ctx context.Context, client *ent.Client, name string) {
	GinkgoHelper()
	_, err := client.ScheduledJob.Create().SetName(name).Save(ctx)
	Expect(err).NotTo(HaveOccurred())
}
