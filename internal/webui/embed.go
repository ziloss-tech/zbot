package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed frontend/dist
var frontendFS embed.FS

// frontendHandler serves the React SPA from the embedded frontend/dist directory.
// All non-API, non-static paths fall back to index.html for client-side routing.
func frontendHandler() http.Handler {
	dist, err := fs.Sub(frontendFS, "frontend/dist")
	if err != nil {
		// If frontend/dist doesn't exist, return a handler that 404s.
		return http.NotFoundHandler()
	}

	fileServer := http.FileServer(http.FS(dist))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file directly first.
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		// Check if the file exists in the embedded FS.
		if f, err := dist.Open(path); err == nil {
			f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback — serve index.html for client-side routing.
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}
