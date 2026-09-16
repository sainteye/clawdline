package http

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// page serves the console itself: the document and every asset under it.
//
// The Swift app templates this document per request and a separate script
// re-implements the same fill statically for the hosted build. Two renderers of
// one document is one too many, so this is the only one here, and the hosted
// build will read it too.
type page struct{ root string }

func newPage(root string) *page { return &page{root: root} }

// WebRoot is where the console's files live. It is configuration rather than a
// constant because the bundle ships beside the binary in a release and sits in
// a checkout during development.
func WebRoot() string {
	if v := os.Getenv("CLAWDLINE_NEXT_WEB"); v != "" {
		return v
	}
	return ""
}

func (p *page) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.root == "" {
		writeRefusal(w, http.StatusNotImplemented, "no_web_root",
			"this daemon was not told where the console is; set CLAWDLINE_NEXT_WEB")
		return
	}
	clean := filepath.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
	if clean == "/" {
		p.document(w)
		return
	}
	// A path is resolved under the root and then checked to still be under it,
	// so a request cannot walk out of the bundle.
	full := filepath.Join(p.root, clean)
	if !strings.HasPrefix(full, p.root+string(filepath.Separator)) {
		writeRefusal(w, http.StatusBadRequest, "bad_path", "that path leaves the console")
		return
	}
	if _, err := os.Stat(full); err != nil {
		writeRefusal(w, http.StatusNotFound, "not_found", clean)
		return
	}
	http.ServeFile(w, r, full)
}

// document fills the slots the console's index.html leaves for its host.
func (p *page) document(w http.ResponseWriter) {
	body, err := os.ReadFile(filepath.Join(p.root, "index.html"))
	if err != nil {
		writeRefusal(w, http.StatusInternalServerError, "no_document", err.Error())
		return
	}
	html := string(body)
	html = strings.Replace(html, "<!-- clawdline:modules -->", p.modulePreloads(), 1)
	// The words, written in rather than asked for. Fetching them was the last
	// round trip in front of the first paint and the worst-placed one: the page
	// is deliberately blank until they land, so the request was a beat of
	// nothing on every load. Left as a comment when there is no catalog, and
	// the console falls back to the route.
	html = strings.Replace(html, "<!-- clawdline:strings -->", p.strings(), 1)
	// The cloud slot is left empty on purpose: this is a local console, and the
	// declaration is what tells the client which of its three transports it is.
	html = strings.Replace(html, "<!-- clawdline:cloud -->", "", 1)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

// strings returns the one line that carries the catalog into the document.
//
// It is JSON inside a script element, so the one sequence that could end the
// element early is escaped. Nothing else needs escaping: JSON's own encoder has
// already dealt with quotes and backslashes, and a second pass over them would
// corrupt the very strings it was meant to protect.
func (p *page) strings() string {
	body, err := os.ReadFile(filepath.Join(p.root, "strings", "zh-Hant.json"))
	if err != nil {
		return ""
	}
	var catalog map[string]any
	if json.Unmarshal(body, &catalog) != nil {
		return ""
	}
	catalog["lang"] = "zh-Hant"
	catalog["dir"] = "ltr"
	out, err := json.Marshal(catalog)
	if err != nil {
		return ""
	}
	return "<script>window.__strings=" +
		strings.ReplaceAll(string(out), "</", `<\/`) + "</script>"
}

func (p *page) modulePreloads() string {
	var out []string
	root := filepath.Join(p.root, "app", "js")
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".js") {
			return nil
		}
		rel, err := filepath.Rel(p.root, path)
		if err != nil {
			return nil
		}
		if strings.HasSuffix(rel, "app/js/main.js") {
			return nil // the entry is loaded, not preloaded
		}
		out = append(out, `<link rel="modulepreload" href="/`+filepath.ToSlash(rel)+`">`)
		return nil
	})
	sort.Strings(out)
	return strings.Join(out, "\n")
}
