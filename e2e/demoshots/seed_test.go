package main

import (
	"database/sql"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// season builds a season the way GET /series/{id} answers it: a season
// without episodes carries no "episodes" key at all.
func season(number int, episodeIDs ...int) obj {
	out := obj{"number": float64(number)}
	if len(episodeIDs) > 0 {
		eps := make(list, 0, len(episodeIDs))
		for _, id := range episodeIDs {
			eps = append(eps, obj{"id": float64(id)})
		}
		out["episodes"] = eps
	}
	return out
}

var _ = Describe("calendarEpisodes", Label("unit", "demoshots"), func() {
	It("takes the first five episodes of the last season", func() {
		pick, err := calendarEpisodes(obj{"seasons": list{
			season(0, 1),
			season(1, 10, 11),
			season(2, 20, 21, 22, 23, 24, 25, 26),
		}})
		Expect(err).NotTo(HaveOccurred())
		Expect(pick.season).To(Equal(2))
		Expect(pick.episodes).To(Equal([]int{20, 21, 22, 23, 24}))
		Expect(pick.empty).To(BeEmpty())
	})

	It("passes over trailing seasons that have no episode, and says which", func() {
		pick, err := calendarEpisodes(obj{"seasons": list{
			season(1, 10, 11),
			season(2),
			season(3),
		}})
		Expect(err).NotTo(HaveOccurred())
		Expect(pick.season).To(Equal(1))
		Expect(pick.episodes).To(Equal([]int{10, 11}))
		Expect(pick.empty).To(Equal([]int{3, 2}))
	})

	It("fails on a show with seasons but no episode", func() {
		_, err := calendarEpisodes(obj{"seasons": list{season(1), season(2)}})
		Expect(err).To(MatchError(ContainSubstring("none of its 2 seasons")))
	})

	It("fails on a show with no season", func() {
		_, err := calendarEpisodes(obj{})
		Expect(err).To(MatchError(ContainSubstring("none of its 0 seasons")))
	})
})

var _ = Describe("setDate", Label("unit", "demoshots"), func() {
	const update = `UPDATE episodes SET air_date = ? WHERE id = ?`

	var db *sql.DB

	BeforeEach(func(ctx SpecContext) {
		var err error
		db, err = sql.Open(
			"sqlite",
			"file:"+filepath.Join(GinkgoT().TempDir(), "streamline.db"),
		)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(db.Close)
		_, err = db.ExecContext(
			ctx,
			`CREATE TABLE episodes (id INTEGER PRIMARY KEY, air_date DATETIME);
			 INSERT INTO episodes (id) VALUES (7);`,
		)
		Expect(err).NotTo(HaveOccurred())
	})

	It("dates the row", func(ctx SpecContext) {
		when := time.Date(2026, 11, 4, 20, 0, 0, 0, time.UTC)
		Expect(setDate(ctx, db, update, when, 7)).To(Succeed())

		var dated int
		Expect(db.QueryRowContext(
			ctx,
			`SELECT count(*) FROM episodes WHERE id = 7 AND air_date IS NOT NULL`,
		).Scan(&dated)).To(Succeed())
		Expect(dated).To(Equal(1))
	})

	It("fails when the id matches no row", func(ctx SpecContext) {
		err := setDate(ctx, db, update, time.Now(), 0)
		Expect(err).To(MatchError(ContainSubstring("matched 0 rows")))
	})
})
