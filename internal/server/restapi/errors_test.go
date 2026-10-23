package restapi

import (
	"errors"
	"io"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/internal/download"
)

var _ = Describe("Internal error sanitization",
	Label("unit", "server", "restapi"), func() {
		var app *apiKeyApp
		BeforeEach(func() { app = newAPIKeyApp() })

		const leak = "sqlite: no such column /var/lib/streamline/db.sqlite"

		It("generifies the error a handler returns to the strict adapter", func() {
			app.downloads.EXPECT().Queue(mock.Anything).
				Return(download.QueueSnapshot{}, errors.New(leak)).Once()

			resp := app.do(
				app.req(http.MethodGet, "/api/v1/activity/queue", app.adminKey, nil),
			)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusInternalServerError))
			body, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(body)).NotTo(ContainSubstring("sqlite"))
			Expect(string(body)).To(ContainSubstring(internalErrorMessage))
		})
	})
