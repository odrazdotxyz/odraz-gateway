package gateway

import (
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/odrazdotxyz/demo/api"
)

// The OpenAPI contract and the routes must agree: every route has an operation and every operation has a route.
func TestOpenAPIMatchesRoutes(t *testing.T) {
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(api.OpenAPI, &spec); err != nil {
		t.Fatal(err)
	}
	inSpec := map[string]bool{}
	for p, ops := range spec.Paths {
		for m := range ops {
			if m == "parameters" {
				continue
			}
			inSpec[strings.ToUpper(m)+" "+p] = true
		}
	}
	s := New(Config{Backends: []Backend{{ID: "x", Vendor: "nextcloud", URL: "http://x"}},
		DemoLogin: func(Backend) (string, string, bool) { return "", "", false }}) // the hook adds /auth/demo
	inCode := map[string]bool{}
	for _, r := range s.Routes() {
		inCode[r] = true
	}
	var missing, extra []string
	for r := range inCode {
		if !inSpec[r] {
			missing = append(missing, r)
		}
	}
	for r := range inSpec {
		if !inCode[r] {
			extra = append(extra, r)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("routes missing from api/openapi.yaml: %v", missing)
	}
	if len(extra) > 0 {
		t.Errorf("operations in api/openapi.yaml with no route: %v", extra)
	}
}
