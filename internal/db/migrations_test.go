package db

import (
	"context"
	"database/sql"
	"fmt"
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

		Expect(runMigrations(context.Background(), sqlDB)).To(Succeed())
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

		Expect(runMigrations(context.Background(), sqlDB)).To(Succeed())
		Expect(ids()).To(Equal([]int{1, 2, 3}))
	})
})

// backfillPersonCreditsVersion is the data migration that fills persons and
// credits from the legacy JSON cast columns. It only ever runs against a
// database that already holds cast, so the seeded state below is the subject:
// a clean install has nothing for it to carry across.
const backfillPersonCreditsVersion = 20260910102919

var _ = Describe("person credits backfill", Label("integration", "db"), func() {
	var sqlDB *sql.DB

	BeforeEach(func() {
		var err error
		sqlDB, err = sql.Open(
			"sqlite",
			"file:"+filepath.Join(GinkgoT().TempDir(), "backfill.db"),
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(sqlDB.Close()).To(Succeed()) })

		src, err := iofs.New(migrationsFS, "migrations")
		Expect(err).NotTo(HaveOccurred())
		previous, err := src.Prev(backfillPersonCreditsVersion)
		Expect(err).NotTo(HaveOccurred())
		drv, err := sqlite.WithInstance(sqlDB, &sqlite.Config{})
		Expect(err).NotTo(HaveOccurred())
		m, err := migrate.NewWithInstance("iofs", src, "sqlite", drv)
		Expect(err).NotTo(HaveOccurred())
		Expect(m.Migrate(previous)).To(Succeed())
	})

	seedMovie := func(id int, title string, tmdbID int, cast any) {
		GinkgoHelper()
		_, err := sqlDB.Exec(
			"INSERT INTO movies (id, create_time, update_time, title,"+
				" original_title, year, tmdb_id, `cast`)"+
				" VALUES (?, datetime('now'), datetime('now'), ?, ?, 2020, ?, ?)",
			id, title, title, tmdbID, cast,
		)
		Expect(err).NotTo(HaveOccurred())
	}

	seedShow := func(id int, title string, tvdbID int, cast any) {
		GinkgoHelper()
		_, err := sqlDB.Exec(
			"INSERT INTO tv_shows (id, create_time, update_time, title, year,"+
				" tvdb_id, `cast`)"+
				" VALUES (?, datetime('now'), datetime('now'), ?, 2020, ?, ?)",
			id, title, tvdbID, cast,
		)
		Expect(err).NotTo(HaveOccurred())
	}

	queryStrings := func(query string) []string {
		GinkgoHelper()
		rows, err := sqlDB.Query(query)
		Expect(err).NotTo(HaveOccurred())
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			Expect(rows.Scan(&s)).To(Succeed())
			out = append(out, s)
		}
		Expect(rows.Err()).NotTo(HaveOccurred())
		return out
	}

	people := func() []string {
		GinkgoHelper()
		return queryStrings(
			"SELECT name || '/' || tmdb_id || '/' || tvdb_id ||" +
				" '/' || profile_url FROM persons ORDER BY name",
		)
	}

	credits := func() []string {
		GinkgoHelper()
		return queryStrings(
			"SELECT p.name || '/' || c.`character` || '/' || c.`order` ||" +
				" '/' || COALESCE(m.title, t.title)" +
				" FROM credits c" +
				" JOIN persons p ON p.id = c.person_credits" +
				" LEFT JOIN movies m ON m.id = c.movie_credits" +
				" LEFT JOIN tv_shows t ON t.id = c.tv_show_credits" +
				" ORDER BY 1",
		)
	}

	It("keeps two id-less series actors apart", func() {
		// The shape TVDB cast is stored in: no tmdb_id worth anything and no
		// tvdb_id key at all, since the field postdates these rows. Keying on
		// tmdb_id collapsed every such actor in the library into one person.
		seedShow(1, "Gamma", 3, `[{"tmdb_id":0,"name":"Zachary Chasseriaud",`+
			`"character":"Léo","profile_url":""}]`)
		seedShow(2, "Delta", 4, `[{"tmdb_id":0,"name":"Nina Meurisse",`+
			`"character":"Claire","profile_url":""}]`)

		Expect(runMigrations(context.Background(), sqlDB)).To(Succeed())

		Expect(people()).To(ConsistOf(
			"Nina Meurisse/0/0/",
			"Zachary Chasseriaud/0/0/",
		))
		Expect(credits()).To(ConsistOf(
			"Nina Meurisse/Claire/0/Delta",
			"Zachary Chasseriaud/Léo/0/Gamma",
		))
	})

	It("merges one person across movies and series and keeps billing order", func() {
		seedMovie(1, "Alpha", 11, `[{"tmdb_id":10,"name":"Ada Lovelace",`+
			`"character":"Herself","profile_url":""},`+
			`{"tmdb_id":20,"name":"Bob Stone","character":"Bob",`+
			`"profile_url":"https://img/bob.jpg"}]`)
		seedMovie(2, "Beta", 12, `[{"tmdb_id":10,"name":"Ada Lovelace",`+
			`"character":"Narrator","profile_url":"https://img/ada.jpg"}]`)
		seedShow(3, "Gamma", 13, `[{"tmdb_id":0,"name":"Ada Lovelace",`+
			`"character":"The Analyst","profile_url":""}]`)

		Expect(runMigrations(context.Background(), sqlDB)).To(Succeed())

		// The series entry carries no id, so it keys on the name and lands on
		// the same person the tmdb-keyed movie entries did.
		Expect(people()).To(ConsistOf(
			"Ada Lovelace/10/0/https://img/ada.jpg",
			"Bob Stone/20/0/https://img/bob.jpg",
		))
		Expect(credits()).To(ConsistOf(
			"Ada Lovelace/Herself/0/Alpha",
			"Ada Lovelace/Narrator/0/Beta",
			"Ada Lovelace/The Analyst/0/Gamma",
			"Bob Stone/Bob/1/Alpha",
		))
	})

	It("folds case and punctuation when keying on the name", func() {
		seedShow(1, "Gamma", 3, `[{"tmdb_id":0,"name":"Ada M. Lovelace",`+
			`"character":"Herself","profile_url":""}]`)
		seedShow(2, "Delta", 4, `[{"tmdb_id":0,"name":"ada m lovelace",`+
			`"character":"The Analyst","profile_url":"https://img/ada.jpg"}]`)

		Expect(runMigrations(context.Background(), sqlDB)).To(Succeed())

		Expect(people()).To(HaveLen(1))
		Expect(credits()).To(HaveLen(2))
	})

	It("skips a nameless entry and a title whose cast was never written", func() {
		seedMovie(1, "Alpha", 11, nil)
		seedMovie(2, "Beta", 12, `[]`)
		seedShow(3, "Gamma", 3, `[{"tmdb_id":0,"name":"","character":"Extra"}]`)

		Expect(runMigrations(context.Background(), sqlDB)).To(Succeed())

		Expect(people()).To(BeEmpty())
		Expect(credits()).To(BeEmpty())
	})

	It("empties both tables on the way down", func() {
		seedMovie(1, "Alpha", 11, `[{"tmdb_id":10,"name":"Ada Lovelace",`+
			`"character":"Herself","profile_url":""}]`)
		Expect(runMigrations(context.Background(), sqlDB)).To(Succeed())
		Expect(people()).To(HaveLen(1))

		src, err := iofs.New(migrationsFS, "migrations")
		Expect(err).NotTo(HaveOccurred())
		drv, err := sqlite.WithInstance(sqlDB, &sqlite.Config{})
		Expect(err).NotTo(HaveOccurred())
		m, err := migrate.NewWithInstance("iofs", src, "sqlite", drv)
		Expect(err).NotTo(HaveOccurred())
		previous, err := src.Prev(backfillPersonCreditsVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(m.Migrate(previous)).To(Succeed())

		Expect(people()).To(BeEmpty())
		Expect(credits()).To(BeEmpty())
	})
})

