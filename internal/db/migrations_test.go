package db

import (
	"database/sql"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	_ "modernc.org/sqlite"
)

// requestActiveUniqueVersion is the migration this spec exercises. Clean-database
// application is already covered wherever Open runs against a file path; what
// needs its own spec is the upgrade of a database that already holds the
// duplicates the index forbids, because that is the one path where the
// migration can abort and leave a deployment unable to start.
const requestActiveUniqueVersion = 20260814140936

var _ = Describe("runMigrations", Label("integration", "db"), func() {
	var sqlDB *sql.DB

	// Seeded rows reference a user that is never created, so foreign keys stay
	// off here — the subject is the dedup, not referential integrity.
	BeforeEach(func() {
		var err error
		sqlDB, err = sql.Open(
			"sqlite",
			"file:"+filepath.Join(GinkgoT().TempDir(), "migrate.db"),
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(sqlDB.Close()).To(Succeed()) })
	})

	exec := func(query string, args ...any) error {
		GinkgoHelper()
		_, err := sqlDB.Exec(query, args...)
		return err
	}

	seed := func(id int, mediaType string, mediaID int, status string) {
		GinkgoHelper()
		Expect(exec(
			`INSERT INTO requests
			 (id, create_time, update_time, media_type, media_id, title,
			  status, user_requests)
			 VALUES (?, datetime('now'), datetime('now'), ?, ?, 'T', ?, 1)`,
			id, mediaType, mediaID, status,
		)).To(Succeed())
	}

	ids := func() []int {
		GinkgoHelper()
		rows, err := sqlDB.Query(`SELECT id FROM requests ORDER BY id`)
		Expect(err).NotTo(HaveOccurred())
		defer rows.Close()
		var out []int
		for rows.Next() {
			var id int
			Expect(rows.Scan(&id)).To(Succeed())
			out = append(out, id)
		}
		Expect(rows.Err()).NotTo(HaveOccurred())
		return out
	}

	// Stops one version short of the migration under test so the duplicates can
	// be seeded in the state a real deployment would have reached them in.
	migrateToPrevious := func() {
		GinkgoHelper()
		src, err := iofs.New(migrationsFS, "migrations")
		Expect(err).NotTo(HaveOccurred())
		previous, err := src.Prev(requestActiveUniqueVersion)
		Expect(err).NotTo(HaveOccurred())
		drv, err := sqlite.WithInstance(sqlDB, &sqlite.Config{})
		Expect(err).NotTo(HaveOccurred())
		m, err := migrate.NewWithInstance("iofs", src, "sqlite", drv)
		Expect(err).NotTo(HaveOccurred())
		Expect(m.Migrate(previous)).To(Succeed())
	}

	It("collapses duplicate active requests and then enforces uniqueness", func() {
		migrateToPrevious()

		seed(1, "movie", 42, "pending")   // earliest of its set — survives
		seed(2, "movie", 42, "pending")   // duplicate — collapsed
		seed(3, "movie", 42, "approved")  // duplicate under another status
		seed(4, "movie", 42, "denied")    // outside the predicate — survives
		seed(5, "tvshow", 7, "available") // earliest of its set — survives
		seed(6, "tvshow", 7, "pending")   // duplicate — collapsed
		seed(7, "movie", 99, "pending")   // no duplicate — survives

		Expect(runMigrations(sqlDB)).To(Succeed())
		Expect(ids()).To(Equal([]int{1, 4, 5, 7}))

		By("rejecting a second active row for media the index now covers")
		Expect(exec(
			`INSERT INTO requests
			 (id, create_time, update_time, media_type, media_id, title,
			  status, user_requests)
			 VALUES (8, datetime('now'), datetime('now'), 'movie', 42, 'T',
			         'pending', 1)`,
		)).NotTo(Succeed())

		By("still admitting a denied row, which the predicate excludes")
		seed(9, "movie", 42, "denied")
	})

	It("applies to a database holding no duplicates", func() {
		migrateToPrevious()

		seed(1, "movie", 42, "pending")
		seed(2, "movie", 42, "denied")
		seed(3, "tvshow", 7, "approved")

		Expect(runMigrations(sqlDB)).To(Succeed())
		Expect(ids()).To(Equal([]int{1, 2, 3}))
	})
})
