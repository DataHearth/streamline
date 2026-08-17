package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/go-chi/chi/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/mock"

	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/mediaserver"
	msmocks "github.com/datahearth/streamline/internal/mediaserver/mocks"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

var errPlexUnreachable = errors.New("plex unreachable")

// expirePlexFlow backdates a begun flow past its TTL. The store is
// package-private, so a spec can age a record directly instead of the
// production type carrying an injectable clock for the test's benefit.
func expirePlexFlow(p *plexPinFlows, id string) {
	GinkgoHelper()
	p.mu.Lock()
	defer p.mu.Unlock()
	f, ok := p.flows[id]
	Expect(ok).To(BeTrue(), "flow %q was never begun", id)
	f.expiresAt = time.Now().Add(-time.Minute)
	p.flows[id] = f
}

var _ = Describe("Plex PIN endpoints", Label("unit", "server"), func() {
	const upstreamPinID = uint64(12345)

	var (
		handler *Handler
		servers *msmocks.MockManager
		router  chi.Router
	)

	claimsFor := func(userID uint32, role, jti string) *auth.Claims {
		return &auth.Claims{UserID: userID, Role: role, JTI: jti}
	}

	adminA := func() *auth.Claims { return claimsFor(1, "admin", "jti-a") }
	adminB := func() *auth.Claims { return claimsFor(2, "admin", "jti-b") }

	do := func(
		method, target string,
		claims *auth.Claims,
	) *httptest.ResponseRecorder {
		GinkgoHelper()
		req := httptest.NewRequest(method, target, nil)
		req.Host = "streamline.example"
		if claims != nil {
			req = req.WithContext(auth.ContextWithClaims(req.Context(), claims))
		}
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		return rr
	}

	begin := func(claims *auth.Claims) *httptest.ResponseRecorder {
		GinkgoHelper()
		return do(http.MethodPost, "/settings/media-servers/plex/pin", claims)
	}

	poll := func(flowID string, claims *auth.Claims) *httptest.ResponseRecorder {
		GinkgoHelper()
		return do(
			http.MethodGet,
			"/settings/media-servers/plex/pin/"+flowID,
			claims,
		)
	}

	beginOK := func(claims *auth.Claims) string {
		GinkgoHelper()
		servers.EXPECT().
			BeginPlexPin(mock.Anything).
			Return(mediaserver.PlexPin{
				ID:       upstreamPinID,
				AuthURL:  "https://app.plex.tv/auth#?code=abcd",
				ClientID: "cid",
			}, nil).
			Once()
		rr := begin(claims)
		Expect(rr.Code).To(Equal(http.StatusOK))
		var body plexPinBeginResponse
		Expect(json.Unmarshal(rr.Body.Bytes(), &body)).To(Succeed())
		Expect(body.FlowID).NotTo(BeEmpty())
		return body.FlowID
	}

	BeforeEach(func() {
		configtest.Setup()
		servers = msmocks.NewMockManager(GinkgoT())
		handler = New(Deps{MediaServers: servers})
		router = chi.NewRouter()
		handler.registerWebMediaServerRoutes(router)
	})

	DescribeTable(
		"rejects begin for anything but an admin session",
		func(claims *auth.Claims) {
			Expect(begin(claims).Code).To(Equal(http.StatusForbidden))
		},
		Entry("member", claimsFor(3, "member", "jti-m")),
		Entry("request_only", claimsFor(4, "request_only", "jti-r")),
		Entry("no claims", nil),
	)

	DescribeTable(
		"rejects poll for anything but an admin session",
		func(claims *auth.Claims) {
			Expect(poll("anything", claims).Code).To(Equal(http.StatusForbidden))
		},
		Entry("member", claimsFor(3, "member", "jti-m")),
		Entry("request_only", claimsFor(4, "request_only", "jti-r")),
		Entry("no claims", nil),
	)

	It("returns an opaque flow id, never the Plex pin id", func() {
		servers.EXPECT().
			BeginPlexPin(mock.Anything).
			Return(mediaserver.PlexPin{
				ID:       upstreamPinID,
				AuthURL:  "https://app.plex.tv/auth#?code=abcd",
				ClientID: "cid",
			}, nil).
			Once()

		rr := begin(adminA())
		Expect(rr.Code).To(Equal(http.StatusOK))
		Expect(rr.Header().Get("Cache-Control")).To(Equal("no-store, private"))
		Expect(rr.Body.String()).NotTo(ContainSubstring("pin_id"))
		Expect(rr.Body.String()).NotTo(ContainSubstring("12345"))

		var body plexPinBeginResponse
		Expect(json.Unmarshal(rr.Body.Bytes(), &body)).To(Succeed())
		Expect(body.FlowID).To(HaveLen(64))
		Expect(body.AuthURL).To(Equal("https://app.plex.tv/auth#?code=abcd"))
		Expect(body.ClientID).To(Equal("cid"))
	})

	It("refuses a flow to an admin session that did not start it", func() {
		flowID := beginOK(adminA())

		Expect(poll(flowID, adminB()).Code).To(Equal(http.StatusNotFound))
	})

	It("refuses a flow to another session of the same admin", func() {
		flowID := beginOK(adminA())

		Expect(
			poll(flowID, claimsFor(1, "admin", "jti-other")).Code,
		).To(Equal(http.StatusNotFound))
	})

	It("refuses the raw upstream pin id as a flow id", func() {
		beginOK(adminA())

		Expect(poll("12345", adminA()).Code).To(Equal(http.StatusNotFound))
	})

	It("polls through to Plex for the owner and consumes the flow", func() {
		flowID := beginOK(adminA())
		servers.EXPECT().
			PollPlexPin(mock.Anything, upstreamPinID).
			Return(mediaserver.PlexPinResult{AuthToken: "tok"}, nil).
			Once()

		rr := poll(flowID, adminA())
		Expect(rr.Code).To(Equal(http.StatusOK))
		Expect(rr.Header().Get("Cache-Control")).To(Equal("no-store, private"))
		var body plexPinPollResponse
		Expect(json.Unmarshal(rr.Body.Bytes(), &body)).To(Succeed())
		Expect(body.AuthToken).To(Equal("tok"))

		Expect(poll(flowID, adminA()).Code).To(Equal(http.StatusNotFound))
	})

	It("keeps the flow alive while Plex reports the PIN pending", func() {
		flowID := beginOK(adminA())
		servers.EXPECT().
			PollPlexPin(mock.Anything, upstreamPinID).
			Return(mediaserver.PlexPinResult{}, nil).
			Twice()

		Expect(poll(flowID, adminA()).Code).To(Equal(http.StatusOK))
		Expect(poll(flowID, adminA()).Code).To(Equal(http.StatusOK))

		servers.EXPECT().
			PollPlexPin(mock.Anything, upstreamPinID).
			Return(mediaserver.PlexPinResult{AuthToken: "tok"}, nil).
			Once()
		rr := poll(flowID, adminA())
		Expect(rr.Code).To(Equal(http.StatusOK))
		var body plexPinPollResponse
		Expect(json.Unmarshal(rr.Body.Bytes(), &body)).To(Succeed())
		Expect(body.AuthToken).To(Equal("tok"))
	})

	It("consumes the flow once Plex reports the PIN expired", func() {
		flowID := beginOK(adminA())
		servers.EXPECT().
			PollPlexPin(mock.Anything, upstreamPinID).
			Return(mediaserver.PlexPinResult{Expired: true}, nil).
			Once()

		rr := poll(flowID, adminA())
		Expect(rr.Code).To(Equal(http.StatusOK))
		var body plexPinPollResponse
		Expect(json.Unmarshal(rr.Body.Bytes(), &body)).To(Succeed())
		Expect(body.Expired).To(BeTrue())

		Expect(poll(flowID, adminA()).Code).To(Equal(http.StatusNotFound))
	})

	It("refuses a flow whose TTL has passed", func() {
		flowID := beginOK(adminA())
		expirePlexFlow(handler.plexFlows, flowID)

		Expect(poll(flowID, adminA()).Code).To(Equal(http.StatusNotFound))
	})

	It("stores nothing when Plex refuses to start the flow", func() {
		servers.EXPECT().
			BeginPlexPin(mock.Anything).
			Return(mediaserver.PlexPin{}, errPlexUnreachable).
			Once()

		Expect(begin(adminA()).Code).To(Equal(http.StatusBadGateway))
		Expect(poll("12345", adminA()).Code).To(Equal(http.StatusNotFound))
	})
})
