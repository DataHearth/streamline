package book

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/ent/mediafile"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/mailer"
	mailmocks "github.com/datahearth/streamline/internal/mailer/mocks"
)

const mb = 1 << 20

func writeFile(dir, name string, size int) string {
	GinkgoHelper()
	p := filepath.Join(dir, name)
	Expect(os.WriteFile(p, make([]byte, size), 0o600)).To(Succeed())
	return p
}

func ebookRow(path, quality string) *ent.MediaFile {
	return &ent.MediaFile{
		Path: path, Quality: quality, BookKind: mediafile.BookKindEbook,
	}
}

var _ = Describe("Sendable", Label("unit", "book"), func() {
	var dir string
	BeforeEach(func() { dir = GinkgoT().TempDir() })

	It("prefers the EPUB over a CBZ", func() {
		epub := ebookRow(writeFile(dir, "a.epub", 10), "EPUB")
		cbz := ebookRow(writeFile(dir, "a.cbz", 10), "CBZ")
		got, reason := Sendable([]*ent.MediaFile{cbz, epub}, mb)
		Expect(reason).To(BeEmpty())
		Expect(got).To(BeIdenticalTo(epub))
	})

	It("falls back to the PDF", func() {
		pdf := ebookRow(writeFile(dir, "a.pdf", 10), "PDF")
		got, reason := Sendable([]*ent.MediaFile{pdf}, mb)
		Expect(reason).To(BeEmpty())
		Expect(got).To(BeIdenticalTo(pdf))
	})

	It("names the format email does not accept", func() {
		azw := ebookRow(writeFile(dir, "a.azw3", 10), "AZW3")
		got, reason := Sendable([]*ent.MediaFile{azw}, mb)
		Expect(got).To(BeNil())
		Expect(reason).To(Equal("AZW3 isn't accepted by email; use OPDS"))
	})

	It("ignores an audiobook", func() {
		m4b := ebookRow(writeFile(dir, "a.m4b", 10), "M4B")
		m4b.BookKind = mediafile.BookKindAudiobook
		got, reason := Sendable([]*ent.MediaFile{m4b}, mb)
		Expect(got).To(BeNil())
		Expect(reason).To(Equal("no ebook file"))
	})

	It("refuses a file over the limit", func() {
		big := ebookRow(writeFile(dir, "big.epub", mb+mb/2), "EPUB")
		got, reason := Sendable([]*ent.MediaFile{big}, mb)
		Expect(got).To(BeNil())
		Expect(reason).To(Equal("file is 1.5 MB, over the 1 MB limit"))
	})

	It("reports a file deleted behind the library", func() {
		gone := ebookRow(filepath.Join(dir, "gone.epub"), "EPUB")
		got, reason := Sendable([]*ent.MediaFile{gone}, mb)
		Expect(got).To(BeNil())
		Expect(reason).To(Equal("file is missing on disk"))
	})
})

var _ = Describe("packEmails", Label("unit", "book"), func() {
	item := func(id uint32, size int64) sendItem {
		return sendItem{bookID: id, size: size}
	}

	It("fills first-fit and keeps the order", func() {
		got := packEmails(
			[]sendItem{item(1, 10), item(2, 10), item(3, 10)}, 25, 25,
		)
		Expect(got).To(HaveLen(2))
		Expect(got[0]).To(HaveLen(2))
		Expect(got[0][0].bookID).To(Equal(uint32(1)))
		Expect(got[0][1].bookID).To(Equal(uint32(2)))
		Expect(got[1][0].bookID).To(Equal(uint32(3)))
	})

	It("caps the attachments of one email", func() {
		items := make([]sendItem, 30)
		for i := range items {
			items[i] = item(uint32(i+1), 1)
		}
		got := packEmails(items, 1000, 25)
		Expect(got).To(HaveLen(2))
		Expect(got[0]).To(HaveLen(25))
		Expect(got[1]).To(HaveLen(5))
	})
})

