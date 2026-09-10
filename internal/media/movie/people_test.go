package movie

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/metadata"
	mockmeta "github.com/datahearth/streamline/internal/metadata/mocks"
)

var _ = Describe("Movie cast enrichment", Label("unit", "movies"), func() {
	var (
		ctx       context.Context
		storeMock *dbmocks.MockStore_Expecter
		metaMock  *mockmeta.MockProvider_Expecter
		svc       *Service
		saved     map[uint32]metadata.PersonDetails
		savedMu   sync.Mutex
	)

	BeforeEach(func() {
		ctx = context.Background()
		store := dbmocks.NewMockStore(GinkgoT())
		storeMock = store.EXPECT()
		meta := mockmeta.NewMockProvider(GinkgoT())
		metaMock = meta.EXPECT()
		svc = NewService(store, meta, nil, nil)
		saved = map[uint32]metadata.PersonDetails{}
	})

	pending := func(people ...db.Person) {
		GinkgoHelper()
		storeMock.PeopleNeedingDetails(
			mock.Anything, db.CastOwnerMovie, uint32(9),
		).Return(people, nil).Once()
	}

	// recordSaves accepts any number of saves and records them, so a spec can
	// assert on what was written rather than on a call count alone.
	recordSaves := func() {
		GinkgoHelper()
		storeMock.SavePersonDetails(mock.Anything, mock.Anything, mock.Anything).
			RunAndReturn(func(
				_ context.Context, id uint32, d metadata.PersonDetails,
			) error {
				savedMu.Lock()
				defer savedMu.Unlock()
				saved[id] = d
				return nil
			}).Maybe()
	}

	It("fetches nobody when the work list is empty", func() {
		// An already-stamped person never reaches the service: the query
		// filters on details_fetched_at, so "already enriched" shows up here
		// as an empty work list and no provider call at all.
		pending()
		recordSaves()

		svc.enrichPeople(ctx, 9)

		Expect(saved).To(BeEmpty())
	})

	It("fetches a person by their tmdb id and saves what came back", func() {
		details := metadata.PersonDetails{
			Biography:    "An actor.",
			KnownFor:     "Acting",
			Birthday:     "1951-09-25",
			PlaceOfBirth: "Oakland, California, USA",
			IMDbID:       "nm0000434",
		}
		pending(db.Person{ID: 3, TMDBID: 1892, Name: "Mark Hamill"})
		metaMock.GetPerson(mock.Anything, uint32(1892)).Return(&details, nil).Once()
		recordSaves()

		svc.enrichPeople(ctx, 9)

		Expect(saved).To(HaveLen(1))
		Expect(saved[3]).To(Equal(details))
	})

	It("skips a person the TMDB client cannot look up", func() {
		// The movie service holds only the TMDB client. A tvdb-only person is
		// left to whichever series credits them, and a person with neither id
		// has nothing to look up at all — no GetPerson expectation, so either
		// one reaching the provider fails the spec.
		pending(
			db.Person{ID: 4, TVDBID: 77, Name: "Uncredited Extra"},
			db.Person{ID: 5, Name: "Nobody At All"},
		)
		recordSaves()

		svc.enrichPeople(ctx, 9)

		Expect(saved).To(BeEmpty())
	})

	It("leaves the person unstamped when the provider fails", func() {
		// Nothing is saved, so details_fetched_at stays nil and the next
		// metadata refresh retries — the import itself is unaffected.
		pending(db.Person{ID: 3, TMDBID: 1892, Name: "Mark Hamill"})
		metaMock.GetPerson(mock.Anything, uint32(1892)).
			Return(nil, errors.New("tmdb unreachable")).Once()
		recordSaves()

		svc.enrichPeople(ctx, 9)

		Expect(saved).To(BeEmpty())
	})

	It("swallows a failing work-list query", func() {
		storeMock.PeopleNeedingDetails(
			mock.Anything, db.CastOwnerMovie, uint32(9),
		).Return(nil, errors.New("db gone")).Once()

		svc.enrichPeople(ctx, 9)
	})

	It("keeps going past a person whose lookup failed", func() {
		pending(
			db.Person{ID: 3, TMDBID: 1892, Name: "Mark Hamill"},
			db.Person{ID: 6, TMDBID: 2, Name: "Carrie Fisher"},
		)
		metaMock.GetPerson(mock.Anything, uint32(1892)).
			Return(nil, errors.New("tmdb unreachable")).Once()
		metaMock.GetPerson(mock.Anything, uint32(2)).
			Return(&metadata.PersonDetails{Biography: "Also an actor."}, nil).Once()
		recordSaves()

		svc.enrichPeople(ctx, 9)

		Expect(saved).To(HaveKey(uint32(6)))
		Expect(saved).NotTo(HaveKey(uint32(3)))
	})

	It("holds no more than personDetailConcurrency lookups in flight", func() {
		people := make([]db.Person, 0, 12)
		for i := range uint32(12) {
			people = append(people, db.Person{ID: i + 1, TMDBID: i + 1})
		}
		pending(people...)

		var inFlight, peak atomic.Int64
		metaMock.GetPerson(mock.Anything, mock.Anything).
			RunAndReturn(func(
				_ context.Context, _ uint32,
			) (*metadata.PersonDetails, error) {
				now := inFlight.Add(1)
				for {
					seen := peak.Load()
					if now <= seen || peak.CompareAndSwap(seen, now) {
						break
					}
				}
				defer inFlight.Add(-1)
				return &metadata.PersonDetails{Biography: "b"}, nil
			}).Times(12)
		recordSaves()

		svc.enrichPeople(ctx, 9)

		Expect(saved).To(HaveLen(12))
		Expect(peak.Load()).To(BeNumerically("<=", personDetailConcurrency))
	})
})