// dropCastJSONVersion rebuilds movies and tv_shows to drop their cast columns.
// SQLite cannot drop a column in place, so Atlas emits CREATE/INSERT/DROP/RENAME
// — and the DROP is what makes this spec necessary.
const dropCastJSONVersion = 20260910104500

var _ = Describe("runMigrations against a table rebuild",
	Label("integration", "db"), func() {
		var sqlDB *sql.DB

		// Mirrors the production DSN and pool: foreign keys enforced, one
		// connection. Both matter — enforcement is what cascades, and the single
		// connection is what makes disabling it around the run hold.
		BeforeEach(func() {
			var err error
			sqlDB, err = sql.Open("sqlite", "file:"+
				filepath.Join(GinkgoT().TempDir(), "rebuild.db")+
				"?_pragma=foreign_keys(1)")
			Expect(err).NotTo(HaveOccurred())
			sqlDB.SetMaxOpenConns(1)
			DeferCleanup(func() { Expect(sqlDB.Close()).To(Succeed()) })
		})

		counts := func() map[string]int {
			GinkgoHelper()
			rows, err := sqlDB.Query(`
				SELECT 'movies', count(*) FROM movies
				UNION ALL SELECT 'tv_shows', count(*) FROM tv_shows
				UNION ALL SELECT 'seasons', count(*) FROM seasons
				UNION ALL SELECT 'episodes', count(*) FROM episodes
				UNION ALL SELECT 'media_files', count(*) FROM media_files
				UNION ALL SELECT 'download_records', count(*) FROM download_records
				UNION ALL SELECT 'media_events', count(*) FROM media_events
				UNION ALL SELECT 'credits', count(*) FROM credits
				UNION ALL SELECT 'transcode_jobs', count(*) FROM transcode_jobs`)
			Expect(err).NotTo(HaveOccurred())
			defer rows.Close()
			out := map[string]int{}
			for rows.Next() {
				var table string
				var n int
				Expect(rows.Scan(&table, &n)).To(Succeed())
				out[table] = n
			}
			Expect(rows.Err()).NotTo(HaveOccurred())
			return out
		}

		It("keeps every row that cascades off movies and tv_shows", func() {
			src, err := iofs.New(migrationsFS, "migrations")
			Expect(err).NotTo(HaveOccurred())
			previous, err := src.Prev(dropCastJSONVersion)
			Expect(err).NotTo(HaveOccurred())
			drv, err := sqlite.WithInstance(sqlDB, &sqlite.Config{})
			Expect(err).NotTo(HaveOccurred())
			m, err := migrate.NewWithInstance("iofs", src, "sqlite", drv)
			Expect(err).NotTo(HaveOccurred())
			Expect(m.Migrate(previous)).To(Succeed())

			exec := func(query string) {
				GinkgoHelper()
				_, err := sqlDB.Exec(query)
				Expect(err).To(Succeed())
			}
			exec(`INSERT INTO movies
		      (id, create_time, update_time, title, original_title, year, tmdb_id)
		      VALUES (1, datetime('now'), datetime('now'), 'Alpha', 'Alpha', 2020, 11)`)
			exec(`INSERT INTO tv_shows
		      (id, create_time, update_time, title, year, tvdb_id)
		      VALUES (1, datetime('now'), datetime('now'), 'Beta', 2021, 22)`)
			exec(`INSERT INTO seasons
		      (id, create_time, update_time, number, tv_show_seasons)
		      VALUES (1, datetime('now'), datetime('now'), 1, 1)`)
			exec(`INSERT INTO episodes
		      (id, create_time, update_time, number, season_episodes)
		      VALUES (1, datetime('now'), datetime('now'), 1, 1)`)
			exec(`INSERT INTO media_files
		      (id, create_time, update_time, path, size, movie_media_files)
		      VALUES (1, datetime('now'), datetime('now'), '/m/a.mkv', 1, 1)`)
			exec(`INSERT INTO media_files
		      (id, create_time, update_time, path, size, episode_media_files)
		      VALUES (2, datetime('now'), datetime('now'), '/t/b.mkv', 1, 1)`)
			exec(`INSERT INTO download_records
		      (id, create_time, update_time, title, movie_download_records)
		      VALUES (1, datetime('now'), datetime('now'), 'Alpha.2020', 1)`)
			exec(`INSERT INTO media_events
		      (id, create_time, update_time, type, movie_events)
		      VALUES (1, datetime('now'), datetime('now'), 'imported', 1)`)
			exec(`INSERT INTO persons
		      (id, create_time, update_time, name)
		      VALUES (1, datetime('now'), datetime('now'), 'Gamma')`)
			exec(`INSERT INTO credits
		      (id, create_time, update_time, person_credits, movie_credits)
		      VALUES (1, datetime('now'), datetime('now'), 1, 1)`)
			exec(`INSERT INTO credits
		      (id, create_time, update_time, person_credits, tv_show_credits)
		      VALUES (2, datetime('now'), datetime('now'), 1, 1)`)
			exec(`INSERT INTO transcode_jobs
		      (id, create_time, update_time, media_file_transcode_jobs)
		      VALUES (1, datetime('now'), datetime('now'), 1)`)

			Expect(runMigrations(context.Background(), sqlDB)).To(Succeed())

			By("leaving the rebuilt parents and everything hanging off them intact")
			Expect(counts()).To(Equal(map[string]int{
				"movies":           1,
				"tv_shows":         1,
				"seasons":          1,
				"episodes":         1,
				"media_files":      2,
				"download_records": 1,
				"media_events":     1,
				"credits":          2,
				"transcode_jobs":   1,
			}))

			By("restoring enforcement once the run is over")
			_, err = sqlDB.Exec(`INSERT INTO media_files
		      (id, create_time, update_time, path, size, movie_media_files)
		      VALUES (3, datetime('now'), datetime('now'), '/m/c.mkv', 1, 999)`)
			Expect(err).To(HaveOccurred())
		})
	})