var _ = Describe("sending", Label("unit", "book"), func() {
	var (
		f      *fixture
		sender *mailmocks.MockSender
		userID uint32
	)

	setup := func(deviceEmail string) {
		GinkgoHelper()
		f = newFixture()
		sender = mailmocks.NewMockSender(GinkgoT())
		f.svc.mail = sender
		create := f.client.User.Create().SetEmail("reader@example.com")
		if deviceEmail != "" {
			create.SetDeviceEmail(deviceEmail)
		}
		userID = create.SaveX(f.ctx).ID
	}

	addFile := func(b *ent.Book, path, quality string) {
		GinkgoHelper()
		f.client.MediaFile.Create().
			SetPath(path).SetSize(1).SetQuality(quality).
			SetFormat("x").SetBookID(b.ID).
			SetBookKind(mediafile.BookKindEbook).SaveX(f.ctx)
	}

	Describe("SendBook", func() {
		It("sends the EPUB to the device address", func() {
			setup("me@kindle.com")
			b := f.addBook(1, "Les Misérables", "")
			addFile(b, writeFile(f.dir, "a.epub", 10), "EPUB")

			sender.EXPECT().
				Send(mock.Anything, "me@kindle.com", "Les Misérables", mock.Anything).
				RunAndReturn(func(
					_ context.Context, _, _ string, atts []mailer.Attachment,
				) error {
					Expect(atts).To(HaveLen(1))
					Expect(atts[0].Name).To(Equal("Les Misérables.epub"))
					body, err := io.ReadAll(atts[0].Body)
					Expect(err).NotTo(HaveOccurred())
					Expect(body).To(HaveLen(10))
					return nil
				}).Once()

			Expect(f.svc.SendBook(f.ctx, userID, b.ID)).To(Succeed())
		})

		It("needs a device email", func() {
			setup("")
			b := f.addBook(1, "Elantris", "")
			Expect(f.svc.SendBook(f.ctx, userID, b.ID)).
				To(MatchError(ErrNoDeviceEmail))
		})

		It("needs a configured mailer", func() {
			setup("me@kindle.com")
			f.svc.mail = nil
			b := f.addBook(1, "Elantris", "")
			Expect(f.svc.SendBook(f.ctx, userID, b.ID)).
				To(MatchError(ErrSMTPNotConfigured))
		})

		It("refuses a format email does not accept", func() {
			setup("me@kindle.com")
			b := f.addBook(1, "Elantris", "")
			addFile(b, writeFile(f.dir, "a.azw3", 10), "AZW3")
			err := f.svc.SendBook(f.ctx, userID, b.ID)
			Expect(err).To(MatchError(ErrNotSendable))
			Expect(err.Error()).To(ContainSubstring("AZW3"))
		})

		It("answers not sendable when the file is gone", func() {
			setup("me@kindle.com")
			b := f.addBook(1, "Elantris", "")
			addFile(b, filepath.Join(f.dir, "gone.epub"), "EPUB")
			err := f.svc.SendBook(f.ctx, userID, b.ID)
			Expect(err).To(MatchError(ErrNotSendable))
			Expect(err.Error()).To(ContainSubstring("file is missing on disk"))
		})

		It("reports the state the button reads", func() {
			setup("me@kindle.com")
			b := f.addBook(1, "Elantris", "")
			Expect(f.svc.SendState(f.ctx, userID, f.reloadBook(b.ID))).
				To(Equal(SendState{Reason: "no ebook file"}))
			addFile(b, writeFile(f.dir, "a.epub", 10), "EPUB")
			Expect(f.svc.SendState(f.ctx, userID, f.reloadBook(b.ID))).
				To(Equal(SendState{OK: true}))
		})
	})

	Describe("SendSeries", func() {
		It("packs one email and lists refusals, not unowned volumes", func() {
			setup("me@kindle.com")
			now := time.Now()
			seed := func(hc uint32, pos float64, refreshed *time.Time) db.BookSeed {
				return db.BookSeed{
					HardcoverID:    hc,
					Title:          "Vol " + itoa(uint32(pos)),
					Kind:           "manga",
					SeriesPosition: &pos,
					RefreshedAt:    refreshed,
				}
			}
			s, _, err := f.store.CreateSeries(f.ctx, db.CreateSeriesParams{
				HardcoverID: 50, Title: "One Piece", Kind: "manga",
				Monitor: "none", RefreshedAt: now,
				Volumes: []db.BookSeed{
					seed(1001, 1, &now), seed(1002, 2, &now), seed(1003, 3, &now),
					seed(1004, 4, &now), seed(1005, 5, nil), seed(1006, 6, &now),
				},
			})
			Expect(err).NotTo(HaveOccurred())

			vols := f.reloadSeries(s.ID).Edges.Volumes
			Expect(vols).To(HaveLen(6))
			addFile(vols[0], writeFile(f.dir, "v1.epub", 10), "EPUB")
			addFile(vols[1], writeFile(f.dir, "v2.epub", 10), "EPUB")
			addFile(vols[2], writeFile(f.dir, "v3.cbz", 10), "CBZ")
			addFile(vols[3], filepath.Join(f.dir, "gone.epub"), "EPUB")
			addFile(vols[4], writeFile(f.dir, "v5.epub", 10), "EPUB")

			sender.EXPECT().
				Send(mock.Anything, "me@kindle.com", "One Piece", mock.Anything).
				RunAndReturn(func(
					_ context.Context, _, _ string, atts []mailer.Attachment,
				) error {
					Expect(atts).To(HaveLen(2))
					Expect(atts[0].Name).To(Equal("Vol 1.epub"))
					Expect(atts[1].Name).To(Equal("Vol 2.epub"))
					return nil
				}).Once()

			plan, err := f.svc.SendSeries(f.ctx, userID, s.ID)
			Expect(err).NotTo(HaveOccurred())
			Expect(plan.Volumes).To(Equal(2))
			Expect(plan.Emails).To(Equal(1))
			Expect(plan.Skipped).To(ConsistOf(
				SkippedVolume{
					BookID: vols[2].ID, Title: "Vol 3",
					Reason: "CBZ isn't accepted by email; use OPDS",
				},
				SkippedVolume{
					BookID: vols[3].ID, Title: "Vol 4",
					Reason: "file is missing on disk",
				},
			))
			Eventually(func() int {
				return len(sender.Calls)
			}).Should(Equal(1))
		})

		It("refuses a series with nothing to send", func() {
			setup("me@kindle.com")
			now := time.Now()
			pos := 1.0
			s, _, err := f.store.CreateSeries(f.ctx, db.CreateSeriesParams{
				HardcoverID: 50, Title: "One Piece", Kind: "manga",
				Monitor: "none", RefreshedAt: now,
				Volumes: []db.BookSeed{{
					HardcoverID: 1001, Title: "Vol 1", Kind: "manga",
					SeriesPosition: &pos, RefreshedAt: &now,
				}},
			})
			Expect(err).NotTo(HaveOccurred())
			_, err = f.svc.SendSeries(f.ctx, userID, s.ID)
			Expect(err).To(MatchError(ErrNotSendable))
		})
	})
})
