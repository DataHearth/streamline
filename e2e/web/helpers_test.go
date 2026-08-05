package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-rod/rod"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/e2e/apptest"
	"github.com/datahearth/streamline/e2e/fakes"
	"github.com/datahearth/streamline/internal/auth"
)

var (
	// sessionJWT is the seed admin's session token, minted once in BeforeSuite.
	// auth.Limiter allows 5 POST /auth/login attempts per 15 minutes per IP and
	// every spec shares 127.0.0.1: the three form submissions in auth_test.go
	// plus this mint spend 4 of them.
	sessionJWT string

	// A dedicated client rather than http.DefaultClient: a target that drops
	// instead of refusing would otherwise hang until the suite timeout.
	httpClient = &http.Client{Timeout: 10 * time.Second}
)

// mintSessionJWT logs the seed admin in over plain HTTP and returns the session
// cookie's value, which is the session JWT and doubles as an /api/v1 Bearer.
func mintSessionJWT() string {
	GinkgoHelper()
	body, err := json.Marshal(map[string]string{
		"email":    apptest.AdminEmail,
		"password": apptest.AdminPassword,
	})
	Expect(err).NotTo(HaveOccurred())
	resp, err := httpClient.Post(
		baseURL+"/auth/login",
		"application/json",
		bytes.NewReader(body),
	)
	Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()
	Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
	for _, c := range resp.Cookies() {
		if c.Name == auth.SessionCookie {
			return c.Value
		}
	}
	Fail("login response did not set the session cookie")
	return ""
}

// apiDo issues one /api/v1 request as the seed admin and returns its status.
// Specs use it only to seed or restore state around the interaction under
// test — never to stand in for it. A non-nil out is JSON-decoded.
func apiDo(method, path string, body, out any) int {
	GinkgoHelper()
	var payload io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		Expect(err).NotTo(HaveOccurred())
		payload = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, baseURL+path, payload)
	Expect(err).NotTo(HaveOccurred())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+sessionJWT)
	resp, err := httpClient.Do(req)
	Expect(err).NotTo(HaveOccurred())
	defer resp.Body.Close()
	if out != nil {
		Expect(json.NewDecoder(resp.Body).Decode(out)).To(Succeed())
	}
	return resp.StatusCode
}

// deleteQualityProfile tolerates 404 so it doubles as cleanup for a spec that
// already deleted the profile through the UI.
func deleteQualityProfile(name string) {
	GinkgoHelper()
	Expect(apiDo(
		http.MethodDelete, "/api/v1/quality-profiles/"+name, nil, nil,
	)).To(BeElementOf(http.StatusNoContent, http.StatusNotFound))
}

// seedQualityProfile provisions a profile for specs whose subject is the edit
// or delete interaction rather than the create form.
func seedQualityProfile(name string) {
	GinkgoHelper()
	Expect(apiDo(http.MethodPost, "/api/v1/quality-profiles", map[string]any{
		"name":                 name,
		"preferred_resolution": "1080p",
		"min_resolution":       "720p",
		"upgrade_allowed":      true,
	}, nil)).To(Equal(http.StatusCreated))
	DeferCleanup(func() { deleteQualityProfile(name) })
}

// seedMovie adds the TMDB fake's canned movie and returns its library id, so a
// spec can drive the UI against a populated library without going through the
// add flow it isn't testing. Removal is registered before the caller's first
// assertion: the empty-library specs share this app.
func seedMovie() uint32 {
	GinkgoHelper()
	var movie struct {
		ID uint32 `json:"id"`
	}
	Expect(apiDo(http.MethodPost, "/api/v1/movies", map[string]any{
		"tmdb_id": fakes.MovieTMDBID,
	}, &movie)).To(Equal(http.StatusCreated))
	DeferCleanup(func() {
		Expect(apiDo(
			http.MethodDelete,
			fmt.Sprintf("/api/v1/movies/%d", movie.ID),
			nil,
			nil,
		)).To(Equal(http.StatusNoContent))
	})
	return movie.ID
}

