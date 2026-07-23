package server

import (
	"io/fs"
	"net/http"

	"github.com/ygrip/punakawan/internal/panel/assets"
)

// staticHandler serves the embedded frontend build, falling back to
// index.html for any path that has no matching file - the standard SPA
// routing pattern, since the frontend's own router (once one exists)
// owns paths like /workspaces/checkout-platform, which have no file on
// disk.
func staticHandler() (http.Handler, error) {
	sub, err := fs.Sub(assets.Dist, assets.DistDir)
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(sub, trimLeadingSlash(r.URL.Path)); err != nil {
			r2 := r.Clone(r.Context())
			r2.URL.Path = "/"
			fileServer.ServeHTTP(w, r2)
			return
		}
		fileServer.ServeHTTP(w, r)
	}), nil
}

func trimLeadingSlash(p string) string {
	if p == "" || p == "/" {
		return "index.html"
	}
	if p[0] == '/' {
		return p[1:]
	}
	return p
}
