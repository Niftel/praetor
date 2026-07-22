package tests

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/praetordev/praetor/services/api"
)

// TestHelmAPIProbeRoutes prevents the deployable chart and API router from
// drifting apart. A renamed or mistyped probe path must fail before merge,
// rather than surfacing as a 404 during a cluster rollout.
func TestHelmAPIProbeRoutes(t *testing.T) {
	chartPath := filepath.Join("..", "deployments", "helm", "praetor-v2", "templates", "api.yaml")
	chart, err := os.ReadFile(chartPath)
	if err != nil {
		t.Fatalf("read API Helm template: %v", err)
	}

	probePath := regexp.MustCompile(`(?ms)(readinessProbe|livenessProbe):.*?httpGet:.*?path:\s+(\S+)`)
	matches := probePath.FindAllSubmatch(chart, -1)
	if len(matches) != 2 {
		t.Fatalf("API Helm template contains %d HTTP probe paths, want readiness and liveness", len(matches))
	}

	router, err := api.NewRouter(nil, api.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range matches {
		probe := string(match[1])
		path := string(match[2])
		t.Run(probe, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code == http.StatusNotFound {
				t.Fatalf("Helm probe path %q is not registered by the API router", path)
			}
			expected := http.StatusOK
			if probe == "readinessProbe" {
				// The nil database deliberately makes readiness return 503 while
				// still proving that the configured route exists and executes.
				expected = http.StatusServiceUnavailable
			}
			if rec.Code != expected {
				t.Fatalf("Helm %s path %q returned %d, want %d", probe, path, rec.Code, expected)
			}
		})
	}
}
