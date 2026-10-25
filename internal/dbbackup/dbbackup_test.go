package dbbackup_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/dbbackup"

	_ "modernc.org/sqlite"
)

// openLive opens a database the way the server does: WAL, one connection.
func openLive(path string) *sql.DB {
	GinkgoHelper()
	conn, err := sql.Open("sqlite",
		"file:"+path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	Expect(err).NotTo(HaveOccurred())
	conn.SetMaxOpenConns(1)
	return conn
}

// seed creates a database at path holding schema version and one row.
func seed(path string, version int, dirty bool, row string) {
	GinkgoHelper()
	conn := openLive(path)
	defer conn.Close()
	for _, q := range []string{
		`CREATE TABLE schema_migrations (version uint64, dirty bool)`,
		`CREATE TABLE things (name TEXT)`,
	} {
		_, err := conn.Exec(q)
		Expect(err).NotTo(HaveOccurred())
	}
	_, err := conn.Exec(
		`INSERT INTO schema_migrations VALUES (?, ?)`,
		version,
		dirty,
	)
	Expect(err).NotTo(HaveOccurred())
	_, err = conn.Exec(`INSERT INTO things VALUES (?)`, row)
	Expect(err).NotTo(HaveOccurred())
}

func rows(path string) []string {
	GinkgoHelper()
	conn, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	Expect(err).NotTo(HaveOccurred())
	defer conn.Close()
	rs, err := conn.Query(`SELECT name FROM things ORDER BY name`)
	Expect(err).NotTo(HaveOccurred())
	defer rs.Close()
	var out []string
	for rs.Next() {
		var s string
		Expect(rs.Scan(&s)).To(Succeed())
		out = append(out, s)
	}
	return out
}

var _ = Describe("dbbackup", Label("integration"), func() {
	var (
		ctx    context.Context
		dir    string
		dbPath string
		bdir   string
	)

	BeforeEach(func() {
		ctx = context.Background()
		dir = GinkgoT().TempDir()
		dbPath = filepath.Join(dir, "streamline.db")
		bdir = dbbackup.Dir(dir)
	})

	Describe("SnapshotFile", func() {
		It("writes an owner-only copy holding the live data, WAL included", func() {
			seed(dbPath, 42, false, "a")
			// A writer that stays open keeps its last commit in the WAL.
			live := openLive(dbPath)
			defer live.Close()
			_, err := live.Exec(`PRAGMA wal_autocheckpoint = 0`)
			Expect(err).NotTo(HaveOccurred())
			_, err = live.Exec(`INSERT INTO things VALUES ('b')`)
			Expect(err).NotTo(HaveOccurred())

			b, err := dbbackup.SnapshotFile(ctx, dbPath, bdir, dbbackup.KindManual)
			Expect(err).NotTo(HaveOccurred())
			Expect(b.Kind).To(Equal(dbbackup.KindManual))
			Expect(b.Version).To(Equal(uint(42)))
			Expect(b.Name).To(MatchRegexp(`^streamline-manual-42-\d{8}T\d{6}Z\.db$`))
			Expect(rows(b.Path)).To(Equal([]string{"a", "b"}))

			info, err := os.Stat(b.Path)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
			dinfo, err := os.Stat(bdir)
			Expect(err).NotTo(HaveOccurred())
			Expect(dinfo.Mode().Perm()).To(Equal(os.FileMode(0o700)))
		})

		It("narrows an existing backups directory to its owner", func() {
			seed(dbPath, 1, false, "a")
			Expect(os.MkdirAll(bdir, 0o755)).To(Succeed())
			Expect(os.Chmod(bdir, 0o755)).To(Succeed())
			_, err := dbbackup.SnapshotFile(ctx, dbPath, bdir, dbbackup.KindManual)
			Expect(err).NotTo(HaveOccurred())
			info, err := os.Stat(bdir)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o700)))
		})

		It("never creates a database that is not there", func() {
			_, err := dbbackup.SnapshotFile(ctx, dbPath, bdir, dbbackup.KindManual)
			Expect(err).To(MatchError(ContainSubstring("no database")))
			Expect(dbPath).NotTo(BeAnExistingFile())
		})

		It("gives two snapshots in one second distinct names", func() {
			seed(dbPath, 1, false, "a")
			a, err := dbbackup.SnapshotFile(ctx, dbPath, bdir, dbbackup.KindManual)
			Expect(err).NotTo(HaveOccurred())
			b, err := dbbackup.SnapshotFile(ctx, dbPath, bdir, dbbackup.KindManual)
			Expect(err).NotTo(HaveOccurred())
			Expect(a.Name).NotTo(Equal(b.Name))
		})
	})

	Describe("List and Prune", func() {
		touch := func(name string) {
			GinkgoHelper()
			Expect(os.MkdirAll(bdir, 0o700)).To(Succeed())
			Expect(
				os.WriteFile(filepath.Join(bdir, name), []byte("x"), 0o600),
			).To(Succeed())
		}

		It("lists newest first and ignores foreign files", func() {
			touch("streamline-scheduled-7-20261001T000000Z.db")
			touch("streamline-manual-7-20261003T000000Z.db")
			touch("streamline-pre-migration-6-20261002T000000Z.db")
			touch("notes.txt")
			touch(".streamline-manual-7-20261004T000000Z.db.partial")

			all, err := dbbackup.List(bdir)
			Expect(err).NotTo(HaveOccurred())
			names := make([]string, 0, len(all))
			for _, b := range all {
				names = append(names, b.Name)
			}
			Expect(names).To(Equal([]string{
				"streamline-manual-7-20261003T000000Z.db",
				"streamline-pre-migration-6-20261002T000000Z.db",
				"streamline-scheduled-7-20261001T000000Z.db",
			}))
			Expect(all[1].Kind).To(Equal(dbbackup.KindPreMigration))
			Expect(all[1].Version).To(Equal(uint(6)))
			Expect(
				all[1].CreatedAt,
			).To(Equal(time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)))
		})

		It("reads a missing directory as empty", func() {
			all, err := dbbackup.List(bdir)
			Expect(err).NotTo(HaveOccurred())
			Expect(all).To(BeEmpty())
		})

		It("keeps the newest of one kind and leaves the others alone", func() {
			touch("streamline-scheduled-7-20261001T000000Z.db")
			touch("streamline-scheduled-7-20261002T000000Z.db")
			touch("streamline-scheduled-7-20261003T000000Z.db")
			touch("streamline-manual-7-20260901T000000Z.db")

			removed, err := dbbackup.Prune(bdir, dbbackup.KindScheduled, 2)
			Expect(err).NotTo(HaveOccurred())
			Expect(
				removed,
			).To(Equal([]string{"streamline-scheduled-7-20261001T000000Z.db"}))
			Expect(
				filepath.Join(bdir, "streamline-manual-7-20260901T000000Z.db"),
			).To(BeAnExistingFile())
		})

		It("keeps everything at 0 and never prunes pre-restore copies", func() {
			touch("streamline-scheduled-7-20261001T000000Z.db")
			touch("streamline-pre-restore-7-20261001T000000Z.db")
			touch("streamline-pre-restore-7-20261002T000000Z.db")
			removed, err := dbbackup.Prune(bdir, dbbackup.KindScheduled, 0)
			Expect(err).NotTo(HaveOccurred())
			Expect(removed).To(BeEmpty())
			removed, err = dbbackup.Prune(bdir, dbbackup.KindPreRestore, 1)
			Expect(err).NotTo(HaveOccurred())
			Expect(removed).To(BeEmpty())
		})

		It("clears stale partial files", func() {
			touch(".streamline-manual-7-20261004T000000Z.db.partial")
			p := filepath.Join(
				bdir,
				".streamline-manual-7-20261004T000000Z.db.partial",
			)
			old := time.Now().Add(-48 * time.Hour)
			Expect(os.Chtimes(p, old, old)).To(Succeed())
			_, err := dbbackup.Prune(bdir, dbbackup.KindManual, 5)
			Expect(err).NotTo(HaveOccurred())
			Expect(p).NotTo(BeAnExistingFile())
		})

		It("finds a backup by name and refuses anything else", func() {
			touch("streamline-manual-7-20261003T000000Z.db")
			b, err := dbbackup.Find(bdir, "streamline-manual-7-20261003T000000Z.db")
			Expect(err).NotTo(HaveOccurred())
			Expect(
				b.Path,
			).To(Equal(filepath.Join(bdir, "streamline-manual-7-20261003T000000Z.db")))
			for _, bad := range []string{
				"../streamline.db",
				"streamline-manual-7-20261004T000000Z.db",
				"notes.txt",
				"x/streamline-manual-7-20261003T000000Z.db",
			} {
				_, err := dbbackup.Find(bdir, bad)
				Expect(err).To(MatchError(dbbackup.ErrNotFound), bad)
			}
		})
	})

	Describe("Restore", func() {
		var backup string

		BeforeEach(func() {
			seed(dbPath, 5, false, "old")
			b, err := dbbackup.SnapshotFile(ctx, dbPath, bdir, dbbackup.KindManual)
			Expect(err).NotTo(HaveOccurred())
			backup = b.Path
			live := openLive(dbPath)
			_, err = live.Exec(`INSERT INTO things VALUES ('new')`)
			Expect(err).NotTo(HaveOccurred())
			Expect(live.Close()).To(Succeed())
		})

		It("puts the backup in place and keeps the database it replaces", func() {
			res, err := dbbackup.Restore(ctx, backup, dbPath, bdir, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Restored.Version).To(Equal(uint(5)))
			Expect(rows(dbPath)).To(Equal([]string{"old"}))

			Expect(res.Replaced).NotTo(BeNil())
			Expect(res.Replaced.Kind).To(Equal(dbbackup.KindPreRestore))
			Expect(rows(res.Replaced.Path)).To(Equal([]string{"new", "old"}))
			info, err := os.Stat(dbPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))
			Expect(dbPath + "-wal").NotTo(BeAnExistingFile())
			Expect(dbPath + ".restoring").NotTo(BeAnExistingFile())
		})

		It("restores into an empty data directory", func() {
			Expect(os.Remove(dbPath)).To(Succeed())
			res, err := dbbackup.Restore(ctx, backup, dbPath, bdir, 10)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Replaced).To(BeNil())
			Expect(rows(dbPath)).To(Equal([]string{"old"}))
		})

		It("refuses while the database is open elsewhere", func() {
			live := openLive(dbPath)
			defer live.Close()
			Expect(live.Ping()).To(Succeed())
			_, err := live.Exec(`SELECT count(*) FROM things`)
			Expect(err).NotTo(HaveOccurred())

			_, err = dbbackup.Restore(ctx, backup, dbPath, bdir, 10)
			Expect(err).To(MatchError(dbbackup.ErrInUse))
			Expect(rows(dbPath)).To(Equal([]string{"new", "old"}))
			Expect(dbPath + ".restoring").NotTo(BeAnExistingFile())
		})

		It("refuses a backup newer than the binary", func() {
			_, err := dbbackup.Restore(ctx, backup, dbPath, bdir, 4)
			Expect(err).To(MatchError(ContainSubstring("newer than this binary")))
			Expect(rows(dbPath)).To(Equal([]string{"new", "old"}))
		})

		It("refuses a dirty backup", func() {
			dirty := filepath.Join(dir, "dirty.db")
			seed(dirty, 5, true, "x")
			_, err := dbbackup.Restore(ctx, dirty, dbPath, bdir, 10)
			Expect(err).To(MatchError(ContainSubstring("dirty")))
		})

		It("refuses a file that is not a database", func() {
			junk := filepath.Join(dir, "junk.db")
			Expect(
				os.WriteFile(
					junk,
					[]byte("not sqlite at all, not even close......"),
					0o600,
				),
			).To(Succeed())
			_, err := dbbackup.Restore(ctx, junk, dbPath, bdir, 10)
			Expect(err).To(HaveOccurred())
			Expect(rows(dbPath)).To(Equal([]string{"new", "old"}))
		})

		It("refuses a database without a schema version", func() {
			other := filepath.Join(dir, "other.db")
			conn := openLive(other)
			_, err := conn.Exec(`CREATE TABLE x (y INT)`)
			Expect(err).NotTo(HaveOccurred())
			Expect(conn.Close()).To(Succeed())
			_, err = dbbackup.Restore(ctx, other, dbPath, bdir, 10)
			Expect(err).To(MatchError(ContainSubstring("not a streamline database")))
		})

		It("refuses the live database itself", func() {
			_, err := dbbackup.Restore(ctx, dbPath, dbPath, bdir, 10)
			Expect(err).To(MatchError(ContainSubstring("live database")))
		})
	})
})
