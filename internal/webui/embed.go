package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:assets
var embedded embed.FS

func Handler() http.Handler {
	assets, err := fs.Sub(embedded, "assets")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(assets))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if requested == "." || requested == "" || requested == "index.html" {
			serveIndex(w, assets)
			return
		}
		if _, err := fs.Stat(assets, requested); err != nil {
			serveIndex(w, assets)
			return
		}
		r.URL.Path = "/" + requested
		files.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, assets fs.FS) {
	body, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		http.Error(w, "SuperMonitor frontend is not built. Run `npm run build` in web/.", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
