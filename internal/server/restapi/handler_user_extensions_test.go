package restapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/ent"
	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe(
	"Handler: User extension cards",
	Label("integration", "server", "extensions"),
	func() {
		var app *apiKeyApp

		send := func(key, method, path, body string) (*http.Response, []byte) {
			GinkgoHelper()
			var rd io.Reader
			if body != "" {
				rd = strings.NewReader(body)
			}
			req := app.req(method, "/api/v1"+path, key, rd)
			if body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			resp := app.do(req)
			defer resp.Body.Close()
			raw, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())
			return resp, raw
		}

		userExists := func(ids ...uint32) {
			for _, id := range ids {
				app.auth.EXPECT().GetUserByID(mock.Anything, id).
					Return(&ent.User{ID: id}, nil).Once()
			}
		}

		const prefs = "/users/%d/extensions/echo/cards/prefs"

		BeforeEach(func() {
			configtest.SetupFile()
			app = newAPIKeyApp()
			app.addMember("m")

			client, err := db.Open(context.Background(), app.corePath)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })
			DeferCleanup(func() {
				Expect(app.extensions.Close(context.Background())).To(Succeed())
			})

			echo, err := filepath.Abs("../../extension/testdata/echo/echo.wasm")
			Expect(err).NotTo(HaveOccurred())
			resp, raw := send(app.adminKey, http.MethodPost, "/extensions",
				fmt.Sprintf(`{"name":"echo","source":%q,"enabled":true}`, echo))
			Expect(resp.StatusCode).To(Equal(http.StatusCreated), string(raw))
			resp, raw = send(
				app.adminKey,
				http.MethodPost,
				"/extensions/echo/approve",
				"",
			)
			Expect(resp.StatusCode).To(Equal(http.StatusOK), string(raw))
		})

		It("lists the settings card with the user's stored values", func() {
			userExists(7, 7, 8)
			resp, raw := send(app.adminKey, http.MethodPut, fmt.Sprintf(prefs, 7),
				`{"values":{"allow":true,"limit":128}}`)
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent), string(raw))

			resp, raw = send(app.adminKey, http.MethodGet, "/users/7/extensions", "")
			Expect(resp.StatusCode).To(Equal(http.StatusOK), string(raw))
			var cards []UserExtensionCard
			Expect(json.Unmarshal(raw, &cards)).To(Succeed())
			Expect(cards).To(HaveLen(1))
			Expect(cards[0].Extension).To(Equal("echo"))
			Expect(cards[0].Card).To(Equal("prefs"))
			Expect(cards[0].Title).To(Equal("Echo settings"))
			Expect(cards[0].Fields).To(HaveLen(2))
			Expect(
				cards[0].Values,
			).To(Equal(map[string]any{"allow": true, "limit": 128.0}))

			_, raw = send(app.adminKey, http.MethodGet, "/users/8/extensions", "")
			Expect(json.Unmarshal(raw, &cards)).To(Succeed())
			Expect(
				cards[0].Values,
			).To(Equal(map[string]any{"allow": false, "limit": 0.0}))
		})

		DescribeTable("refuses a write the card does not declare",
			func(body string) {
				userExists(7)
				resp, raw := send(
					app.adminKey,
					http.MethodPut,
					fmt.Sprintf(prefs, 7),
					body,
				)
				Expect(
					resp.StatusCode,
				).To(Equal(http.StatusUnprocessableEntity), string(raw))
				Expect(string(raw)).To(ContainSubstring("card_values_invalid"))
			},
			Entry("a number above its bound", `{"values":{"limit":321}}`),
			Entry("a number below its bound", `{"values":{"limit":-1}}`),
			Entry("a key the card does not declare", `{"values":{"other":true}}`),
			Entry("the wrong type", `{"values":{"allow":"yes"}}`),
		)

		It("never calls the extension for a refused write", func() {
			userExists(7, 7)
			send(app.adminKey, http.MethodPut, fmt.Sprintf(prefs, 7),
				`{"values":{"limit":999}}`)

			_, raw := send(app.adminKey, http.MethodGet, "/users/7/extensions", "")
			var cards []UserExtensionCard
			Expect(json.Unmarshal(raw, &cards)).To(Succeed())
			Expect(cards[0].Values["limit"]).To(Equal(0.0))
		})

		It("answers 404 for an unknown user", func() {
			app.auth.EXPECT().GetUserByID(mock.Anything, uint32(99)).
				Return(nil, auth.ErrUserNotFound).Twice()

			resp, _ := send(app.adminKey, http.MethodGet, "/users/99/extensions", "")
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			resp, _ = send(app.adminKey, http.MethodPut, fmt.Sprintf(prefs, 99),
				`{"values":{"allow":true}}`)
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("refuses a member", func() {
			resp, _ := send(app.memberKey, http.MethodGet, "/users/7/extensions", "")
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
			resp, _ = send(app.memberKey, http.MethodPut, fmt.Sprintf(prefs, 7),
				`{"values":{"allow":true}}`)
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})

		Context("once the extension is stopped", func() {
			BeforeEach(func() {
				resp, raw := send(app.adminKey, http.MethodPatch, "/extensions/echo",
					`{"enabled":false}`)
				Expect(resp.StatusCode).To(Equal(http.StatusOK), string(raw))
			})

			It("leaves the card out of the list", func() {
				userExists(7)
				_, raw := send(
					app.adminKey,
					http.MethodGet,
					"/users/7/extensions",
					"",
				)
				Expect(strings.TrimSpace(string(raw))).To(Equal("[]"))
			})

			It("answers 404 to a write", func() {
				userExists(7)
				resp, _ := send(app.adminKey, http.MethodPut, fmt.Sprintf(prefs, 7),
					`{"values":{"allow":true}}`)
				Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
			})
		})
	},
)
