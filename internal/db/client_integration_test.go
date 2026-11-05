package db

import (
	"context"
	"database/sql"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Database Client", Label("integration", "db"), func() {
	Describe("Open", func() {
		It("should open an in-memory SQLite database and create schema", func() {
			ctx := context.Background()
			client, err := Open(ctx, ":memory:")
			Expect(err).NotTo(HaveOccurred())
			defer client.Close()

			user, err := client.User.Create().
				SetEmail("test@example.com").
				SetRole("admin").
				SetAuthMethod("local").
				Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(user.Email).To(Equal("test@example.com"))
		})

		It("refreshes planner statistics a table has outgrown at boot", func() {
			ctx := context.Background()
			dbPath := filepath.Join(GinkgoT().TempDir(), "streamline.db")
			client, err := Open(ctx, dbPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(client.Close()).To(Succeed())

			raw, err := sql.Open("sqlite", "file:"+dbPath)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(raw.Close)
			insertJobs := func(from, to int) {
				GinkgoHelper()
				_, err := raw.ExecContext(ctx, `
					WITH RECURSIVE n(i) AS (
						SELECT ? UNION ALL SELECT i + 1 FROM n WHERE i < ?
					)
					INSERT INTO scheduled_jobs (name, paused, last_status, last_duration_ms)
					SELECT 'job-' || i, 0, 'never', 0 FROM n`, from, to)
				Expect(err).NotTo(HaveOccurred())
			}
			stat := func() string {
				GinkgoHelper()
				var s string
				Expect(raw.QueryRowContext(
					ctx,
					`SELECT stat FROM sqlite_stat1 WHERE idx = 'scheduled_jobs_name_key'`,
				).Scan(&s)).To(Succeed())
				return s
			}
			insertJobs(1, 100)
			_, err = raw.ExecContext(ctx, "ANALYZE")
			Expect(err).NotTo(HaveOccurred())
			Expect(stat()).To(HavePrefix("100 "))
			insertJobs(101, 20000)

			client, err = Open(ctx, dbPath)
			Expect(err).NotTo(HaveOccurred())
			defer client.Close()

			Expect(stat()).To(HavePrefix("20000 "))
		})

		It("opens a file-backed SQLite database and persists rows", func() {
			ctx := context.Background()
			dbPath := filepath.Join(GinkgoT().TempDir(), "streamline.db")

			client, err := Open(ctx, dbPath)
			Expect(err).NotTo(HaveOccurred())

			_, err = client.User.Create().
				SetEmail("file@example.com").
				SetRole("admin").
				SetAuthMethod("local").
				Save(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(client.Close()).To(Succeed())

			// Reopen; schema + data should persist across the file-handle drop.
			client2, err := Open(ctx, dbPath)
			Expect(err).NotTo(HaveOccurred())
			defer client2.Close()

			count, err := client2.User.Query().Count(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(count).To(Equal(1))
		})

		It(
			"returns error when Schema.Create runs against a canceled context",
			func() {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()

				_, err := Open(ctx, ":memory:")
				Expect(err).To(HaveOccurred())
			},
		)

		It("returns error when the DSN points to an unwritable path", func() {
			// Directory that cannot be created-under (root of a path the
			// current uid cannot write) — SQLite fails to open the file and
			// bubbles up on the first Schema.Create query.
			_, err := Open(
				context.Background(),
				"/proc/1/streamline-cannot-write.db",
			)
			Expect(err).To(HaveOccurred())
		})
	})
})
