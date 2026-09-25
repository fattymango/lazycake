// Package webassets embeds the built web/ output (PLAN.md §6: "Built
// assets go:embed'd into the coordinator binary exactly like index.html is
// today") so the two portal SPAs ship inside the single coordinator binary
// with no separate static-file deploy step.
//
// dist/ is checked in as an empty placeholder (see dist/.gitkeep) so
// `go build ./...` works with no frontend build present; it's populated by
// `npm run build` in web/ (task 7.6's verify step) copying web/dist's
// contents here, which deploy/Dockerfile's node build stage does before the
// Go build stage runs.
//
// This package is not yet wired into cmd/coordinator/run.go: that wiring
// belongs with Phase 7A's portalapi package (docs/01-dashboard-portals/
// IMPLEMENTATION.md tasks 7.2-7.5), which had not landed as of this
// frontend build (7.6-7.9). Handler here is what run.go should mount at
// "/", "/customer.html"+"/tasks"+"/gateways"+... , and "/provider.html"+
// "/machines"+... once that lands (task 7.10).
package webassets

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the embedded build output rooted at dist/, e.g. FS().Open("index.html").
func FS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// Only possible if the embed directive above and this Sub call ever
		// drift apart - a build-time programming error, not a runtime one.
		panic(err)
	}
	return sub
}

// Handler serves the embedded build output as static files, falling back
// to entryFile (e.g. "customer.html" or "provider.html") for any path that
// isn't a real file on disk - the standard single-page-app pattern, so
// client-side routes like /tasks/abc123 load the app instead of 404ing.
// entryFile == "" serves the root chooser page (index.html) with no
// fallback, since it has no client-side routes of its own.
func Handler(entryFile string) http.Handler {
	fsys := FS()
	fileServer := http.FileServer(http.FS(fsys))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if entryFile == "" {
			fileServer.ServeHTTP(w, r)
			return
		}

		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if clean == "" || clean == "." {
			clean = entryFile
		}
		if _, err := fs.Stat(fsys, clean); err != nil {
			// Not a real asset (JS/CSS/etc) - it's a client-side route, so
			// serve the SPA's entry point and let react-router take over.
			r2 := new(http.Request)
			*r2 = *r
			r2.URL.Path = "/" + entryFile
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
}