// downloadRecordEpisodesVersion links every download record to the episodes it
// covers, from the anchor column and the wanted_episodes JSON that the next
// migration drops — so a record the backfill misses loses its episodes for
// good.
const downloadRecordEpisodesVersion = 20261001222021

var _ = Describe(
	"download record episodes backfill",
	Label("integration", "db"),
	func() {
		var sqlDB *sql.DB

		BeforeEach(func() {
			var err error
			sqlDB, err = sql.Open(
				"sqlite",
				"file:"+filepath.Join(GinkgoT().TempDir(), "episodes.db"),
			)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(sqlDB.Close()).To(Succeed()) })

			src, err := iofs.New(migrationsFS, "migrations")
			Expect(err).NotTo(HaveOccurred())
			previous, err := src.Prev(downloadRecordEpisodesVersion)
			Expect(err).NotTo(HaveOccurred())
			drv, err := sqlite.WithInstance(sqlDB, &sqlite.Config{})
			Expect(err).NotTo(HaveOccurred())
			m, err := migrate.NewWithInstance("iofs", src, "sqlite", drv)
			Expect(err).NotTo(HaveOccurred())
			Expect(m.Migrate(previous)).To(Succeed())

			for id := 1; id <= 3; id++ {
				_, err := sqlDB.Exec(
					"INSERT INTO episodes (id, create_time, update_time, number,"+
						" season_episodes) VALUES (?, datetime('now'),"+
						" datetime('now'), ?, 1)",
					id, id,
				)
				Expect(err).NotTo(HaveOccurred())
			}
		})

		seedRecord := func(id int, anchor, wanted any) {
			GinkgoHelper()
			_, err := sqlDB.Exec(
				"INSERT INTO download_records (id, create_time, update_time,"+
					" title, episode_download_records, wanted_episodes)"+
					" VALUES (?, datetime('now'), datetime('now'), 't', ?, ?)",
				id, anchor, wanted,
			)
			Expect(err).NotTo(HaveOccurred())
		}

		links := func() []string {
			GinkgoHelper()
			rows, err := sqlDB.Query(
				"SELECT download_record_id || '/' || episode_id" +
					" FROM download_record_episodes ORDER BY 1",
			)
			Expect(err).NotTo(HaveOccurred())
			defer rows.Close()
			var out []string
			for rows.Next() {
				var s string
				Expect(rows.Scan(&s)).To(Succeed())
				out = append(out, s)
			}
			Expect(rows.Err()).NotTo(HaveOccurred())
			return out
		}

		It("links each record's anchor and grab-time set", func() {
			seedRecord(1, 1, `[1,2,3]`) // a season pack
			seedRecord(2, 2, nil)       // a record that never wrote a set
			seedRecord(3, 3, `[2]`)     // a set that omits its own anchor
			seedRecord(4, nil, nil)     // a movie record

			Expect(runMigrations(context.Background(), sqlDB)).To(Succeed())

			Expect(links()).To(ConsistOf("1/1", "1/2", "1/3", "2/2", "3/2", "3/3"))
		})

		It("skips an id the set names for an episode since deleted", func() {
			seedRecord(1, 1, `[1,99]`)

			Expect(runMigrations(context.Background(), sqlDB)).To(Succeed())

			Expect(links()).To(ConsistOf("1/1"))
			var dangling int
			Expect(sqlDB.QueryRow(
				"SELECT count(*) FROM pragma_foreign_key_check('download_record_episodes')",
			).Scan(&dangling)).To(Succeed())
			Expect(dangling).To(BeZero())
		})

		It("carries the set back into wanted_episodes on the way down", func() {
			seedRecord(1, 1, `[1,2]`)
			Expect(runMigrations(context.Background(), sqlDB)).To(Succeed())

			src, err := iofs.New(migrationsFS, "migrations")
			Expect(err).NotTo(HaveOccurred())
			drv, err := sqlite.WithInstance(sqlDB, &sqlite.Config{})
			Expect(err).NotTo(HaveOccurred())
			m, err := migrate.NewWithInstance("iofs", src, "sqlite", drv)
			Expect(err).NotTo(HaveOccurred())
			Expect(m.Migrate(downloadRecordEpisodesVersion)).To(Succeed())

			var wanted string
			Expect(sqlDB.QueryRow(
				"SELECT (SELECT json_group_array(value) FROM" +
					" (SELECT value FROM json_each(wanted_episodes) ORDER BY value))" +
					" FROM download_records WHERE id = 1",
			).Scan(&wanted)).To(Succeed())
			Expect(wanted).To(Equal("[1,2]"))
		})
	},
)

