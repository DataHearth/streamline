package restapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/config"
	"github.com/datahearth/streamline/internal/db"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var _ = Describe(
	"Handler: Extensions",
	Label("integration", "server", "extensions"),
	func() {
		var (
			app  *apiKeyApp
			echo string
		)

		send := func(method, path, body string) (*http.Response, []byte) {
			GinkgoHelper()
			var rd *strings.Reader
			if body != "" {
				rd = strings.NewReader(body)
			}
			var req *http.Request
			if rd != nil {
				req = app.req(method, "/api/v1"+path, "", rd)
				req.Header.Set("Content-Type", "application/json")
			} else {
				req = app.req(method, "/api/v1"+path, "", nil)
			}
			resp := app.do(req)
			defer resp.Body.Close()
			raw, err := io.ReadAll(resp.Body)
			Expect(err).NotTo(HaveOccurred())
			return resp, raw
		}

		decode := func(raw []byte) Extension {
			GinkgoHelper()
			var e Extension
			Expect(json.Unmarshal(raw, &e)).To(Succeed(), string(raw))
			return e
		}

		createEcho := func(extra string) {
			GinkgoHelper()
			resp, raw := send(http.MethodPost, "/extensions", fmt.Sprintf(
				`{"name":"echo","source":%q,"enabled":true%s}`, echo, extra,
			))
			Expect(resp.StatusCode).To(Equal(http.StatusCreated), string(raw))
		}

		BeforeEach(func() {
			configtest.SetupFile()
			app = newAPIKeyApp()

			client, err := db.Open(context.Background(), app.corePath)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() { Expect(client.Close()).To(Succeed()) })
			DeferCleanup(func() {
				Expect(app.extensions.Close(context.Background())).To(Succeed())
			})

			echo, err = filepath.Abs("../../extension/testdata/echo/echo.wasm")
			Expect(err).NotTo(HaveOccurred())
		})

		It("lists nothing as an empty items array", func() {
			resp, raw := send(http.MethodGet, "/extensions", "")
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			Expect(strings.TrimSpace(string(raw))).
				To(Equal(`{"items":[]}`))
		})

		It("inspects a module without saving it", func() {
			resp, raw := send(http.MethodPost, "/extensions/inspect",
				fmt.Sprintf(`{"source":%q}`, echo))
			Expect(resp.StatusCode).To(Equal(http.StatusOK), string(raw))
			var in ExtensionInspect
			Expect(json.Unmarshal(raw, &in)).To(Succeed())
			Expect(in.Name).To(Equal("echo"))
			Expect(in.Requested.Views).To(ContainElement(
				HaveField("Name", "books"),
			))
			Expect(in.Requested.Views[0].Description).NotTo(BeEmpty())
			Expect(in.Requested.Mounts).To(ConsistOf(
				ExtensionMount{Path: "/echo", Auth: new("self")},
			))
			Expect(in.Card).To(HaveValue(HaveField("Title", "Echo")))
			Expect(config.Get().Extensions).To(BeEmpty())
		})

		It("answers 422 extension_unreadable for a source it cannot read", func() {
			resp, raw := send(http.MethodPost, "/extensions/inspect",
				`{"source":"/nonexistent/x.wasm"}`)
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			var e Error
			Expect(json.Unmarshal(raw, &e)).To(Succeed())
			Expect(e.Code).To(HaveValue(Equal("extension_unreadable")))
			Expect(e.Message).NotTo(BeEmpty())
		})

		It("answers 422 extension_unreadable for a checksum mismatch", func() {
			resp, raw := send(
				http.MethodPost,
				"/extensions/inspect",
				fmt.Sprintf(
					`{"source":%q,"sha256":%q}`,
					echo,
					strings.Repeat("0", 64),
				),
			)
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
			var e Error
			Expect(json.Unmarshal(raw, &e)).To(Succeed())
			Expect(e.Code).To(HaveValue(Equal("extension_unreadable")))
		})

		It("adds an extension that then asks for approval", func() {
			createEcho("")
			resp, raw := send(http.MethodGet, "/extensions", "")
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
			var list ExtensionList
			Expect(json.Unmarshal(raw, &list)).To(Succeed())
			items := list.Items
			Expect(items).To(HaveLen(1))
			Expect(items[0].State).To(Equal(ExtensionState("needs_approval")))
			Expect(items[0].Version).NotTo(BeNil())
			Expect(items[0].Requested.Views).
				To(ContainElement(HaveField("Name", "books")))
			Expect(items[0].Granted.Views).To(BeEmpty())
		})

		It("approves exactly what the manifest asks for", func() {
			createEcho("")
			resp, raw := send(http.MethodPost, "/extensions/echo/approve", "")
			Expect(resp.StatusCode).To(Equal(http.StatusOK), string(raw))
			e := decode(raw)
			Expect(e.State).To(Equal(ExtensionState("running")))
			Expect(e.Granted).To(Equal(*e.Requested))
			Expect(
				config.Get().Extensions[0].Grants.Mounts,
			).To(ContainElement("/echo"))
		})

		It("approves the folders the manifest asks for", func() {
			full, err := filepath.Abs("../../extension/testdata/echo/echo-full.wasm")
			Expect(err).NotTo(HaveOccurred())
			resp, raw := send(http.MethodPost, "/extensions", fmt.Sprintf(
				`{"name":"echo","source":%q,"enabled":true}`, full,
			))
			Expect(resp.StatusCode).To(Equal(http.StatusCreated), string(raw))
			Expect(decode(raw).State).To(Equal(ExtensionState("needs_approval")))

			resp, raw = send(http.MethodPost, "/extensions/echo/approve", "")
			Expect(resp.StatusCode).To(Equal(http.StatusOK), string(raw))
			e := decode(raw)
			Expect(e.State).To(Equal(ExtensionState("running")))
			Expect(e.Granted.Folders).To(ConsistOf("music", "artwork", "cache"))
			Expect(config.Get().Extensions[0].Grants.Folders).
				To(ConsistOf("music", "artwork", "cache"))
		})

		It("answers 404 approving an unknown extension", func() {
			resp, _ := send(http.MethodPost, "/extensions/nope/approve", "")
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It(
			"answers 422 extension_unreadable approving an unreadable module",
			func() {
				createEcho("")
				resp, _ := send(http.MethodPatch, "/extensions/echo",
					`{"source":"/nonexistent/x.wasm"}`)
				Expect(resp.StatusCode).To(Equal(http.StatusOK))

				resp, raw := send(http.MethodPost, "/extensions/echo/approve", "")
				Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
				var e Error
				Expect(json.Unmarshal(raw, &e)).To(Succeed())
				Expect(e.Code).To(HaveValue(Equal("extension_unreadable")))
			},
		)

		It("answers 409 when the name is taken", func() {
			createEcho("")
			resp, _ := send(http.MethodPost, "/extensions", fmt.Sprintf(
				`{"name":"echo","source":%q}`, echo,
			))
			Expect(resp.StatusCode).To(Equal(http.StatusConflict))
		})

		It("saves an unreadable source as a failed extension", func() {
			resp, raw := send(http.MethodPost, "/extensions",
				`{"name":"ghost","source":"/nonexistent/x.wasm","enabled":true}`)
			Expect(resp.StatusCode).To(Equal(http.StatusCreated), string(raw))
			e := decode(raw)
			Expect(e.State).To(Equal(ExtensionState("failed")))
			Expect(e.Reason).NotTo(BeNil())
		})

		It("answers 422 for an invalid entry", func() {
			resp, _ := send(http.MethodPost, "/extensions",
				`{"name":"Bad Name","source":"/x.wasm"}`)
			Expect(resp.StatusCode).To(Equal(http.StatusUnprocessableEntity))
		})

		It("patches enabled and reconciles", func() {
			createEcho("")
			_, _ = send(http.MethodPost, "/extensions/echo/approve", "")

			resp, raw := send(
				http.MethodPatch,
				"/extensions/echo",
				`{"enabled":false}`,
			)
			Expect(resp.StatusCode).To(Equal(http.StatusOK), string(raw))
			Expect(decode(raw).State).To(Equal(ExtensionState("disabled")))
			_, _, ok := app.extensions.Acquire("/echo")
			Expect(ok).To(BeFalse())
		})

		It("answers 404 patching an unknown extension", func() {
			resp, _ := send(http.MethodPatch, "/extensions/nope", `{"enabled":true}`)
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("deletes an entry and keeps its data unless purged", func() {
			createEcho("")
			_, _ = send(http.MethodPost, "/extensions/echo/approve", "")
			dataDir := filepath.Dir(app.extensions.DataPaths()["echo"])
			Expect(dataDir).To(BeADirectory())

			resp, _ := send(http.MethodDelete, "/extensions/echo", "")
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
			Expect(config.Get().Extensions).To(BeEmpty())
			Expect(dataDir).To(BeADirectory())
		})

		It("purges the data directory with purge=true", func() {
			createEcho("")
			_, _ = send(http.MethodPost, "/extensions/echo/approve", "")
			dataDir := filepath.Dir(app.extensions.DataPaths()["echo"])

			resp, _ := send(http.MethodDelete, "/extensions/echo?purge=true", "")
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
			_, err := os.Stat(dataDir)
			Expect(os.IsNotExist(err)).To(BeTrue())
		})

		It("answers 404 deleting an unknown extension", func() {
			resp, _ := send(http.MethodDelete, "/extensions/nope", "")
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("answers 403 on every write when the config is read-only", func() {
			configtest.SetupFile(map[string]any{"read_only": true})
			resp, _ := send(http.MethodPost, "/extensions", fmt.Sprintf(
				`{"name":"echo","source":%q}`, echo,
			))
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})

		It("lets an account manage its secret for an extension's card", func() {
			createEcho("")
			resp, raw := send(http.MethodPost, "/extensions/echo/approve", "")
			Expect(resp.StatusCode).To(Equal(http.StatusOK), string(raw))
			Expect(decode(raw).Card).To(HaveValue(HaveField("Mount", "/echo")))

			const credential = "/account/extensions/echo/credential"
			resp, raw = send(http.MethodGet, "/account/extensions", "")
			Expect(resp.StatusCode).To(Equal(http.StatusOK), string(raw))
			var cards []ExtensionAccountCard
			Expect(json.Unmarshal(raw, &cards)).To(Succeed())
			Expect(cards).To(HaveLen(1))
			Expect(cards[0].Mount).To(Equal("/echo"))
			Expect(cards[0].Enabled).To(BeFalse())

			resp, raw = send(http.MethodPost, credential, "")
			Expect(resp.StatusCode).To(Equal(http.StatusOK), string(raw))
			var created AppAccessCreated
			Expect(json.Unmarshal(raw, &created)).To(Succeed())
			Expect(created.Secret).To(Equal("echo-secret"))
			Expect(created.Enabled).To(BeTrue())
			Expect(created.Username).To(Equal("admin@test.com"))

			resp, raw = send(http.MethodGet, credential, "")
			Expect(resp.StatusCode).To(Equal(http.StatusOK), string(raw))
			Expect(string(raw)).NotTo(ContainSubstring(created.Secret))

			resp, _ = send(http.MethodDelete, credential, "")
			Expect(resp.StatusCode).To(Equal(http.StatusNoContent))
			resp, raw = send(http.MethodGet, credential, "")
			Expect(resp.StatusCode).To(Equal(http.StatusOK), string(raw))
			var access AppAccess
			Expect(json.Unmarshal(raw, &access)).To(Succeed())
			Expect(access.Enabled).To(BeFalse())

			resp, _ = send(http.MethodGet, "/account/extensions/nope/credential", "")
			Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
		})

		It("refuses a member", func() {
			app.memberKey = "member-key"
			req := app.req(http.MethodGet, "/api/v1/extensions", "member-key", nil)
			resp := app.do(req)
			defer resp.Body.Close()
			Expect(resp.StatusCode).To(Equal(http.StatusForbidden))
		})
	},
)
