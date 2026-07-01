package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDocsEndpoints(t *testing.T) {
	h := newTestRouter(t)

	// Swagger UI page.
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/docs status=%d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("/docs content-type=%q", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "SwaggerUIBundle") || !strings.Contains(body, "/openapi.yaml") {
		t.Fatalf("/docs body missing Swagger UI wiring")
	}

	// OpenAPI spec.
	req = httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/openapi.yaml status=%d", rec.Code)
	}
	spec := rec.Body.String()
	for _, want := range []string{"openapi: 3.0.3", "/texts/{URN}", "/texts/hash/{URN}", "HashResponse", "name: cex"} {
		if !strings.Contains(spec, want) {
			t.Fatalf("/openapi.yaml missing %q", want)
		}
	}
}
