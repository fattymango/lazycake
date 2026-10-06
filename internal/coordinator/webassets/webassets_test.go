package webassets

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><title>shell</title>")},
		"assets/index-abc.js":  {Data: []byte("console.log(1)")},
		"assets/index-abc.css": {Data: []byte("body{}")},
	}
}

func get(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

// The bug this guards: refreshing on any non-root page ended in Chrome's
// "too many redirects" page, because the fallback went through
// http.FileServer, which redirects "/index.html" to "./" forever.
func TestDeepLinksServeTheShellWithoutRedirecting(t *testing.T) {
	h := handlerFor(testFS(), "index.html")
	for _, p := range []string{"/", "/tasks", "/tasks/tsk_01M48YQAC1WXCBJCNE5CQRRZQX", "/machines/add", "/tasks/", "/gateways/x/y/z"} {
		rec := get(h, http.MethodGet, p)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d (Location %q), want 200", p, rec.Code, rec.Header().Get("Location"))
			continue
		}
		if !strings.Contains(rec.Body.String(), "<title>shell</title>") {
			t.Errorf("GET %s did not return the app shell: %q", p, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("GET %s Content-Type = %q", p, ct)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("GET %s Cache-Control = %q, want no-cache (the shell must be revalidated)", p, cc)
		}
	}
}

func TestAssetsAreServedAndCachedForever(t *testing.T) {
	h := handlerFor(testFS(), "index.html")
	rec := get(h, http.MethodGet, "/assets/index-abc.js")
	if rec.Code != http.StatusOK || rec.Body.String() != "console.log(1)" {
		t.Fatalf("asset = %d %q", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("hashed asset Cache-Control = %q, want immutable", cc)
	}
}

// A stale hashed bundle (after a redeploy) must 404, not come back as HTML
// that the browser then tries to execute as JavaScript.
func TestMissingFilesAre404NotTheShell(t *testing.T) {
	h := handlerFor(testFS(), "index.html")
	for _, p := range []string{"/assets/index-OLD.js", "/favicon.ico", "/robots.txt"} {
		if rec := get(h, http.MethodGet, p); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
	}
}

func TestUnknownAPIPathsAreJSON404NotTheShell(t *testing.T) {
	h := handlerFor(testFS(), "index.html")
	rec := get(h, http.MethodGet, "/api/portal/nope")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "shell") {
		t.Fatal("an API typo returned the app shell")
	}
}

func TestHeadAndOtherMethods(t *testing.T) {
	h := handlerFor(testFS(), "index.html")
	if rec := get(h, http.MethodHead, "/tasks/x"); rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Fatalf("HEAD = %d with %d body bytes", rec.Code, rec.Body.Len())
	}
	if rec := get(h, http.MethodPost, "/tasks/x"); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d, want 405", rec.Code)
	}
}

func TestNoBuildIsA503NotACrash(t *testing.T) {
	h := handlerFor(fstest.MapFS{}, "index.html")
	if rec := get(h, http.MethodGet, "/tasks/x"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503 when the frontend isn't built", rec.Code)
	}
}