// replaceModeVersion turned download_records.replace_existing into the
// replace_mode enum with a table rebuild, whose generated down step dropped a
// temp table that no longer existed.
const replaceModeVersion = 20260825181930

var _ = Describe("replace_mode migration", Label("integration", "db"), func() {
	It("folds replace_mode back into replace_existing on the way down", func() {
		sqlDB, err := sql.Open(
			"sqlite",
			"file:"+filepath.Join(GinkgoT().TempDir(), "replace.db"),
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(sqlDB.Close()).To(Succeed()) })

		src, err := iofs.New(migrationsFS, "migrations")
		Expect(err).NotTo(HaveOccurred())
		drv, err := sqlite.WithInstance(sqlDB, &sqlite.Config{})
		Expect(err).NotTo(HaveOccurred())
		m, err := migrate.NewWithInstance("iofs", src, "sqlite", drv)
		Expect(err).NotTo(HaveOccurred())
		Expect(m.Migrate(replaceModeVersion)).To(Succeed())

		_, err = sqlDB.Exec(
			"INSERT INTO download_records (id, create_time, update_time, title," +
				" replace_mode) VALUES" +
				" (1, datetime('now'), datetime('now'), 'a', 'all')," +
				" (2, datetime('now'), datetime('now'), 'b', 'upgrades')," +
				" (3, datetime('now'), datetime('now'), 'c', 'none')",
		)
		Expect(err).NotTo(HaveOccurred())

		previous, err := src.Prev(replaceModeVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(m.Migrate(previous)).To(Succeed())

		rows, err := sqlDB.Query(
			"SELECT id, replace_existing FROM download_records ORDER BY id",
		)
		Expect(err).NotTo(HaveOccurred())
		defer rows.Close()
		got := map[int]bool{}
		for rows.Next() {
			var id int
			var replace bool
			Expect(rows.Scan(&id, &replace)).To(Succeed())
			got[id] = replace
		}
		Expect(rows.Err()).NotTo(HaveOccurred())
		Expect(got).To(Equal(map[int]bool{1: true, 2: false, 3: false}))
	})
})

