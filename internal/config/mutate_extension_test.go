package config_test

import (
	"context"
	"errors"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Extension CRUD", Label("unit", "config"), func() {
	ctx := context.Background()
	hash := strings.Repeat("ab", 32)

	BeforeEach(func() { configtest.SetupFile() })

	local := func(name string) config.ExtensionEntry {
		return config.ExtensionEntry{
			Name: name, Source: "/opt/extensions/" + name + ".wasm", Enabled: true,
			Grants: config.ExtensionGrants{
				Mounts: []string{"/opds"}, Views: []string{"books"},
			},
		}
	}

	It("adds, updates and deletes", func() {
		Expect(config.AddExtension(ctx, local("opds"))).To(Succeed())
		Expect(config.Get().Extensions).To(HaveLen(1))
		Expect(config.Get().Extensions[0].Grants.Views).To(Equal([]string{"books"}))

		Expect(config.AddExtension(ctx, local("opds"))).
			To(MatchError(config.ErrExtensionExists))

		Expect(
			config.UpdateExtension(
				ctx,
				"opds",
				func(e *config.ExtensionEntry) error {
					e.Enabled = false
					e.Name = "renamed"
					return nil
				},
			),
		).To(Succeed())
		got := config.Get().Extensions[0]
		Expect(got.Enabled).To(BeFalse())
		Expect(got.Name).To(Equal("opds"))

		Expect(
			config.UpdateExtension(ctx, "nope", func(*config.ExtensionEntry) error {
				return nil
			}),
		).To(MatchError(config.ErrExtensionNotFound))

		boom := errors.New("boom")
		Expect(
			config.UpdateExtension(ctx, "opds", func(*config.ExtensionEntry) error {
				return boom
			}),
		).To(MatchError(boom))

		Expect(config.DeleteExtension(ctx, "opds")).To(Succeed())
		Expect(config.Get().Extensions).To(BeEmpty())
		Expect(config.DeleteExtension(ctx, "opds")).
			To(MatchError(config.ErrExtensionNotFound))
	})

	DescribeTable("refuses an invalid entry",
		func(e config.ExtensionEntry) {
			Expect(config.AddExtension(ctx, e)).NotTo(Succeed())
			Expect(config.Get().Extensions).To(BeEmpty())
		},
		Entry("an uppercase name", config.ExtensionEntry{
			Name: "OPDS", Source: "/a.wasm",
		}),
		Entry("a leading dash", config.ExtensionEntry{
			Name: "-opds", Source: "/a.wasm",
		}),
		Entry("a name over 32 characters", config.ExtensionEntry{
			Name: strings.Repeat("a", 33), Source: "/a.wasm",
		}),
		Entry("an http source", config.ExtensionEntry{
			Name:   "opds",
			Source: "http://example.com/a.wasm",
			SHA256: strings.Repeat("ab", 32),
		}),
		Entry("an https source without sha256", config.ExtensionEntry{
			Name: "opds", Source: "https://example.com/a.wasm",
		}),
		Entry("a relative path", config.ExtensionEntry{
			Name: "opds", Source: "a.wasm",
		}),
		Entry("a short sha256", config.ExtensionEntry{
			Name: "opds", Source: "https://example.com/a.wasm", SHA256: "abcd",
		}),
	)

	It("accepts an https source with sha256", func() {
		Expect(config.AddExtension(ctx, config.ExtensionEntry{
			Name: "opds", Source: "https://example.com/a.wasm", SHA256: hash,
		})).To(Succeed())
	})

	It("refuses an update that breaks a rule", func() {
		Expect(config.AddExtension(ctx, local("opds"))).To(Succeed())
		Expect(
			config.UpdateExtension(
				ctx,
				"opds",
				func(e *config.ExtensionEntry) error {
					e.Source = "http://example.com/a.wasm"
					return nil
				},
			),
		).NotTo(Succeed())
		Expect(config.Get().Extensions[0].Source).To(HavePrefix("/opt"))
	})
})
