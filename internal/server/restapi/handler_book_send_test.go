package restapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/internal/media/book"
)

var _ = Describe("Handler: book send", Label("unit", "server", "books"), func() {
	var app *apiKeyApp

	BeforeEach(func() {
		app = newAPIKeyApp()
	})

	post := func(path string) *http.Response {
		GinkgoHelper()
		return app.do(app.req(http.MethodPost, path, app.requestOnlyKey, nil))
	}
	errorOf := func(resp *http.Response) Error {
		GinkgoHelper()
		defer resp.Body.Close()
		var e Error
		Expect(json.NewDecoder(resp.Body).Decode(&e)).To(Succeed())
		return e
	}

	DescribeTable("POST /books/{id}/send",
		func(sendErr error, status int, code string) {
			app.books.EXPECT().
				SendBook(mock.Anything, app.requestOnlyID, uint32(7)).
				Return(sendErr).
				Once()
			resp := post("/api/v1/books/7/send")
			Expect(resp.StatusCode).To(Equal(status))
			if code != "" {
				Expect(*errorOf(resp).Code).To(Equal(code))
			}
		},
		Entry("sends", nil, http.StatusNoContent, ""),
		Entry("no sendable file",
			fmt.Errorf("%w: AZW3 isn't accepted by email", book.ErrNotSendable),
			http.StatusUnprocessableEntity, "not_sendable"),
		Entry("no device address", book.ErrNoDeviceEmail,
			http.StatusUnprocessableEntity, "no_device_email"),
		Entry("smtp off", book.ErrSMTPNotConfigured,
			http.StatusServiceUnavailable, "smtp_not_configured"),
		Entry("unknown book", book.ErrBookNotFound, http.StatusNotFound, ""),
		Entry("mail server refuses", errors.New("550 mailbox unavailable"),
			http.StatusBadGateway, "send_failed"),
	)

	It("answers not_sendable with the reason alone", func() {
		app.books.EXPECT().
			SendBook(mock.Anything, mock.Anything, mock.Anything).
			Return(fmt.Errorf("%w: file is missing on disk", book.ErrNotSendable)).
			Once()
		Expect(errorOf(post("/api/v1/books/7/send")).Message).
			To(Equal("file is missing on disk"))
	})

	It("lets a request-only user send", func() {
		app.books.EXPECT().
			SendBook(mock.Anything, mock.Anything, mock.Anything).
			Return(nil).
			Once()
		Expect(post("/api/v1/books/7/send").StatusCode).
			To(Equal(http.StatusNoContent))
	})

	Describe("POST /books/series/{id}/send", func() {
		It("answers 202 with the plan", func() {
			app.books.EXPECT().
				SendSeries(mock.Anything, app.requestOnlyID, uint32(9)).
				Return(book.SeriesSendPlan{
					Emails: 2, Volumes: 3,
					Skipped: []book.SkippedVolume{
						{BookID: 4, Title: "Vol 4", Reason: "no ebook file"},
					},
				}, nil).
				Once()
			resp := post("/api/v1/books/series/9/send")
			Expect(resp.StatusCode).To(Equal(http.StatusAccepted))
			defer resp.Body.Close()
			var plan SeriesSendPlan
			Expect(json.NewDecoder(resp.Body).Decode(&plan)).To(Succeed())
			Expect(plan.Emails).To(Equal(2))
			Expect(plan.SentVolumes).To(Equal(3))
			Expect(plan.Skipped).To(Equal([]SeriesSendSkipped{
				{BookId: 4, Title: "Vol 4", Reason: "no ebook file"},
			}))
		})

		DescribeTable("maps errors",
			func(sendErr error, status int, code string) {
				app.books.EXPECT().
					SendSeries(mock.Anything, mock.Anything, mock.Anything).
					Return(book.SeriesSendPlan{}, sendErr).
					Once()
				resp := post("/api/v1/books/series/9/send")
				Expect(resp.StatusCode).To(Equal(status))
				if code != "" {
					Expect(*errorOf(resp).Code).To(Equal(code))
				}
			},
			Entry("nothing sendable",
				fmt.Errorf("%w: no volume is sendable", book.ErrNotSendable),
				http.StatusUnprocessableEntity, "not_sendable"),
			Entry("no device address", book.ErrNoDeviceEmail,
				http.StatusUnprocessableEntity, "no_device_email"),
			Entry("smtp off", book.ErrSMTPNotConfigured,
				http.StatusServiceUnavailable, "smtp_not_configured"),
			Entry("unknown series", book.ErrSeriesNotFound,
				http.StatusNotFound, ""),
		)
	})

	_ = context.Background
})