const albumTagsManagedVersion = 20261013122718

var _ = Describe("album tags_managed backfill", Label("integration", "db"), func() {
	var (
		sqlDB *sql.DB
		m     *migrate.Migrate
	)

	BeforeEach(func() {
		var err error
		sqlDB, err = sql.Open(
			"sqlite",
			"file:"+filepath.Join(GinkgoT().TempDir(), "tags.db"),
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(sqlDB.Close()).To(Succeed()) })
		src, err := iofs.New(migrationsFS, "migrations")
		Expect(err).NotTo(HaveOccurred())
		previous, err := src.Prev(albumTagsManagedVersion)
		Expect(err).NotTo(HaveOccurred())
		drv, err := sqlite.WithInstance(sqlDB, &sqlite.Config{})
		Expect(err).NotTo(HaveOccurred())
		m, err = migrate.NewWithInstance("iofs", src, "sqlite", drv)
		Expect(err).NotTo(HaveOccurred())
		Expect(m.Migrate(previous)).To(Succeed())
	})

	exec := func(q string, args ...any) {
		GinkgoHelper()
		_, err := sqlDB.Exec(q, args...)
		Expect(err).NotTo(HaveOccurred())
	}

	// album seeds an album with one track per file source given.
	album := func(id int, sources ...string) {
		GinkgoHelper()
		exec(
			"INSERT INTO albums (id, create_time, update_time, mbid, title, artist_albums)"+
				" VALUES (?, datetime('now'), datetime('now'), ?, 'A', 1)",
			id,
			fmt.Sprint("rg-", id),
		)
		for i, src := range sources {
			tid := id*10 + i
			exec(
				"INSERT INTO tracks (id, create_time, update_time, title, disc, position,"+
					" album_tracks) VALUES (?, datetime('now'), datetime('now'), 'T', 1, ?, ?)",
				tid,
				i+1,
				id,
			)
			exec(
				"INSERT INTO media_files (create_time, update_time, path, size, source,"+
					" track_media_files) VALUES (datetime('now'), datetime('now'), ?, 1, ?, ?)",
				fmt.Sprint("/m/", tid),
				src,
				tid,
			)
		}
	}

	managed := func(id int) bool {
		GinkgoHelper()
		var v bool
		Expect(sqlDB.QueryRow("SELECT tags_managed FROM albums WHERE id = ?", id).
			Scan(&v)).To(Succeed())
		return v
	}

	It("leaves only albums made entirely of adopted files unmanaged", func() {
		exec(
			"INSERT INTO artists (id, create_time, update_time, mbid, name, monitor)" +
				" VALUES (1, datetime('now'), datetime('now'), 'art', 'X', 'all')",
		)
		album(1, "orphan", "wizard")
		album(2, "orphan", "auto")
		album(3, "auto")
		album(4)

		Expect(m.Migrate(albumTagsManagedVersion)).To(Succeed())

		Expect(managed(1)).To(BeFalse())
		Expect(managed(2)).To(BeTrue())
		Expect(managed(3)).To(BeTrue())
		Expect(managed(4)).To(BeTrue())
	})
})

