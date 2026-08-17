package main

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/ent"
	entuser "github.com/datahearth/streamline/ent/user"
)

var _ = Describe("parseRole", Label("unit", "cli"), func() {
	It("accepts every role the schema declares", func() {
		for _, r := range []entuser.Role{
			entuser.RoleAdmin,
			entuser.RoleMember,
			entuser.RoleRequestOnly,
		} {
			got, err := parseRole(string(r))
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal(r))
		}
	})

	It("trims surrounding whitespace", func() {
		got, err := parseRole("  admin  ")
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal(entuser.RoleAdmin))
	})

	It("rejects an unknown role and names the valid ones", func() {
		_, err := parseRole("superuser")
		Expect(err).To(MatchError(ContainSubstring(`invalid role "superuser"`)))
		Expect(err).To(MatchError(ContainSubstring("admin, member, request_only")))
	})

	It("rejects an empty role", func() {
		_, err := parseRole("")
		Expect(err).To(MatchError(ContainSubstring("invalid role")))
	})
})

var _ = Describe("formatUserTable", Label("unit", "cli"), func() {
	It("renders a header and one row per user", func() {
		created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		out := formatUserTable([]*ent.User{
			{
				ID:          1,
				Email:       "admin@example.com",
				Role:        entuser.RoleAdmin,
				DisplayName: "Admin",
				CreateTime:  created,
			},
			{
				ID:         2,
				Email:      "member@example.com",
				Role:       entuser.RoleMember,
				CreateTime: created,
			},
		})
		Expect(out).To(ContainSubstring("ID"))
		Expect(out).To(ContainSubstring("DISPLAY NAME"))
		Expect(out).To(ContainSubstring("admin@example.com"))
		Expect(out).To(ContainSubstring("member@example.com"))
		Expect(out).To(ContainSubstring("2026-01-02T03:04:05Z"))
	})

	It("stands in a placeholder for a missing display name", func() {
		out := formatUserTable([]*ent.User{
			{ID: 7, Email: "nobody@example.com", Role: entuser.RoleRequestOnly},
		})
		Expect(out).To(ContainSubstring("request_only"))
		Expect(out).To(ContainSubstring("-"))
	})

	It("renders only the header for an empty listing", func() {
		Expect(formatUserTable(nil)).To(HaveSuffix("CREATED\n"))
	})
})
