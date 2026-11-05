package db

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("isRead", Label("unit", "db"), func() {
	DescribeTable(
		"routes a statement by its first word",
		func(query string, read bool) {
			Expect(isRead(query)).To(Equal(read))
		},
		Entry("an ent select", "SELECT `users`.`id` FROM `users`", true),
		Entry("lower case", "select 1", true),
		Entry("leading whitespace", "\n\t  SELECT count(*) FROM books", true),
		Entry(
			"ent's insert with RETURNING",
			"INSERT INTO `users` (`email`) VALUES (?) RETURNING `id`",
			false,
		),
		Entry("an update", "UPDATE `users` SET `email` = ?", false),
		Entry(
			"a CTE, which may write",
			"WITH x AS (SELECT 1) SELECT * FROM x",
			false,
		),
		Entry("a pragma", "PRAGMA optimize", false),
		Entry("shorter than the keyword", "SEL", false),
		Entry("empty", "", false),
	)
})
