// Serves the built web app (MONETA_WEB_DIR) next to the API, so production needs a single process and one origin.
package httpapi

import (
	"net/http"
	"path/filepath"
)

// webApp serves files from dir and falls back to index.html for client-side routes (/transactions, /import...).
func webApp(dir string) http.Handler {
	root := http.Dir(dir) // http.Dir rejects paths escaping dir
	files := http.FileServer(root)
	index := filepath.Join(dir, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if f, err := root.Open(r.URL.Path); err == nil {
			info, statErr := f.Stat()
			_ = f.Close()
			if statErr == nil && !info.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		http.ServeFile(w, r, index)
	})
}
