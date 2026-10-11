package dashboard

import (
	"bytes"
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// dist is the web app built by `bun run build` in web/. A Go build without it
// still works and serves a page saying so.
//
//go:embed all:dist
var dist embed.FS

const missingAppPage = `<!doctype html>
<html lang="en">
<meta charset="utf-8">
<title>Tero</title>
<h1>Tero is running</h1>
<p>This build has no dashboard. Build it with <code>make build</code>.</p>
</html>
`

// webApp serves the built dashboard. Its hashed assets are cached for good;
// every other path gets index.html, and the app picks the page from the path.
type webApp struct {
	files fs.FS
}

func newWebApp() webApp {
	files, _ := fs.Sub(dist, "dist")
	return webApp{files: files}
}

func (a webApp) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	isReadOnly := r.Method == http.MethodGet || r.Method == http.MethodHead
	if !isReadOnly {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	isAsset := strings.HasPrefix(r.URL.Path, "/assets/")
	if isAsset {
		a.serveAsset(w, r)
		return
	}

	a.serveIndex(w)
}

func (a webApp) serveAsset(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	data, err := fs.ReadFile(a.files, name)
	if errors.Is(err, fs.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Something went wrong on the server", http.StatusInternalServerError)
		return
	}

	// Vite puts a content hash in every asset name, so a name never changes
	// meaning.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	// The zero time leaves out Last-Modified: embedded files have no real one.
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func (a webApp) serveIndex(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")

	index, err := fs.ReadFile(a.files, "index.html")
	if err != nil {
		w.Write([]byte(missingAppPage))
		return
	}

	w.Write(index)
}
