package consoleui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:assets
var assets embed.FS

func FS() fs.FS {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	return sub
}

func Available() bool {
	_, err := fs.Stat(FS(), "index.html")
	return err == nil
}

type handler struct {
	fsys fs.FS
	fs   http.Handler
}

func Handler() http.Handler {
	f := FS()
	return &handler{fsys: f, fs: http.FileServer(http.FS(f))}
}

func (h *handler) exists(name string) bool {
	st, err := fs.Stat(h.fsys, name)
	return err == nil && !st.IsDir()
}

func (h *handler) serveFile(w http.ResponseWriter, r *http.Request, name string, status int) {
	b, err := fs.ReadFile(h.fsys, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ct := "text/html; charset=utf-8"
	switch path.Ext(name) {
	case ".js":
		ct = "application/javascript"
	case ".css":
		ct = "text/css; charset=utf-8"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		w.Write(b)
	}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p := path.Clean("/" + r.URL.Path)
	if strings.HasPrefix(p, "/v1/") || strings.HasPrefix(p, "/llm/") || strings.HasPrefix(p, "/mcp/") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
		return
	}
	name := strings.TrimPrefix(p, "/")
	if name == "" {
		h.serveFile(w, r, "index.html", http.StatusOK)
		return
	}
	if strings.HasPrefix(name, "_next/") && h.exists(name) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		h.fs.ServeHTTP(w, r)
		return
	}
	if h.exists(name) {
		if strings.HasSuffix(name, ".html") {
			h.serveFile(w, r, name, http.StatusOK)
			return
		}
		h.fs.ServeHTTP(w, r)
		return
	}
	if h.exists(name + "/index.html") {
		h.serveFile(w, r, name+"/index.html", http.StatusOK)
		return
	}
	if path.Ext(name) == "" && h.exists("404.html") {
		h.serveFile(w, r, "404.html", http.StatusNotFound)
		return
	}
	http.NotFound(w, r)
}
