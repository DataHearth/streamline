package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/auth"
	"github.com/datahearth/streamline/internal/testutil/configtest"
)

// discovered stands in for an OIDC manager whose Init reached only the
// providers it names.
type discovered map[string]bool

func (discovered) Init(context.Context, string) {}

func (d discovered) Get(name string) (*auth.OIDCProvider, bool) {
	if !d[name] {
		return nil, false
	}
	return &auth.OIDCProvider{Name: name}, true
}

var _ = Describe("authConfig", Label("unit", "server"), func() {
	provider := func(name string) map[string]any {
		return map[string]any{
			"name": name, "issuer": "https://" + name + ".example",
			"client_id": "c", "client_secret": "s",
		}
	}

	providers := func(oidc auth.OIDCManager) []providerInfo {
		GinkgoHelper()
		configtest.Setup(map[string]any{"auth": map[string]any{
			"oidc": []any{provider("up"), provider("down")},
		}})
		rr := httptest.NewRecorder()
		New(
			Deps{OIDC: oidc},
		).authConfig(rr, httptest.NewRequest(http.MethodGet, "/auth/config", nil))
		Expect(rr.Code).To(Equal(http.StatusOK))
		var body authConfigResponse
		Expect(json.Unmarshal(rr.Body.Bytes(), &body)).To(Succeed())
		return body.Providers
	}

	It("lists only the providers whose discovery succeeded", func() {
		Expect(providers(discovered{"up": true})).
			To(Equal([]providerInfo{{Name: "up"}}))
	})

	It("lists none without an OIDC manager", func() {
		Expect(providers(nil)).To(BeEmpty())
	})
})
