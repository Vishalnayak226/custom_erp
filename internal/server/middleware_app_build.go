package server

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// 297.1: every API reply names the build of the browser files on disk
// (X-ERP-Build: VIEW_MODULE_VERSION in public/app.js, which the release bumps
// whenever any file under public/ changes - gate G4). An open tab compares it
// with the build it loaded and reloads itself at the next safe moment, so a
// deploy never needs a hard refresh.

const appBuildHeaderName = "X-ERP-Build"

// appBuildRecheck is how long a read build number is trusted before the file's
// modification time is looked at again. A deploy that swaps public/ without a
// restart is seen within this window.
const appBuildRecheck = 10 * time.Second

var appBuildRe = regexp.MustCompile(`const VIEW_MODULE_VERSION = '(\d+)';`)

type appBuildSource struct {
	path    string
	mu      sync.Mutex
	value   string
	modTime time.Time
	checked time.Time
}

var appBuild = &appBuildSource{path: filepath.Join("public", "app.js")}

// get returns the build number, re-reading the file only when its modification
// time moved. A missing or unreadable file keeps the last good value ("" before
// the first), and no header is sent for "".
func (s *appBuildSource) get(now time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.checked.IsZero() && now.Sub(s.checked) < appBuildRecheck {
		return s.value
	}
	s.checked = now
	info, err := os.Stat(s.path)
	if err != nil || (s.value != "" && info.ModTime().Equal(s.modTime)) {
		return s.value
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return s.value
	}
	if m := appBuildRe.FindSubmatch(data); m != nil {
		s.value = string(m[1])
		s.modTime = info.ModTime()
	}
	return s.value
}

func appBuildHeader(next http.Handler) http.Handler {
	return appBuildHeaderFrom(appBuild, next)
}

func appBuildHeaderFrom(src *appBuildSource, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if v := src.get(time.Now()); v != "" {
				w.Header().Set(appBuildHeaderName, v)
			}
		}
		next.ServeHTTP(w, r)
	})
}
