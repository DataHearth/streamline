package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/user"
	"github.com/datahearth/streamline/internal/role"
)

var _ = Describe("Extension view catalogue", Label("integration", "db"), func() {
	var (
		ctx    context.Context
		client *ent.Client
		conn   *sql.Conn
	)

	BeforeEach(func() {
		ctx = context.Background()
		dir := GinkgoT().TempDir()
		corePath := filepath.Join(dir, "core.db")

		var err error
		client, err = Open(ctx, corePath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { client.Close() })

		_, err = New(client).CreateUser(ctx, CreateUserParams{
			Email:        "Reader@Example.com",
			DisplayName:  "Reader",
			PasswordHash: "secret-hash",
			Role:         role.Seed(user.RoleAdmin),
			AuthMethod:   "local",
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = client.MediaFile.Create().
			SetPath("/library/books/secret/path.epub").SetSize(10).Save(ctx)
		Expect(err).NotTo(HaveOccurred())

		ext, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "ext.db"))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { ext.Close() })
		conn, err = ext.Conn(ctx)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { conn.Close() })
		_, err = conn.ExecContext(ctx,
			"ATTACH DATABASE 'file:"+corePath+"?mode=ro' AS core")
		Expect(err).NotTo(HaveOccurred())
	})

	It("lists views by name", func() {
		for _, v := range ExtensionViews() {
			got, ok := ExtensionViewByName(v.Name)
			Expect(ok).To(BeTrue(), v.Name)
			Expect(got.Name).To(Equal(v.Name))
			Expect(v.Description).NotTo(BeEmpty(), v.Name)
			Expect(v.Tables).NotTo(BeEmpty(), v.Name)
		}
		_, ok := ExtensionViewByName("sessions")
		Expect(ok).To(BeFalse())
	})

	It("exposes exactly the declared columns, in order, and no secrets", func() {
		forbidden := []string{
			"password_hash", "email", "password", "path",
			"device_email",
		}
		allowed := map[string]string{
			"user_logins|email": "",
			"music_files|path":  "",
			"video_files|path":  "",
		}
		for _, v := range ExtensionViews() {
			_, err := conn.ExecContext(ctx,
				"CREATE TEMP VIEW streamline_"+v.Name+" AS "+v.SQL)
			Expect(err).NotTo(HaveOccurred(), v.Name)

			rows, err := conn.QueryContext(
				ctx,
				"SELECT * FROM streamline_"+v.Name+" LIMIT 0", //nolint:gosec // view names come from the catalogue
			)
			Expect(err).NotTo(HaveOccurred(), v.Name)
			cols, err := rows.Columns()
			Expect(err).NotTo(HaveOccurred())
			Expect(rows.Close()).To(Succeed())
			Expect(cols).To(Equal(v.Columns), v.Name)
			Expect(v.Columns).To(Equal(cols))

			for _, c := range v.Columns {
				if _, ok := allowed[v.Name+"|"+c]; ok {
					continue
				}
				Expect(forbidden).NotTo(ContainElement(c), v.Name)
			}
		}
	})

	It("keeps secrets out of row values", func() {
		for _, v := range ExtensionViews() {
			_, err := conn.ExecContext(ctx,
				"CREATE TEMP VIEW streamline_"+v.Name+" AS "+v.SQL)
			Expect(err).NotTo(HaveOccurred(), v.Name)
			if v.Name == "user_logins" {
				continue
			}
			rows, err := conn.QueryContext(
				ctx,
				"SELECT * FROM streamline_"+v.Name, //nolint:gosec // view names come from the catalogue
			)
			Expect(err).NotTo(HaveOccurred())
			cols, _ := rows.Columns()
			for rows.Next() {
				vals := make([]any, len(cols))
				ptrs := make([]any, len(cols))
				for i := range vals {
					ptrs[i] = &vals[i]
				}
				Expect(rows.Scan(ptrs...)).To(Succeed())
				for _, x := range vals {
					s, _ := x.(string)
					Expect(
						strings.ToLower(s),
					).NotTo(ContainSubstring("reader@example.com"))
					Expect(s).NotTo(ContainSubstring("secret"))
					Expect(s).NotTo(ContainSubstring("hunter22"))
				}
			}
			Expect(rows.Close()).To(Succeed())
		}
	})

	It("groups every music view under one bundle", func() {
		for _, n := range []string{
			"artists", "artist_members", "albums", "album_artists", "tracks",
			"music_credits", "music_files", "genres",
		} {
			v, ok := ExtensionViewByName(n)
			Expect(ok).To(BeTrue(), n)
			Expect(v.Bundle).To(Equal("music"), n)
		}
		for _, n := range []string{"books", "book_series", "authors", "book_contributions", "book_files"} {
			v, _ := ExtensionViewByName(n)
			Expect(v.Bundle).To(Equal("books"), n)
		}
		for _, n := range []string{"movies", "series", "seasons", "episodes", "video_files"} {
			v, _ := ExtensionViewByName(n)
			Expect(v.Bundle).To(Equal("video"), n)
		}
	})

	It("marks a user locked only until the lock expires", func() {
		v, _ := ExtensionViewByName("users")
		_, err := conn.ExecContext(
			ctx,
			"CREATE TEMP VIEW streamline_users AS "+v.SQL,
		)
		Expect(err).NotTo(HaveOccurred())
		locked := func() int {
			GinkgoHelper()
			var n int
			Expect(conn.QueryRowContext(ctx,
				"SELECT locked FROM streamline_users").Scan(&n)).To(Succeed())
			return n
		}
		Expect(locked()).To(Equal(0))

		Expect(client.User.Update().
			SetLockedUntil(time.Now().Add(time.Hour)).Exec(ctx)).To(Succeed())
		Expect(locked()).To(Equal(1))

		Expect(client.User.Update().
			SetLockedUntil(time.Now().Add(-time.Hour)).Exec(ctx)).To(Succeed())
		Expect(locked()).To(Equal(0))
	})

	It("lowercases the email in user_logins", func() {
		v, _ := ExtensionViewByName("user_logins")
		_, err := conn.ExecContext(
			ctx,
			"CREATE TEMP VIEW streamline_user_logins AS "+v.SQL,
		)
		Expect(err).NotTo(HaveOccurred())
		var email string
		Expect(conn.QueryRowContext(ctx,
			"SELECT email FROM streamline_user_logins").Scan(&email)).To(Succeed())
		Expect(email).To(Equal("reader@example.com"))
	})
})
