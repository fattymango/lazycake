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
// cmd/coordinator mounts Handler("index.html") at "/".
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
// to entryFile (e.g. "index.html") for any path that isn't a real file - the
// standard single-page-app pattern, so client-side routes like
// /tasks/abc123 load the app instead of 404ing, including when the browser
// is refreshed on one. entryFile == "" serves the build output with no
// fallback.
func Handler(entryFile string) http.Handler {
	return handlerFor(FS(), entryFile)
}

// handlerFor is Handler over an arbitrary filesystem, so tests don't depend
// on whether a frontend build is present in dist/.
//
// The fallback writes the entry file's bytes itself. It must not rewrite
// the request path to "/index.html" and hand it to http.FileServer: the
// file server answers any path ending in "/index.html" with a redirect to
// "./", which for a deep link like /tasks/abc resolves to /tasks/, which
// falls back and redirects again - an infinite loop that the browser
// reports as ERR_TOO_MANY_REDIRECTS (a refresh on any non-root page).
func handlerFor(fsys fs.FS, entryFile string) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if entryFile == "" {
			fileServer.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		clean := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if clean != "" {
			if info, err := fs.Stat(fsys, clean); err == nil && !info.IsDir() {
				if strings.HasPrefix(clean, "assets/") {
					// Vite content-hashes everything under assets/, so a given
					// URL never changes meaning.
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// Anything under /api/ that reached here is a typo'd endpoint: say so
		// as JSON instead of returning the app shell with a 200.
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error":"not found"}`))
			return
		}
		// A missing file (it has an extension: a stale hashed bundle after a
		// redeploy, a favicon that doesn't exist) is a real 404. Returning the
		// HTML shell for it makes the browser try to run HTML as JavaScript.
		if path.Ext(clean) != "" {
			http.NotFound(w, r)
			return
		}

		body, err := fs.ReadFile(fsys, entryFile)
		if err != nil {
			http.Error(w, "frontend not built", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// The shell references hashed bundles; it must be revalidated on every
		// load or a user keeps an old shell pointing at deleted files.
		w.Header().Set("Cache-Control", "no-cache")
		if r.Method == http.MethodHead {
			return
		}
		w.Write(body)
	})
}
