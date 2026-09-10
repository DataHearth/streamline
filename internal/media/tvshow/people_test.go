package tvshow

import (
	"context"
	"errors"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/internal/db"
	dbmocks "github.com/datahearth/streamline/internal/db/mocks"
	"github.com/datahearth/streamline/internal/metadata"
	mockmeta "github.com/datahearth/streamline/internal/metadata/mocks"
)

var _ = Describe("Series cast enrichment", Label("unit", "series"), func() {
	var (
		ctx       context.Context
		storeMock *dbmocks.MockStore_Expecter
		metaMock  *mockmeta.MockTVProvider_Expecter
		svc       *Service
		saved     map[uint32]metadata.PersonDetails
		savedMu   sync.Mutex
	)

	BeforeEach(func() {
		ctx = context.Background()
		store := dbmocks.NewMockStore(GinkgoT())
		storeMock = store.EXPECT()
		meta := mockmeta.NewMockTVProvider(GinkgoT())
		metaMock = meta.EXPECT()
		svc = NewService(store, meta, nil, nil)
		saved = map[uint32]metadata.PersonDetails{}
	})

	pending := func(people ...db.Person) {
		GinkgoHelper()
		storeMock.PeopleNeedingDetails(
			mock.Anything, db.CastOwnerSeries, uint32(4),
		).Return(people, nil).Once()
	}

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

	It("fetches nobody when every credited person is already stamped", func() {
		pending()
		recordSaves()

		svc.enrichPeople(ctx, 4)

		Expect(saved).To(BeEmpty())
	})

	It("fetches a person by their tvdb id and keeps the empty fields empty", func() {
		// TVDB carries no known-for department and usually no socials. Empty
		// is the correct answer there, not a hole to fill from elsewhere.
		details := metadata.PersonDetails{
			Biography: "A performer.",
			Birthday:  "1970-01-02",
		}
		pending(db.Person{ID: 8, TVDBID: 456, Name: "Ann Actress"})
		metaMock.GetPerson(mock.Anything, uint32(456)).Return(&details, nil).Once()
		recordSaves()

		svc.enrichPeople(ctx, 4)

		Expect(saved).To(HaveLen(1))
		Expect(saved[8]).To(Equal(details))
		Expect(saved[8].KnownFor).To(BeEmpty())
		Expect(saved[8].TwitterID).To(BeEmpty())
	})

	It("skips a person the TVDB client cannot look up", func() {
		pending(
			db.Person{ID: 9, TMDBID: 1892, Name: "Mark Hamill"},
			db.Person{ID: 10, Name: "Nobody At All"},
		)
		recordSaves()

		svc.enrichPeople(ctx, 4)

		Expect(saved).To(BeEmpty())
	})

	It("leaves the person unstamped when the provider fails", func() {
		pending(db.Person{ID: 8, TVDBID: 456, Name: "Ann Actress"})
		metaMock.GetPerson(mock.Anything, uint32(456)).
			Return(nil, errors.New("tvdb unreachable")).Once()
		recordSaves()

		svc.enrichPeople(ctx, 4)

		Expect(saved).To(BeEmpty())
	})

	It("swallows a failing work-list query", func() {
		storeMock.PeopleNeedingDetails(
			mock.Anything, db.CastOwnerSeries, uint32(4),
		).Return(nil, errors.New("db gone")).Once()

		svc.enrichPeople(ctx, 4)
	})
})
