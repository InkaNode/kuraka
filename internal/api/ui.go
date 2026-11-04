package api

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// csp allows only same-origin resources; the UI ships no inline code.
const csp = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; " +
	"connect-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'; " +
	"form-action 'self'; frame-ancestors 'none'"

// WithUI sets the file system holding the web panel (index.html at its root).
func (s *Server) WithUI(fsys fs.FS) *Server {
	s.ui = fsys
	return s
}

// serveUI serves static files and falls back to index.html for client-side
// routes, so deep links keep working.
func (s *Server) serveUI(w http.ResponseWriter, r *http.Request) {
	if s.ui == nil {
		writeError(w, 404, "not_found", "Resource not found")
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	if st, err := fs.Stat(s.ui, name); err == nil && !st.IsDir() {
		http.ServeFileFS(w, r, s.ui, name)
		return
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		writeError(w, 500, "internal", "Internal error")
		return
	}
	// Unknown asset paths are real 404s; anything else is an app route.
	if strings.HasPrefix(name, "assets/") {
		http.NotFound(w, r)
		return
	}
	http.ServeFileFS(w, r, s.ui, "index.html")
}