// revokeInvitesFor deletes every invite bound to email — keyed by email
// because the UI never reveals the id of the invite it just created.
func revokeInvitesFor(email string) {
	GinkgoHelper()
	var invites []struct {
		ID    uint32 `json:"id"`
		Email string `json:"email"`
	}
	Expect(apiDo(http.MethodGet, "/api/v1/auth/invites", nil, &invites)).
		To(Equal(http.StatusOK))
	for _, inv := range invites {
		if inv.Email != email {
			continue
		}
		Expect(apiDo(
			http.MethodDelete,
			fmt.Sprintf("/api/v1/auth/invites/%d", inv.ID),
			nil,
			nil,
		)).To(BeElementOf(http.StatusNoContent, http.StatusNotFound))
	}
}

// displayName reads the seed admin's current display name so a spec can put it
// back after editing it through the UI.
func displayName() string {
	GinkgoHelper()
	var me struct {
		DisplayName string `json:"display_name"`
	}
	Expect(apiDo(http.MethodGet, "/api/v1/auth/me", nil, &me)).
		To(Equal(http.StatusOK))
	return me.DisplayName
}

func setDisplayName(name string) {
	GinkgoHelper()
	Expect(apiDo(http.MethodPatch, "/api/v1/auth/me", map[string]any{
		"display_name": name,
	}, nil)).To(Equal(http.StatusOK))
}

// expectToast waits for a svelte-sonner toast whose title contains title.
// Polled rather than looked up with MustElementR: when the action raised a
// *different* toast — typically the server's error message — the selector would
// never match and the spec would burn the page's whole budget before failing
// with nothing to show. Polling lets the failure carry what was on screen.
func expectToast(page *rod.Page, title string) {
	GinkgoHelper()
	var shown []string
	Eventually(func() bool {
		shown = nil
		for _, el := range page.MustElements(`[data-sonner-toast] [data-title]`) {
			text := el.MustText()
			shown = append(shown, text)
			if strings.Contains(text, title) {
				return true
			}
		}
		return false
	}).
		WithTimeout(5*time.Second).
		WithPolling(50*time.Millisecond).
		Should(BeTrue(), func() string {
			if len(shown) == 0 {
				return fmt.Sprintf("no toast appeared; expected %q", title)
			}
			return fmt.Sprintf("expected a toast titled %q, got %q", title, shown)
		})
}

// visibleElements returns only the matches that are actually rendered. The
// responsive rework ships a phone and a desktop variant of several landmarks —
// two asides labelled "Primary navigation", two navs labelled "Movie status" —
// both present in the DOM and gated purely by CSS. Specs run at 1440px, so the
// phone twin is the hidden one, and it sorts first.
func visibleElements(page *rod.Page, selector string) rod.Elements {
	GinkgoHelper()
	var out rod.Elements
	for _, el := range page.MustElements(selector) {
		if el.MustVisible() {
			out = append(out, el)
		}
	}
	return out
}

// visibleElement is visibleElements for the single-match case. It polls: the
// variant that wins is decided by CSS, which is not settled on the first paint.
// Reaching for MustElement instead hands back whichever copy sorts first, and
// clicking a hidden one just burns the page budget into a deadline error.
func visibleElement(page *rod.Page, selector string) *rod.Element {
	GinkgoHelper()
	var found *rod.Element
	Eventually(func() bool {
		matches := visibleElements(page, selector)
		if len(matches) == 0 {
			return false
		}
		found = matches[0]
		return true
	}).
		WithTimeout(5*time.Second).
		WithPolling(50*time.Millisecond).
		Should(BeTrue(), "no visible element matched %q", selector)
	return found
}

// expectPath waits for the client-side router to commit path. Routify paints
// the new route (and its heading) a tick before it rewrites history, and the
// list pages rewrite their own query string from an effect after that — so the
// URL is the last thing to settle, never the first.
func expectPath(page *rod.Page, path string) {
	GinkgoHelper()
	Eventually(func() string { return page.MustInfo().URL }).
		WithTimeout(5 * time.Second).
		WithPolling(50 * time.Millisecond).
		Should(HaveSuffix(path))
}
