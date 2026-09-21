package http

import (
	"encoding/json"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sainteye/clawdline-go/internal/contract"
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

// consoleReading is what `/` would answer now, without asking it.
//
// It exists because the question "is the console back" was answered with
// /v1/health on 2026-09-21, twice, and health was right both times: the daemon
// was alive. It had been started without CLAWDLINE_NEXT_WEB, `/` answered 501
// to everybody, and nothing on the startup path said so.
func consoleReading(root string) contract.ConsoleDiagnostics {
	if root == "" {
		return contract.ConsoleDiagnostics{
			State:  contract.ConsoleStateNone,
			Detail: "CLAWDLINE_NEXT_WEB is not set, so / answers 501 no_web_root and this address shows no page",
		}
	}
	index := filepath.Join(root, "index.html")
	if _, err := os.Stat(index); err != nil {
		// A bundle being rebuilt underneath a running daemon is one way here:
		// tools/package-macos.sh removes the app before it copies the console.
		return contract.ConsoleDiagnostics{
			State:  contract.ConsoleStateBroken,
			Root:   root,
			Detail: "CLAWDLINE_NEXT_WEB has no index.html, so / answers 500 no_document: " + err.Error(),
		}
	}
	return contract.ConsoleDiagnostics{State: contract.ConsoleStateServed, Root: root, Detail: "served from " + root}
}

// consoleLogLine is the startup line beside `listening`, which is the one line
// of this daemon's log that somebody reads after a restart.
func consoleLogLine(c contract.ConsoleDiagnostics) string {
	switch c.State {
	case contract.ConsoleStateServed:
		return "console: " + c.Detail
	case contract.ConsoleStateNone:
		return "console: NONE — this daemon was not told where the console is: " + c.Detail +
			". The API, the broker and the cloud line run without it; a browser here gets a refusal, not a page."
	default:
		return "console: BROKEN — " + c.Detail
	}
}

func (p *page) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if p.root == "" {
		writeRefusal(w, http.StatusNotImplemented, "no_web_root",
			"this daemon was not told where the console is; start it with CLAWDLINE_NEXT_WEB set to the console's built files")
		return
	}
	// A URL path is a slash path whatever this machine spells its own paths
	// with, so it is cleaned with path.Clean and only then turned into one.
	// filepath.Clean cleaned "/" to a lone backslash on Windows, so the console's own
	// document never matched and every load answered `bad_path`.
	clean := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
	if clean == "/" {
		p.document(w)
		return
	}
	// A path is resolved under the root and then checked to still be under it,
	// so a request cannot walk out of the bundle.
	full := filepath.Join(p.root, filepath.FromSlash(clean))
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
