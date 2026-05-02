package webui

import (
	"io/fs"
	"net/http"
	"strings"
)

// apiPrefixes are URL prefixes reserved for backend APIs.
// Requests matching these are never served by the SPA handler.
var apiPrefixes = []string{"/v1/", "/ws", "/health", "/mcp/"}

// cspHeader replaces the meta-tag CSP and adds worker-src for Vault Graph View
// (sigma.js creates Web Workers via blob URLs).
const cspHeader = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com https://cdn.jsdelivr.net; font-src 'self' data: https://fonts.gstatic.com https://cdn.jsdelivr.net; img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self' ws: wss:; worker-src 'self' blob:; base-uri 'self';"

// Handler returns an http.Handler that serves the embedded SPA.
// Returns nil if no assets are embedded (built without embedui tag).
func Handler() http.Handler {
	fsys := Assets()
	if fsys == nil {
		return nil
	}
	fileServer := http.FileServer(http.FS(fsys))
	return &spaHandler{fs: fsys, fileServer: fileServer}
}

type spaHandler struct {
	fs         fs.FS
	fileServer http.Handler
}

func (h *spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Never intercept API routes.
	for _, prefix := range apiPrefixes {
		if strings.HasPrefix(r.URL.Path, prefix) {
			http.NotFound(w, r)
			return
		}
	}

	// Try to serve the file directly.
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}

	// Check if file exists in the embedded FS.
	if _, err := fs.Stat(h.fs, path); err == nil {
		// Static assets: set long cache for /assets/* (Vite hashed filenames).
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}

		// index.html: strip the meta CSP tag and send it via HTTP header instead.
		// This allows us to add worker-src without rebuilding the UI bundle.
		if path == "index.html" {
			h.serveIndexHTML(w, r)
			return
		}

		h.fileServer.ServeHTTP(w, r)
		return
	}

	// SPA fallback: serve index.html for any unmatched route.
	// This handles client-side routing (React Router).
	h.serveIndexHTML(w, r)
}

// serveIndexHTML reads index.html from the embedded FS, removes the
// <meta http-equiv="Content-Security-Policy"> tag, and serves the rest
// with the CSP delivered as an HTTP header (which overrides/broadens
// the policy and can include worker-src blob:).
func (h *spaHandler) serveIndexHTML(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(h.fs, "index.html")
	if err != nil {
		http.Error(w, "index.html not found", http.StatusInternalServerError)
		return
	}

	html := string(data)

	// Remove the meta CSP tag so it doesn't collide with the HTTP header.
	// The tag is a single line: <meta http-equiv="Content-Security-Policy" content="..." />
	if start := strings.Index(html, `<meta http-equiv="Content-Security-Policy"`); start >= 0 {
		if end := strings.Index(html[start:], `/>`); end >= 0 {
			html = html[:start] + html[start+end+2:]
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", cspHeader)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))
}
