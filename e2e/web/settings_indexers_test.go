package web

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/testutil/cardiganntest"
)

// newReadOnlyPage opens path as the seed admin with /auth/config answering
// read_only. The suite's one app owns the process-wide config, and the YAML
// a read-only form shows is built in the browser, so only the SPA has to
// believe it. noCommit makes the catalog answer as one not read yet.
func newReadOnlyPage(path string, noCommit bool) *rod.Page {
	GinkgoHelper()
	b := newContext()
	Expect(b.SetCookies([]*proto.NetworkCookieParam{{
		Name:     auth.SessionCookie,
		Value:    sessionJWT,
		URL:      baseURL,
		Path:     "/",
		HTTPOnly: true,
	}})).To(Succeed())
	router := b.HijackRequests()
	router.MustAdd(baseURL+"/auth/config", func(h *rod.Hijack) {
		h.MustLoadResponse()
		var body map[string]any
		Expect(json.Unmarshal([]byte(h.Response.Body()), &body)).To(Succeed())
		body["read_only"] = true
		h.Response.SetBody(body)
	})
	if noCommit {
		router.MustAdd(baseURL+"/api/v1/cardigann/updates", func(h *rod.Hijack) {
			h.Response.SetBody(map[string]any{
				"upstream": map[string]any{"commit": "", "date": ""},
				"pending":  []any{},
			})
		})
	}
	go router.Run()
	DeferCleanup(router.MustStop)
	return openPage(b, path)
}

var _ = Describe("Settings indexers", Label("e2e"), func() {
	It("shows a built-in indexer as YAML on a read-only instance", func() {
		cardiganntest.Install()
		page := newReadOnlyPage("/settings/indexers", false)
		page.MustElementR("button", "^Add built-in$").MustClick()
		page.MustElementR("div[role=dialog] li button", "^3D Torrents").MustClick()

		// Page lookups wait for the definition to load; an element's own do not.
		page.MustElement(`#cardigann-form input[name="settings.username"]`).
			MustInput("u")
		page.MustElement(`#cardigann-form input[name="settings.password"]`).
			MustInput("a:b#c")
		page.MustElementR("button", "^Show YAML$").MustClick()

		yaml := page.MustElementR("div[role=dialog] pre", "protocol: cardigann").
			MustText()
		Expect(yaml).To(ContainSubstring(`definition: "3dtorrents"`))
		Expect(
			yaml,
		).To(ContainSubstring(`commit: "` + cardiganntest.Upstream.Commit + `"`))
		Expect(yaml).To(ContainSubstring(`username: "u"`))
		Expect(yaml).To(ContainSubstring(`password: "a:b#c"`))
		Expect(yaml).To(ContainSubstring("priority: 25"))
		Expect(yaml).NotTo(ContainSubstring("freeleech"))
		// It is pasted under the file's own indexers: key.
		Expect(yaml).NotTo(ContainSubstring("indexers:"))
	})

	It("will not show YAML with no catalog commit to pin", func() {
		cardiganntest.Install()
		page := newReadOnlyPage("/settings/indexers", true)
		page.MustElementR("button", "^Add built-in$").MustClick()
		page.MustElementR("div[role=dialog] li button", "^3D Torrents").MustClick()
		page.MustElement(`#cardigann-form input[name="settings.username"]`).
			MustInput("u")
		Consistently(func() bool {
			return page.MustElementR("button", "^Show YAML$").
				MustProperty("disabled").
				Bool()
		}).WithTimeout(time.Second).Should(BeTrue())
	})

	It("escapes the line breaks YAML would fold", func() {
		cardiganntest.Install()
		page := newReadOnlyPage("/settings/indexers", false)
		page.MustElementR("button", "^Add built-in$").MustClick()
		page.MustElementR("div[role=dialog] li button", "^3D Torrents").MustClick()
		page.MustElement(`#cardigann-form input[name="settings.username"]`).
			MustInput("a\u2028b\u0085c")
		page.MustElementR("button", "^Show YAML$").MustClick()
		yaml := page.MustElementR("div[role=dialog] pre", "protocol: cardigann").
			MustText()
		Expect(yaml).To(ContainSubstring(`username: "a\u2028b\u0085c"`))
	})

	It("saves an edit of an indexer with a setting read from a file", func() {
		cardiganntest.Install()
		path := filepath.Join(GinkgoT().TempDir(), "freeleech")
		Expect(os.WriteFile(path, []byte("true"), 0o600)).To(Succeed())
		const name = "FileHeld"
		Expect(config.Update(context.Background(), func(c *config.Config) error {
			c.Indexers = append(c.Indexers, config.IndexerEntry{
				Name: name, Protocol: "cardigann", Enabled: true,
				Cardigann: &config.CardigannIndexer{
					Definition: "3dtorrents",
					Commit:     cardiganntest.Upstream.Commit,
					Settings: map[string]string{
						"username":       "u",
						"freeleech_file": path,
					},
				},
			})
			return nil
		})).To(Succeed())
		DeferCleanup(func() {
			Expect(config.DeleteIndexer(context.Background(), name)).To(Succeed())
		})

		page := newSessionPage("/settings/indexers")
		page.MustElementX(`//*[text()="` + name + `"]/ancestor::*` +
			`[.//button[@aria-label="Edit indexer"]][1]` +
			`//button[@aria-label="Edit indexer"]`).MustClick()
		form := page.MustElement("#cardigann-form")
		Expect(page.MustElementR("#cardigann-form *", path).MustText()).
			To(ContainSubstring(path))
		Expect(form.MustHas(`input[name="settings.freeleech"]`)).To(BeFalse())
		page.MustElement(`#cardigann-form input[name="settings.username"]`).
			MustSelectAllText().MustInput("v")
		page.MustElementR("button", "^Save changes$").MustClick()
		expectToast(page, "Indexer updated")
		got, _ := config.FindIndexer(name)
		Expect(got.Cardigann.Settings).To(And(
			HaveKeyWithValue("username", "v"),
			HaveKeyWithValue("freeleech_file", path),
			Not(HaveKey("freeleech")),
		))
	})
})
