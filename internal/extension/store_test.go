package extension

import (
	"context"
	"database/sql"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	_ "modernc.org/sqlite"
)

var testViews = []View{
	{
		Name:   "albums",
		SQL:    "SELECT id, title, year FROM core.albums",
		Tables: []string{"albums"},
	},
	{
		Name:   "users",
		SQL:    "SELECT id, name FROM core.users",
		Tables: []string{"users"},
	},
}

const coreSchemaSQL = `
CREATE TABLE users(id INTEGER PRIMARY KEY, name TEXT, password_hash TEXT);
CREATE TABLE albums(id INTEGER PRIMARY KEY, title TEXT, year INTEGER, internal_flag TEXT);
CREATE TABLE sessions(id INTEGER PRIMARY KEY, token TEXT);
INSERT INTO users VALUES (1, 'ann', 'secret'), (2, 'bob', 'secret');
INSERT INTO albums VALUES (1, 'First', 2001, 'x'), (2, 'Second', 2002, 'y');
INSERT INTO sessions VALUES (1, 'tok');`

func openCore(path string) *sql.DB {
	GinkgoHelper()
	db, err := sql.Open(
		"sqlite",
		"file:"+uriEscaper.Replace(path)+
			"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
	)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(db.Close)
	_, err = db.Exec(coreSchemaSQL)
	Expect(err).NotTo(HaveOccurred())
	return db
}

func openTestStore(coreDB string) *Store {
	GinkgoHelper()
	store, err := OpenStore(
		context.Background(),
		filepath.Join(GinkgoT().TempDir(), "ext", "data.db"),
		coreDB,
		testViews,
	)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(store.Close)
	return store
}

func testConn(store *Store) *Conn {
	GinkgoHelper()
	conn, err := store.Conn(context.Background())
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(conn.Release)
	return conn
}

var _ = Describe("Store", Label("unit"), func() {
	var (
		core     *sql.DB
		corePath string
		store    *Store
	)

	BeforeEach(func() {
		corePath = filepath.Join(GinkgoT().TempDir(), "core.db")
		core = openCore(corePath)
		store = openTestStore(corePath)
	})

	It("sees a core commit on the next statement", func() {
		conn := testConn(store)
		_, err := core.Exec("INSERT INTO albums VALUES (3, 'Third', 2003, 'z')")
		Expect(err).NotTo(HaveOccurred())

		res, err := conn.Query(context.Background(),
			"SELECT count(*) FROM streamline_albums", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Error).To(BeEmpty())
		Expect(res.Rows).To(Equal([][]any{{int64(3)}}))
	})

	It("does not hold up core's writer behind an open transaction", func() {
		conn := testConn(store)
		ctx := context.Background()
		for _, stmt := range []string{
			"CREATE TABLE annotations(id INTEGER PRIMARY KEY, note TEXT)",
			"INSERT INTO annotations VALUES (1, 'a')",
			"BEGIN",
			"UPDATE annotations SET note = 'b'",
		} {
			res, err := conn.Exec(ctx, stmt, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Error).To(BeEmpty(), stmt)
		}

		start := time.Now()
		_, err := core.Exec("INSERT INTO albums VALUES (3, 'Third', 2003, 'z')")
		Expect(err).NotTo(HaveOccurred())
		Expect(time.Since(start)).To(BeNumerically("<", time.Second))
	})

	It("interrupts a runaway query and stays usable", func() {
		conn := testConn(store)
		ctx, cancel := context.WithTimeout(
			context.Background(),
			200*time.Millisecond,
		)
		defer cancel()

		start := time.Now()
		res, err := conn.Query(
			ctx,
			"WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM c) SELECT count(*) FROM c",
			nil,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Error).NotTo(BeEmpty())
		Expect(time.Since(start)).To(BeNumerically("<", 3*time.Second))

		res, err = conn.Query(context.Background(), "SELECT 1", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Error).To(BeEmpty())
	})

	It("rolls back an open transaction on release", func() {
		ctx := context.Background()
		first, err := store.Conn(ctx)
		Expect(err).NotTo(HaveOccurred())
		for _, stmt := range []string{
			"CREATE TABLE annotations(id INTEGER PRIMARY KEY)",
			"BEGIN",
			"INSERT INTO annotations VALUES (1)",
		} {
			res, err := first.Exec(ctx, stmt, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Error).To(BeEmpty(), stmt)
		}
		Expect(first.Release()).To(Succeed())

		second := testConn(store)
		res, err := second.Query(ctx, "SELECT count(*) FROM annotations", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Error).To(BeEmpty())
		Expect(res.Rows).To(Equal([][]any{{int64(0)}}))
	})

	It("attaches a core path holding a single quote", func() {
		quoted := filepath.Join(GinkgoT().TempDir(), "it's.db")
		openCore(quoted)
		conn := testConn(openTestStore(quoted))

		res, err := conn.Query(context.Background(),
			"SELECT title FROM streamline_albums ORDER BY id", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Error).To(BeEmpty())
		Expect(res.Rows).To(Equal([][]any{{"First"}, {"Second"}}))
	})

	DescribeTable("refuses an in-memory core database",
		func(path string) {
			_, err := OpenStore(context.Background(),
				filepath.Join(GinkgoT().TempDir(), "data.db"), path, testViews)
			Expect(err).To(MatchError(ErrCoreInMemory))
		},
		Entry(":memory:", ":memory:"),
		Entry("shared memory URI", "file:mem_x?mode=memory"),
	)

	It("fails to open when the core database is missing", func() {
		_, err := OpenStore(context.Background(),
			filepath.Join(GinkgoT().TempDir(), "data.db"),
			filepath.Join(GinkgoT().TempDir(), "absent.db"), testViews)
		Expect(err).To(HaveOccurred())
	})

	It("refuses a value or a result past the response cap", func() {
		ctx := context.Background()
		conn := testConn(store)

		res, err := conn.Query(ctx, "SELECT zeroblob(300000000)", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Error).NotTo(BeEmpty())

		res, err = conn.Query(ctx, `WITH RECURSIVE n(i) AS (
			SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 40
		) SELECT zeroblob(1000000) FROM n`, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Error).To(Equal(errResultTooLarge))
	})
})
