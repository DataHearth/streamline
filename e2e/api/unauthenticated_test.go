package api

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"

	"github.com/knadh/koanf/parsers/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	streamlineapi "github.com/datahearth/streamline/api"
)

// apiRoute is one operation from the embedded OpenAPI spec, with its path
// template's {param} segments already filled with a throwaway value.
type apiRoute struct {
	method string
	path   string
}

// pathParamToken matches a {param} segment in a spec path template.
var pathParamToken = regexp.MustCompile(`\{([^}]+)\}`)

// pathParamPlaceholder maps a path parameter's name to the throwaway value
// the sweep sends in its place. Any name not listed here is treated as a
// numeric id. The auth middleware runs before chi resolves the route, so
// nothing downstream ever sees these substituted values.
var pathParamPlaceholder = map[string]string{
	"hash": hash40,
	"name": "e2e",
}

// concretePath fills every {param} in a spec path template with a throwaway
// value.
func concretePath(template string) string {
	return pathParamToken.ReplaceAllStringFunc(template, func(token string) string {
		name := token[1 : len(token)-1]
		if v, ok := pathParamPlaceholder[name]; ok {
			return v
		}
		return "1"
	})
}

// operationKeys maps a path item's operation keys to their HTTP method. A
// path item also carries non-operation keys (`parameters`, `summary`,
// `description`), so the sweep allow-lists rather than treating every key
// under a path as a method.
var operationKeys = map[string]string{
	"get":     http.MethodGet,
	"put":     http.MethodPut,
	"post":    http.MethodPost,
	"delete":  http.MethodDelete,
	"options": http.MethodOptions,
	"head":    http.MethodHead,
	"patch":   http.MethodPatch,
	"trace":   http.MethodTrace,
}

// specRoutes walks every path+method declared under /api/v1 in the embedded
// OpenAPI spec (api/openapi.yaml, embedded as streamlineapi.OpenAPISpec), so
// the sweep tracks the spec automatically instead of a hand-maintained list
// that can silently stop covering routes added later.
//
// Plain YAML rather than an OpenAPI loader: path + method is the whole of what
// the sweep needs, and the spec's `$ref`s are all under components. A path item
// that was itself a `$ref` would be missed — nothing in the spec is written
// that way, and `$ref`-only paths are the one shape to avoid here.
func specRoutes() []apiRoute {
	GinkgoHelper()
	doc, err := yaml.Parser().Unmarshal(streamlineapi.OpenAPISpec)
	Expect(err).NotTo(HaveOccurred())
	paths, ok := doc["paths"].(map[string]any)
	Expect(ok).To(BeTrue(), "spec has no paths mapping")

	var routes []apiRoute
	for path, item := range paths {
		operations, ok := item.(map[string]any)
		Expect(ok).To(BeTrue(), "path %s is not a mapping", path)
		for key := range operations {
			method, isOperation := operationKeys[key]
			if !isOperation {
				continue
			}
			routes = append(
				routes,
				apiRoute{method: method, path: concretePath(path)},
			)
		}
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].path != routes[j].path {
			return routes[i].path < routes[j].path
		}
		return routes[i].method < routes[j].method
	})
	return routes
}

var _ = Describe("REST API authentication sweep", Label("e2e"), func() {
	It("401s every route declared in api/openapi.yaml without credentials", func() {
		routes := specRoutes()
		// A loader that silently returned nothing would make every route below
		// vacuously pass, so guard against that before trusting the sweep.
		Expect(routes).NotTo(BeEmpty())
		for _, route := range routes {
			By(fmt.Sprintf("%s %s", route.method, route.path))
			resp := do(route.method, "/api/v1"+route.path, anon, nil)
			Expect(resp.StatusCode).To(
				Equal(http.StatusUnauthorized),
				"%s %s", route.method, route.path,
			)
			Expect(resp.Body.Close()).To(Succeed())
		}
	})
})
