package webui

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var distFS embed.FS

// Handler serves the embedded SPA bundle, setting immutable cache headers for
// hashed /assets/* files and falling back unknown non-/api GET routes to index.html.
func Handler() http.Handler {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		panic("webui: embedded dist filesystem missing: " + err.Error())
	}
	fileServer := http.FileServerFS(sub)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqPath := r.URL.Path

		// Never serve SPA index.html for API paths.
		if reqPath == "/api" || strings.HasPrefix(reqPath, "/api/") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error": "Not Found",
				"code":  "NOT_FOUND",
			})
			return
		}

		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		cleanPath := strings.TrimPrefix(path.Clean("/"+reqPath), "/")
		if cleanPath == "" || cleanPath == "." {
			serveIndexHTML(w, r, sub)
			return
		}

		info, err := fs.Stat(sub, cleanPath)
		if err == nil && !info.IsDir() {
			if cleanPath == "index.html" {
				serveIndexHTML(w, r, sub)
				return
			}
			if strings.HasPrefix(cleanPath, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "public, max-age=3600")
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		// Do not fall back missing asset files to index.html to avoid MIME mismatches.
		if strings.HasPrefix(cleanPath, "assets/") {
			http.NotFound(w, r)
			return
		}

		serveIndexHTML(w, r, sub)
	})
}

func serveIndexHTML(w http.ResponseWriter, r *http.Request, sub fs.FS) {
	data, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		http.Error(w, "WebUI assets unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
