package api

import (
	"bytes"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/ethosagent/meterwise/internal/contract"
)

// staticHandler serves the built web app. A path that names a file is served as
// is; any other extension-less GET falls back to index.html so client-side
// routes survive a reload. /api/* never reaches it (the mux sends it to the JSON
// 404). With no built app the answer is a typed not_found, never a stack trace.
func staticHandler(web fs.FS) http.Handler {
	files := http.FileServerFS(web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, contract.NewError(contract.CodeNotFound, "no such route"))
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if info, err := fs.Stat(web, name); err == nil && !info.IsDir() && name != indexName {
			setCache(w, name)
			files.ServeHTTP(w, r)
			return
		}
		if name == indexName || path.Ext(name) == "" {
			serveIndex(w, r, web)
			return
		}
		writeError(w, contract.NewError(contract.CodeNotFound, "no such file"))
	})
}

// setCache lets the browser keep hashed assets for a year and never keep the entry page.
func setCache(w http.ResponseWriter, name string) {
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
}

const indexName = "index.html"

// serveIndex answers with the app shell. It reads the file itself rather than
// going through http.FileServer, which would redirect /index.html to ./.
func serveIndex(w http.ResponseWriter, r *http.Request, web fs.FS) {
	b, err := fs.ReadFile(web, indexName)
	if err != nil {
		writeError(w, contract.NewError(contract.CodeNotFound, "the web app is not built (run npm run build in web/)"))
		return
	}
	setCache(w, indexName)
	http.ServeContent(w, r, indexName, time.Time{}, bytes.NewReader(b))
}
