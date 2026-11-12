package extension

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Policy", Label("unit"), func() {
	var (
		conn  *Conn
		store *Store
		ctx   = context.Background()
	)

	BeforeEach(func() {
		corePath := filepath.Join(GinkgoT().TempDir(), "core.db")
		openCore(corePath)
		store = openTestStore(corePath)
		conn = testConn(store)
	})

	allowed := func(stmt string) {
		GinkgoHelper()
		res, err := conn.Exec(ctx, stmt, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Error).To(BeEmpty(), stmt)
	}
	denied := func(stmt string) {
		GinkgoHelper()
		res, err := conn.Exec(ctx, stmt, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(
			res.Error,
		).To(MatchRegexp("not authorized|authorization denied"), stmt)
	}
	deniedQuery := func(stmt string) {
		GinkgoHelper()
		res, err := conn.Query(ctx, stmt, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(
			res.Error,
		).To(MatchRegexp("not authorized|authorization denied"), stmt)
	}

	It("lets the extension own tables in its main database", func() {
		allowed("CREATE TABLE annotations(album_id INTEGER, note TEXT)")
		allowed("INSERT INTO annotations VALUES (2, 'two'), (1, 'one')")
	})

	It("joins host views with the extension's own tables", func() {
		allowed(
			"CREATE TABLE annotations(album_id INTEGER, user_id INTEGER, note TEXT)",
		)
		allowed("INSERT INTO annotations VALUES (2, 1, 'two'), (1, 2, 'one')")

		res, err := conn.Query(ctx, `
SELECT a.title, u.name, n.note
FROM streamline_albums a
JOIN annotations n ON n.album_id = a.id
JOIN streamline_users u ON u.id = n.user_id
ORDER BY a.id`, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Error).To(BeEmpty())
		Expect(res.Rows).To(Equal([][]any{
			{"First", "bob", "one"},
			{"Second", "ann", "two"},
		}))
	})

	DescribeTable("refuses what leaks or mutates core",
		func(stmt string) { deniedQuery(stmt) },
		Entry("column outside the view", "SELECT internal_flag FROM core.albums"),
		Entry("secret column", "SELECT password_hash FROM core.users"),
		Entry(
			"secret column in a subquery",
			"SELECT (SELECT password_hash FROM core.users) FROM streamline_albums",
		),
		Entry("unqualified core table", "SELECT * FROM users"),
		Entry("core schema", "SELECT sql FROM core.sqlite_master"),
		Entry("temp schema", "SELECT sql FROM sqlite_temp_master"),
		Entry("table no view is built on", "SELECT count(*) FROM core.sessions"),
	)

	It("lets a guest index its own tables", func() {
		for _, stmt := range []string{
			"CREATE TABLE plays (user_id INTEGER, track_id INTEGER)",
			"CREATE INDEX plays_user ON plays (user_id)",
		} {
			res, err := conn.Exec(ctx, stmt, nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Error).To(BeEmpty(), stmt)
		}
	})

	It("has no view that was not granted", func() {
		res, err := conn.Query(ctx, "SELECT * FROM streamline_sessions", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Error).To(ContainSubstring("no such table"))
	})

	It("refuses to write through a view", func() {
		res, err := conn.Exec(ctx, "DELETE FROM streamline_albums", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Error).To(ContainSubstring("is a view"))
	})

	DescribeTable(
		"refuses what rewrites the sandbox",
		func(stmt string) { denied(stmt) },
		Entry("write to core", "INSERT INTO core.albums VALUES (9, 'x', 1, 'x')"),
		Entry("drop a host view", "DROP VIEW streamline_albums"),
		Entry(
			"temp view over core",
			"CREATE TEMP VIEW v AS SELECT * FROM core.users",
		),
		Entry("attach", "ATTACH DATABASE '/etc/passwd' AS p"),
		Entry("detach", "DETACH DATABASE core"),
		Entry("pragma", "PRAGMA writable_schema=1"),
		Entry(
			"main view borrowing a host view's name",
			"CREATE VIEW streamline_x AS SELECT password_hash FROM core.users",
		),
	)

	It("refuses a main trigger borrowing a host view's name", func() {
		allowed("CREATE TABLE annotations(album_id INTEGER)")
		res, err := conn.Exec(
			ctx,
			"CREATE TRIGGER streamline_t AFTER INSERT ON annotations BEGIN SELECT password_hash FROM core.users; END",
			nil,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Error).To(MatchRegexp("not authorized|authorization denied"))
	})

	It("answers a count over a view", func() {
		res, err := conn.Query(ctx, "SELECT count(*) FROM streamline_albums", nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Error).To(BeEmpty())
		Expect(res.Rows).To(Equal([][]any{{int64(2)}}))
	})

	It("counts a bounded recursive CTE", func() {
		res, err := conn.Query(
			ctx,
			"WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM c WHERE x < 10) SELECT count(*) FROM c",
			nil,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.Error).To(BeEmpty())
		Expect(res.Rows).To(Equal([][]any{{int64(10)}}))
	})

	It("lets the extension build views over its own tables and host views", func() {
		allowed("CREATE TABLE annotations(album_id INTEGER)")
		allowed(
			"CREATE VIEW annotated AS SELECT a.title FROM streamline_albums a JOIN annotations n ON n.album_id = a.id",
		)
	})
})
