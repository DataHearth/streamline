package download

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	enttvshow "github.com/datahearth/streamline/ent/tvshow"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/library"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe("Adoption", Label("unit", "downloads"), func() {
	hasFileFn := func(v bool) func(*ent.Movie) bool {
		return func(*ent.Movie) bool { return v }
	}

	Describe("classifyMovieAdoption", func() {
		candidates := []*ent.Movie{
			{ID: 3, Title: "The Batman", Year: 2022, TmdbID: 414906},
		}

		BeforeEach(func() {
			configtest.Setup(map[string]any{
				"quality_profiles": []any{
					map[string]any{
						"name": "HD", "preferred_resolution": "1080p",
						"min_resolution": "1080p",
					},
				},
				"quality_default_profile": "HD",
			})
		})

		It("auto-imports a fileless movie at OK resolution", func() {
			parsed := library.Parse("The.Batman.2022.1080p.BluRay-X")
			dec, ok := classifyMovieAdoption(parsed, candidates, hasFileFn(false))
			Expect(ok).To(BeTrue())
			Expect(dec.autoImport).To(BeTrue())
			Expect(dec.movieID).To(Equal(uint32(3)))
			Expect(dec.quality).To(Equal("1080p"))
			Expect(dec.reason).To(BeEmpty())
		})

		It("proposes when the movie already has a file", func() {
			parsed := library.Parse("The.Batman.2022.1080p.BluRay-X")
			dec, ok := classifyMovieAdoption(parsed, candidates, hasFileFn(true))
			Expect(ok).To(BeTrue())
			Expect(dec.autoImport).To(BeFalse())
			Expect(dec.reason).To(Equal("already have a file"))
		})

		It("proposes when resolution is below the profile minimum", func() {
			parsed := library.Parse("The.Batman.2022.720p.WEB-X")
			dec, ok := classifyMovieAdoption(parsed, candidates, hasFileFn(false))
			Expect(ok).To(BeTrue())
			Expect(dec.autoImport).To(BeFalse())
			Expect(dec.reason).To(ContainSubstring("below minimum"))
		})

		It("proposes on an ambiguous (multi) title match", func() {
			multi := []*ent.Movie{
				{ID: 3, Title: "The Batman", Year: 2022, TmdbID: 1},
				{ID: 4, Title: "The Batman", Year: 2022, TmdbID: 2},
			}
			parsed := library.Parse("The.Batman.2022.1080p.BluRay-X")
			dec, ok := classifyMovieAdoption(parsed, multi, hasFileFn(false))
			Expect(ok).To(BeTrue())
			Expect(dec.autoImport).To(BeFalse())
			Expect(dec.reason).To(Equal("ambiguous match"))
		})

		It("skips when no movie matches", func() {
			parsed := library.Parse("Some.Other.Film.2019.1080p-X")
			_, ok := classifyMovieAdoption(parsed, candidates, hasFileFn(false))
			Expect(ok).To(BeFalse())
		})
	})

	Describe("resolutionOK", func() {
		It("ranks the resolution ladder (4K == 2160p)", func() {
			Expect(resolutionOK("720p", "1080p")).To(BeFalse())
			Expect(resolutionOK("1080p", "1080p")).To(BeTrue())
			Expect(resolutionOK("2160p", "1080p")).To(BeTrue())
			Expect(resolutionOK("4K", "2160p")).To(BeTrue())
			Expect(resolutionOK("", "1080p")).To(BeFalse())
			Expect(resolutionOK("1080p", "")).To(BeTrue())
		})
	})

	Describe("classifyEpisodeAdoption", func() {
		// buildShow wires one season with two episodes; ep2 optionally carries a
		// media file. anime toggles the show type.
		buildShow := func(anime, ep2HasFile bool) *ent.TVShow {
			ep1 := &ent.Episode{ID: 101, Number: 1, AbsoluteNumber: 1}
			ep2 := &ent.Episode{ID: 102, Number: 2, AbsoluteNumber: 2}
			if ep2HasFile {
				ep2.Edges.MediaFiles = []*ent.MediaFile{{ID: 9}}
			}
			season := &ent.Season{
				Number: 1,
				Edges:  ent.SeasonEdges{Episodes: []*ent.Episode{ep1, ep2}},
			}
			typ := enttvshow.TypeStandard
			if anime {
				typ = enttvshow.TypeAnime
			}
			return &ent.TVShow{
				ID: 1, Title: "The Bear", Type: typ,
				Edges: ent.TVShowEdges{Seasons: []*ent.Season{season}},
			}
		}

		BeforeEach(func() {
			configtest.Setup(map[string]any{
				"quality_profiles": []any{
					map[string]any{
						"name": "HD", "preferred_resolution": "1080p",
						"min_resolution": "1080p",
					},
				},
				"quality_default_profile": "HD",
			})
		})

		It("auto-imports a fileless episode at OK resolution", func() {
			shows := []*ent.TVShow{buildShow(false, false)}
			parsed := library.Parse("The.Bear.S01E02.1080p.WEB-X")
			dec, ok := classifyEpisodeAdoption(parsed, shows, episodeHasFile)
			Expect(ok).To(BeTrue())
			Expect(dec.autoImport).To(BeTrue())
			Expect(dec.episodeID).To(Equal(uint32(102)))
		})

		It("proposes when the matched episode already has a file", func() {
			shows := []*ent.TVShow{buildShow(false, true)}
			parsed := library.Parse("The.Bear.S01E02.1080p.WEB-X")
			dec, ok := classifyEpisodeAdoption(parsed, shows, episodeHasFile)
			Expect(ok).To(BeTrue())
			Expect(dec.autoImport).To(BeFalse())
			Expect(dec.reason).To(Equal("already have a file"))
		})

		It("proposes a season pack linked to the season's first episode", func() {
			shows := []*ent.TVShow{buildShow(false, false)}
			parsed := library.ParseResult{
				Title: "The Bear", Season: 1, SeasonPack: true, Resolution: "1080p",
			}
			dec, ok := classifyEpisodeAdoption(parsed, shows, episodeHasFile)
			Expect(ok).To(BeTrue())
			Expect(dec.autoImport).To(BeFalse())
			Expect(dec.reason).To(Equal("season pack, review manually"))
			Expect(dec.episodeID).To(Equal(uint32(101)))
		})

		It("matches an anime release on its absolute number", func() {
			shows := []*ent.TVShow{buildShow(true, false)}
			parsed := library.ParseResult{
				Title: "The Bear", AbsoluteNumber: 2, Resolution: "1080p",
			}
			dec, ok := classifyEpisodeAdoption(parsed, shows, episodeHasFile)
			Expect(ok).To(BeTrue())
			Expect(dec.episodeID).To(Equal(uint32(102)))
			Expect(dec.autoImport).To(BeTrue())
		})

		It("skips when no show title matches", func() {
			shows := []*ent.TVShow{buildShow(false, false)}
			parsed := library.Parse("Other.Show.S01E01.1080p-X")
			_, ok := classifyEpisodeAdoption(parsed, shows, episodeHasFile)
			Expect(ok).To(BeFalse())
		})
	})

	Describe("AdoptManualTorrents", func() {
		var (
			ctx   context.Context
			store *dbmocks.MockStore
			mgr   Adopter
		)
		BeforeEach(func() {
			ctx = context.Background()
			store = dbmocks.NewMockStore(GinkgoT())
			mgr = New(store).(Adopter)
		})

		It("returns the error when listing known hashes fails", func() {
			store.EXPECT().AllDownloadRecordHashes(mock.Anything).
				Return(nil, errors.New("db boom")).Once()
			_, err := mgr.AdoptManualTorrents(ctx)
			Expect(err).To(MatchError(ContainSubstring("db boom")))
		})

		It("no-ops when no download clients are configured", func() {
			configtest.Setup()
			store.EXPECT().AllDownloadRecordHashes(mock.Anything).
				Return(map[string]struct{}{}, nil).Once()
			ids, err := mgr.AdoptManualTorrents(ctx)
			Expect(err).NotTo(HaveOccurred())
			Expect(ids).To(BeEmpty())
		})
	})
})
