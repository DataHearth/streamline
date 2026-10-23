package api

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type qualityProfileView struct {
	Name                string `json:"name"`
	PreferredResolution string `json:"preferred_resolution"`
	MinResolution       string `json:"min_resolution"`
	UpgradeAllowed      bool   `json:"upgrade_allowed"`
}

// createQualityProfile adds a 1080p/720p profile and schedules its removal.
// Cleanup is registered before the first assertion so a later failure cannot
// leak the entity; 404 covers specs that delete it themselves.
func createQualityProfile(name string) qualityProfileView {
	GinkgoHelper()
	resp := post("/api/v1/quality-profiles", adminAuth, map[string]any{
		"name":                 name,
		"preferred_resolution": "1080p",
		"min_resolution":       "720p",
		"upgrade_allowed":      true,
	})
	defer resp.Body.Close()
	DeferCleanup(func() {
		cleanup := del("/api/v1/quality-profiles/"+name, adminAuth, nil)
		defer cleanup.Body.Close()
		Expect(cleanup.StatusCode).To(BeElementOf(
			http.StatusNoContent, http.StatusNotFound,
		))
	})
	Expect(resp.StatusCode).To(Equal(http.StatusCreated))
	var profile qualityProfileView
	decode(resp, &profile)
	return profile
}

var _ = Describe("REST API quality profiles", Label("e2e"), func() {
	It("lists the seeded default profile", func() {
		resp := get("/api/v1/quality-profiles", adminAuth)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		var profiles []struct {
			Name string `json:"name"`
		}
		decode(resp, &profiles)
		Expect(profiles).To(ContainElement(HaveField("Name", "default")))
	})

	It("creates, updates and deletes a profile", func() {
		profile := createQualityProfile("e2e-qp")
		Expect(profile.Name).To(Equal("e2e-qp"))
		Expect(profile.PreferredResolution).To(Equal("1080p"))
		Expect(profile.MinResolution).To(Equal("720p"))
		Expect(profile.UpgradeAllowed).To(BeTrue())

		updated := put("/api/v1/quality-profiles/e2e-qp", adminAuth, map[string]any{
			"name":                 "e2e-qp",
			"preferred_resolution": "2160p",
			"min_resolution":       "1080p",
			"upgrade_allowed":      false,
		})
		defer updated.Body.Close()
		Expect(updated.StatusCode).To(Equal(http.StatusOK))
		decode(updated, &profile)
		Expect(profile.PreferredResolution).To(Equal("2160p"))
		Expect(profile.MinResolution).To(Equal("1080p"))
		Expect(profile.UpgradeAllowed).To(BeFalse())

		deleted := del("/api/v1/quality-profiles/e2e-qp", adminAuth, nil)
		defer deleted.Body.Close()
		Expect(deleted.StatusCode).To(Equal(http.StatusNoContent))
	})

	It("409s a duplicate profile name", func() {
		createQualityProfile("e2e-qp-dup")
		resp := post("/api/v1/quality-profiles", adminAuth, map[string]any{
			"name":                 "e2e-qp-dup",
			"preferred_resolution": "1080p",
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusConflict))
	})

	It("422s an out-of-range resolution", func() {
		resp := post("/api/v1/quality-profiles", adminAuth, map[string]any{
			"name":                 "e2e-qp-bad",
			"preferred_resolution": "480p",
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
	})

	It("404s updating and deleting an unknown profile", func() {
		updated := put(
			"/api/v1/quality-profiles/e2e-missing",
			adminAuth,
			map[string]any{
				"name":                 "e2e-missing",
				"preferred_resolution": "1080p",
			},
		)
		defer updated.Body.Close()
		Expect(updated.StatusCode).To(Equal(http.StatusNotFound))

		deleted := del("/api/v1/quality-profiles/e2e-missing", adminAuth, nil)
		defer deleted.Body.Close()
		Expect(deleted.StatusCode).To(Equal(http.StatusNotFound))
	})

	It("409s deleting the configured default profile", func() {
		resp := del("/api/v1/quality-profiles/default", adminAuth, nil)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusConflict))
	})

	It("403s mutations for a non-admin", func() {
		created := post("/api/v1/quality-profiles", viewerAuth, map[string]any{
			"name":                 "e2e-qp-nope",
			"preferred_resolution": "1080p",
		})
		defer created.Body.Close()
		Expect(created.StatusCode).To(Equal(http.StatusForbidden))

		updated := put(
			"/api/v1/quality-profiles/default",
			viewerAuth,
			map[string]any{
				"name":                 "default",
				"preferred_resolution": "1080p",
			},
		)
		defer updated.Body.Close()
		Expect(updated.StatusCode).To(Equal(http.StatusForbidden))

		deleted := del("/api/v1/quality-profiles/default", viewerAuth, nil)
		defer deleted.Body.Close()
		Expect(deleted.StatusCode).To(Equal(http.StatusForbidden))
	})
})
