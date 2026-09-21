package http

import (
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/icon"
)

// The shell a phone needs before any of this is an app on a home screen: the
// manifest, the marks, the launch images and the service worker.
//
// All five paths are on the gate's open list already and were answering 404,
// which is a particular kind of gap: a browser asks for `/favicon.ico` and the
// manifest **outside the page's own credentials**, before it knows who anybody
// is, so a route behind the gate is a home screen with no icon on it. The list
// was right and there was nothing behind it.
//
// Four of them are files in the console's bundle — `web/console/public/` — and
// are served by `page`. Three of those four are **byte copies** of what the
// Swift app draws: `icon-192.png`, `icon-512.png` and `favicon.ico` have a
// rounded tile in them, and AppKit's rasteriser for that one curve is not
// something a port matches to the pixel, so the bytes are taken rather than
// re-derived. `manifest.webmanifest` is a copy for the simpler reason that it
// is 488 bytes that must not drift. `sw.js` is a copy of
// `RemotePage.serviceWorker()` — see `tools/extract-service-worker.py`.
//
// The launch images are the one thing here that is drawn, and they have to be:
// there are twenty geometries, the list grows every autumn, and shipping them
// would be 1.1MB of PNG in a repository. They are axis-aligned rectangles on a
// flat ground, so `icon.Splash` is a faithful port rather than an approximation
// of one — see the note there.

func init() {
	// Registered rather than trusted. Go's built-in table has neither, and what
	// it falls back to is the system's — `/etc/apache2/mime.types` on a Mac,
	// nothing at all in a container — so the same bundle would be served with
	// three different types on three machines. A manifest sent as
	// `text/plain` is a home screen that installs without a name.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json; charset=utf-8")
	_ = mime.AddExtensionType(".ico", "image/x-icon")
}

// withPWA answers the paths that are drawn rather than filed, and hands
// everything else to the console.
func (s *Server) withPWA(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := routePath(r)
		if width, height, ok := splashPath(p); ok {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				writeRefusal(w, http.StatusMethodNotAllowed, "bad_request", "That is read with GET.")
				return
			}
			body, ok := icon.Splash(width, height)
			if !ok {
				writeRefusal(w, http.StatusNotFound, "not_found", "No splash that size")
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("Cache-Control", "public, max-age=86400")
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
			if r.Method == http.MethodHead {
				return
			}
			_, _ = w.Write(body)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// splashPath reads `/splash-1179x2556.png` — the pixel size of one particular
// iPhone. iOS names the device in a media query and asks for the image that
// fits it, so the sizes are not a list this end can know in advance.
func splashPath(p string) (int, int, bool) {
	if !strings.HasPrefix(p, "/splash-") || !strings.HasSuffix(p, ".png") {
		return 0, 0, false
	}
	body := strings.TrimSuffix(strings.TrimPrefix(p, "/splash-"), ".png")
	parts := strings.Split(body, "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	width, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	height, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	return width, height, true
}
