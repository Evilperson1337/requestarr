package webui

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

// Handler serves the embedded Vite build and falls back to index.html only for
// browser routes. API and auth namespaces are never treated as SPA routes.
func Handler() http.Handler {
	root, err := fs.Sub(assets, "dist")
	if err != nil {
		panic("embedded web assets missing")
	}
	files := http.FS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "requests" { // file server redirects directory-like paths ending in a slash; routes are SPA paths.
			r.URL.Path = "/requests/"
		}
		if strings.HasPrefix(name, "api/") || name == "api" || strings.HasPrefix(name, "auth/") || name == "auth" {
			http.NotFound(w, r)
			return
		}
		if name == "" || name == "." {
			name = "index.html"
		}
		f, err := root.Open(name)
		if err == nil {
			_ = f.Close()
			if ext := path.Ext(name); ext != "" {
				if typ := mime.TypeByExtension(ext); typ != "" {
					w.Header().Set("Content-Type", typ)
				}
			}
			if strings.HasPrefix(name, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			serveFile(files, name, w, r)
			return
		}
		if path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		serveFile(files, "index.html", w, r)
	})
}

func serveFile(fsys http.FileSystem, name string, w http.ResponseWriter, r *http.Request) {
	f, err := fsys.Open("/" + name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, name, info.ModTime(), f)
}
