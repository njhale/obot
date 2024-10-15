package ui

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httputil"
	"path"
	"strings"
)

//go:embed admin/build*/client* admin/build*/client*/assets*/_*
var embedded embed.FS

func Handler(devPort int) http.Handler {
	if devPort == 0 {
		return http.HandlerFunc(serve)
	}
	rp := httputil.ReverseProxy{
		Director: func(r *http.Request) {
			r.URL.Scheme = "http"
			r.URL.Host = fmt.Sprintf("localhost:%d", devPort)
		},
	}
	return &rp
}

func serve(w http.ResponseWriter, r *http.Request) {
	if !strings.Contains(strings.ToLower(r.UserAgent()), "mozilla") {
		http.NotFound(w, r)
		return
	}

	path := path.Join("admin/build/client/", r.URL.Path)
	if _, err := fs.Stat(embedded, path); err == nil {
		http.ServeFileFS(w, r, embedded, path)
	} else {
		http.ServeFileFS(w, r, embedded, "admin/build/client/index.html")
	}
}