package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/e2e/fakes"
	"github.com/datahearth/streamline/internal/download"
)

const kaamelottTVDBID = 79175

// The homelab release that produced this spec: a six-season French integrale
// grabbed by hand to fill three gaps. It names no season, so library.Parse
// reports season 0 — the *specials* — and the proposal used to anchor on
// S00E01, an episode the torrent does not even contain.
const integraleName = "Kaamelott Integrale (Livres I a VI + Bonus) FRENCH " +
	"[1080p] BDRIP HEVC-H265 10bits - Themouche"

// episodeSize clears library.MinEpisodeSize (5 MiB) so the preview's file
// filter counts it; smallFileSize deliberately does not.
const (
	episodeSize = 200 << 20
	smallFile   = 1 << 20
)

var _ = Describe("Manual torrent adoption", Label("e2e", "api"), func() {
	// seedKaamelott writes three seasons of two episodes each, with S01E01
	// already on disk. The gaps are S01E100-shaped: the second episode of each
	// season, numbered 100 so the anchor cannot be confused with "the first
	// episode of the first season".
	seedKaamelott := func(ctx context.Context) {
		GinkgoHelper()
		show := app.DB.TVShow.Create().
			SetTitle("Kaamelott").SetYear(2005).SetTvdbID(kaamelottTVDBID).
			SaveX(ctx)
		// A populated specials season, which is what made season 0 reachable
		// as an anchor in the first place.
		specials := app.DB.Season.Create().
			SetNumber(0).SetTvShow(show).SaveX(ctx)
		app.DB.Episode.Create().
			SetNumber(1).SetTitle("Bonus").SetSeason(specials).SaveX(ctx)

		for n := uint16(1); n <= 3; n++ {
			season := app.DB.Season.Create().
				SetNumber(n).SetTvShow(show).SaveX(ctx)
			first := app.DB.Episode.Create().
				SetNumber(1).SetTitle(fmt.Sprintf("Livre %d, 1", n)).
				SetSeason(season).SaveX(ctx)
			app.DB.MediaFile.Create().
				SetPath(fmt.Sprintf("/library/Kaamelott/S%02dE01.mkv", n)).
				SetSize(episodeSize).SetEpisode(first).SaveX(ctx)
			app.DB.Episode.Create().
				SetNumber(100).SetTitle(fmt.Sprintf("Livre %d, 100", n)).
				SetSeason(season).SaveX(ctx)
		}
		DeferCleanup(func() {
			app.DB.TVShow.DeleteOneID(show.ID).ExecX(context.Background())
		})
	}

	// registerClient points streamline at the fake for the duration of one
	// spec. Config-backed resources are global, so it is removed again rather
	// than left for whichever spec runs next.
	registerClient := func(fakeURL string) {
		GinkgoHelper()
		u, err := url.Parse(fakeURL)
		Expect(err).NotTo(HaveOccurred())
		port, err := strconv.ParseUint(u.Port(), 10, 16)
		Expect(err).NotTo(HaveOccurred())

		resp := post("/api/v1/download-clients", adminAuth, map[string]any{
			"name": "e2e-qbit", "client_type": "qbittorrent",
			"host": u.Hostname(), "port": port,
			"auth_method": "password",
			"username":    fakes.QBUsername, "password": fakes.QBPassword,
			"enabled": true,
		})
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusCreated), bodyText(resp))
		DeferCleanup(func() {
			del("/api/v1/download-clients/e2e-qbit", adminAuth, nil).
				Body.Close()
		})
	}

	type previewEpisode struct {
		Season  uint16 `json:"season"`
		Episode uint16 `json:"episode"`
		Title   string `json:"title"`
	}

	type proposal struct {
		ID          uint32 `json:"id"`
		Title       string `json:"title"`
		HasFile     bool   `json:"has_file"`
		ParsedTitle string `json:"parsed_title"`
		Media       *struct {
			Type    string `json:"type"`
			Title   string `json:"title"`
			Season  uint16 `json:"season"`
			Episode uint16 `json:"episode"`
		} `json:"media"`
	}

	findProposal := func(title string) proposal {
		GinkgoHelper()
		var pending struct {
			Items []proposal `json:"items"`
		}
		resp := get("/api/v1/activity/pending", adminAuth)
		defer resp.Body.Close()
		decode(resp, &pending)
		for _, item := range pending.Items {
			if item.Title == title {
				return item
			}
		}
		Fail("no proposal filed for " + title)
		return proposal{}
	}

	It("anchors a whole-series pack on a gap and previews what it fills", func() {
		ctx := context.Background()
		seedKaamelott(ctx)

		// One file per episode the library knows, in per-Livre subdirectories —
		// the layout that defeated the importer's non-recursive pack walk —
		// plus a bonus file matching nothing and an undersized sample.
		var files []fakes.QBFile
		for n := 1; n <= 3; n++ {
			for _, ep := range []int{1, 100} {
				files = append(files, fakes.QBFile{
					Path: fmt.Sprintf(
						"Kaamelott Integrale/Livre %d/Kaamelott.S%02dE%02d.1080p.BDRIP.mkv",
						n,
						n,
						ep,
					),
					Size: episodeSize,
				})
			}
		}
		files = append(files,
			fakes.QBFile{
				Path: "Kaamelott Integrale/Bonus/Making.Of.1080p.mkv",
				Size: episodeSize,
			},
			fakes.QBFile{
				Path: "Kaamelott Integrale/Livre 1/sample.mkv",
				Size: smallFile,
			},
		)
		qb := fakes.NewQBittorrent(fakes.QBTorrent{
			Hash: "e2eadopt0000000000000000000000000000abcd",
			Name: integraleName, Files: files,
		})
		registerClient(qb.URL)

		// The download poll job's own call, made directly so the spec does not
		// wait on a scheduler tick. App.Downloads is the same value.
		adopter, ok := app.Downloads.(download.Adopter)
		Expect(ok).To(BeTrue())
		enqueued, err := adopter.AdoptManualTorrents(ctx)
		Expect(err).NotTo(HaveOccurred())
		// A pack is never auto-imported, however clean it looks.
		Expect(enqueued).To(BeEmpty())

		// The release name carries the integrale tag and the language between
		// the title and the resolution, so nothing in the library matches it
		// and the proposal is filed unidentified — the homelab's own first
		// step, and the reason the operator names it by hand.
		filed := findProposal(integraleName)
		Expect(filed.Media).To(BeNil())
		Expect(filed.ParsedTitle).To(HavePrefix("Kaamelott"))

		identified := post(
			fmt.Sprintf("/api/v1/activity/pending/%d/identify", filed.ID),
			adminAuth,
			map[string]any{"kind": "series", "provider_id": kaamelottTVDBID},
		)
		defer identified.Body.Close()
		Expect(identified.StatusCode).
			To(Equal(http.StatusNoContent), bodyText(identified))

		named := findProposal(integraleName)
		Expect(named.Media).NotTo(BeNil())
		Expect(named.Media.Type).To(Equal("episode"))
		Expect(named.Media.Title).To(Equal("Kaamelott"))
		// The gap it can fill — not S00E01 (specials, and not even in the
		// torrent) and not S01E01 (already on disk).
		Expect(named.Media.Season).To(Equal(uint16(1)))
		Expect(named.Media.Episode).To(Equal(uint16(100)))
		// Anchored on a gap, so the row offers Import, not Replace.
		Expect(named.HasFile).To(BeFalse())

		var preview struct {
			Imports   []previewEpisode `json:"imports"`
			OnDisk    []previewEpisode `json:"on_disk"`
			Unmatched int              `json:"unmatched"`
		}
		previewed := get(
			fmt.Sprintf("/api/v1/activity/pending/%d/preview", named.ID),
			adminAuth,
		)
		defer previewed.Body.Close()
		// bodyText drains, so the status is checked without it here — the
		// decode below is what needs the body.
		Expect(previewed.StatusCode).To(Equal(http.StatusOK))
		decode(previewed, &preview)

		filled := make([]string, 0, len(preview.Imports))
		for _, e := range preview.Imports {
			filled = append(filled, fmt.Sprintf("S%02dE%02d", e.Season, e.Episode))
		}
		Expect(filled).To(ConsistOf("S01E100", "S02E100", "S03E100"))
		Expect(preview.OnDisk).To(HaveLen(3))
		// The making-of only; the undersized sample never reaches matching.
		Expect(preview.Unmatched).To(Equal(1))
	})

	It("previews nothing for a proposal with no episode", func() {
		qb := fakes.NewQBittorrent()
		registerClient(qb.URL)

		resp := get("/api/v1/activity/pending/999999/preview", adminAuth)
		defer resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		Expect(strings.ToLower(bodyText(resp))).To(ContainSubstring("not found"))
	})
})