// Every down step used to be Atlas's own reverse, and for a table rebuild that
// is only "DROP TABLE new_<table>", a table that no longer exists by then:
// nine migrations could not be rolled back past, and the init one dropped
// parent tables under enforced foreign keys. ent/migrate now checks each down
// against the replayed schema; this runs the whole chain the way a rollback
// would, with foreign keys on and rows in the rebuilt tables.
var _ = Describe("down migrations", Label("integration", "db"), func() {
	It("roll a populated database all the way back", func() {
		sqlDB, err := sql.Open(
			"sqlite",
			"file:"+filepath.Join(GinkgoT().TempDir(), "down.db")+
				"?_pragma=foreign_keys(1)",
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(sqlDB.Close()).To(Succeed()) })
		sqlDB.SetMaxOpenConns(1)

		src, err := iofs.New(migrationsFS, "migrations")
		Expect(err).NotTo(HaveOccurred())
		drv, err := sqlite.WithInstance(sqlDB, &sqlite.Config{})
		Expect(err).NotTo(HaveOccurred())
		m, err := migrate.NewWithInstance("iofs", src, "sqlite", drv)
		Expect(err).NotTo(HaveOccurred())
		Expect(runMigrations(context.Background(), sqlDB)).To(Succeed())

		_, err = sqlDB.Exec(
			"INSERT INTO movies (id, create_time, update_time, title," +
				" original_title, year, tmdb_id) VALUES" +
				" (1, datetime('now'), datetime('now'), 'm', 'm', 2026, 1)",
		)
		Expect(err).NotTo(HaveOccurred())
		_, err = sqlDB.Exec(
			"INSERT INTO download_records (id, create_time, update_time, title," +
				" movie_download_records, rejected) VALUES" +
				" (1, datetime('now'), datetime('now'), 'r', 1, true)",
		)
		Expect(err).NotTo(HaveOccurred())

		// As runMigrations does it: a file's own PRAGMA foreign_keys is a no-op
		// inside golang-migrate's per-file transaction, so enforcement goes off
		// on the one connection first.
		_, err = sqlDB.Exec("PRAGMA foreign_keys = off")
		Expect(err).NotTo(HaveOccurred())
		Expect(m.Down()).To(Succeed())

		var tables int
		Expect(sqlDB.QueryRow(
			"SELECT count(*) FROM sqlite_master WHERE type = 'table'" +
				" AND name NOT IN ('schema_migrations', 'sqlite_sequence')",
		).Scan(&tables)).To(Succeed())
		Expect(tables).To(BeZero())
	})
})
