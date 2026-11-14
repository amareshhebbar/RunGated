package consoleui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func get(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func TestConsoleAssetsAreEmbedded(t *testing.T) {
	if !Available() {
		t.Fatal("console assets missing: run make console")
	}
	h := Handler()
	for _, p := range []string{"/", "/live/", "/agents/", "/incidents/", "/approvals/", "/pins/", "/policy/", "/audit/", "/settings/"} {
		rec := get(h, "GET", p)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "<html") {
			t.Fatalf("%s: %d", p, rec.Code)
		}
		if rec.Header().Get("X-Frame-Options") != "DENY" {
			t.Fatalf("%s: missing frame protection", p)
		}
	}
}

func TestConsoleRoutingRules(t *testing.T) {
	h := Handler()
	if rec := get(h, "GET", "/v1/unknown"); rec.Code != 404 || !strings.Contains(rec.Header().Get("Content-Type"), "json") {
		t.Fatalf("api path fallthrough: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := get(h, "GET", "/does/not/exist"); rec.Code != 404 {
		t.Fatalf("unknown page: %d", rec.Code)
	}
	if rec := get(h, "POST", "/"); rec.Code != 405 {
		t.Fatalf("post: %d", rec.Code)
	}
	if rec := get(h, "GET", "/../../etc/passwd"); rec.Code == 200 && strings.Contains(rec.Body.String(), "root:") {
		t.Fatal("path traversal")
	}
}
