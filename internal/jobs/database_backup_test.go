package jobs

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/dbbackup"
	"github.com/datahearth/streamline/internal/scheduler"
	"github.com/datahearth/streamline/internal/testutil/configtest"

	_ "modernc.org/sqlite"
)

var _ = Describe("DatabaseBackup", Label("integration"), func() {
	var dbPath, dir string

	BeforeEach(func() {
		configtest.Setup(map[string]any{
			"backup": map[string]any{"retention": map[string]any{"scheduled": 2}},
		})
		root := GinkgoT().TempDir()
		dbPath = filepath.Join(root, "streamline.db")
		dir = dbbackup.Dir(root)
		conn, err := sql.Open("sqlite", "file:"+dbPath)
		Expect(err).NotTo(HaveOccurred())
		_, err = conn.Exec(
			`CREATE TABLE schema_migrations (version uint64, dirty bool);
			INSERT INTO schema_migrations VALUES (3, false)`,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(conn.Close()).To(Succeed())
	})

	every := func(d time.Duration) func() time.Duration {
		return func() time.Duration { return d }
	}

	scheduled := func() []dbbackup.Backup {
		GinkgoHelper()
		all, err := dbbackup.List(dir)
		Expect(err).NotTo(HaveOccurred())
		var out []dbbackup.Backup
		for _, b := range all {
			if b.Kind == dbbackup.KindScheduled {
				out = append(out, b)
			}
		}
		return out
	}

	It("takes a backup when none is recent, and skips the next boot tick", func() {
		job := DatabaseBackup(dbPath, dir, every(24*time.Hour))
		Expect(job(context.Background())).To(Succeed())
		Expect(scheduled()).To(HaveLen(1))
		Expect(scheduled()[0].Version).To(Equal(uint(3)))

		Expect(job(context.Background())).To(Succeed())
		Expect(scheduled()).To(HaveLen(1))
	})

	It("takes one on an ordinary tick a moment short of a full interval", func() {
		Expect(os.MkdirAll(dir, 0o700)).To(Succeed())
		last := time.Now().
			UTC().
			Add(-time.Hour + time.Second).
			Format("20060102T150405Z")
		Expect(os.WriteFile(
			filepath.Join(dir, "streamline-scheduled-3-"+last+".db"), nil, 0o600,
		)).To(Succeed())
		Expect(
			DatabaseBackup(dbPath, dir, every(time.Hour))(context.Background()),
		).To(Succeed())
		Expect(scheduled()).To(HaveLen(2))
	})

	It("always takes one on a manual run, and prunes past the retention", func() {
		Expect(os.MkdirAll(dir, 0o700)).To(Succeed())
		for _, n := range []string{
			"streamline-scheduled-3-20260101T000000Z.db",
			"streamline-scheduled-3-20260102T000000Z.db",
		} {
			Expect(os.WriteFile(filepath.Join(dir, n), nil, 0o600)).To(Succeed())
		}
		job := DatabaseBackup(dbPath, dir, every(time.Hour))
		Expect(job(scheduler.WithManual(context.Background()))).To(Succeed())
		Expect(job(scheduler.WithManual(context.Background()))).To(Succeed())
		got := scheduled()
		Expect(got).To(HaveLen(2))
		for _, b := range got {
			Expect(
				b.Name,
			).NotTo(ContainSubstring("2026010"), "the seeded old ones went first")
		}
	})
})
