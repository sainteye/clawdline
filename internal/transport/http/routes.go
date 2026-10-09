package http

import (
	"net/http"
	"sort"
)

// APILevel says which set of routes this build answers. It is the
// `api_level` of api/v1/routes.json, reported on GET /v1/health and in the
// Cloud machine descriptor, and it rises every time a route is added or
// removed: `go run ./tools/contract-gen` refuses to write a changed route set
// at the level the file already records, and `-check` fails while the table
// and the file disagree.
//
// It exists because app.clawdline.com deploys every commit and the daemons
// it reads do not. On 2026-10-04 a newer console said 「讀取失敗」 beside
// features an older daemon simply lacked (docs/updates.md). A console that
// knows the level can say "this machine needs an update" before it asks.
const APILevel = 5

// Route is one pattern this daemon registers. Method is "*" for a handler
// that judges the method itself, which today is every one of them: Go's mux
// matches prefixes, so most handlers also hold every sub-route under their
// pattern, and an unknown one answers 501 `not_implemented`
// (writeNoSuchRoute). A sub-route added under an existing prefix is therefore
// not a change to this table — say so by raising APILevel by hand when a
// console has to know about it.
type Route struct {
	Method  string `json:"method"`
	Pattern string `json:"pattern"`
}

// SetVersion names this build for GET /v1/health. The version is the
// command's (`main.version`, stamped by the release build), so the command
// hands it over rather than this package guessing it.
func (s *Server) SetVersion(v string) { s.version = v }

type route struct {
	Route
	handle http.HandlerFunc
}

// Routes is the route table without its handlers, sorted by pattern: what
// contract-gen writes into api/v1/routes.json.
func Routes() []Route {
	table := (&Server{}).routeTable()
	out := make([]Route, 0, len(table))
	for _, rt := range table {
		out = append(out, rt.Route)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pattern != out[j].Pattern {
			return out[i].Pattern < out[j].Pattern
		}
		return out[i].Method < out[j].Method
	})
	return out
}
