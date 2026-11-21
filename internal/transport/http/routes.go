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
// Explicit Bot input uploads and authenticated readback arrive at level 70.
// Live Bot roster streams are available from level 71.
// Human Bot routine management is available from level 73.
// A person's browser status, activity, audit, screenshot, control, person,
// open and mute requests accept a Bot target (bot_id with its conversation)
// from level 74; an older daemon ignores an unknown GET field, so a console
// must not name a Bot to a source below 74.
// Group rooms and the person's team management approvals arrive at level 76.
// Question-focused voice analysis arrives at level 77.
// Human artifact title changes arrive at level 78.
// Layered Bot memory and native configuration discovery arrive at level 79.
// Managed task-resource inspection, planning and guarded execution arrive at level 80.
// Per-Bot network settings management arrives at level 84.
// Main-chat reactions arrive at API 85 with Bot schema 11.
// Local Bot consent synchronization arrives at API 86 without a schema change.
// Project Session message boards arrive at level 87.
// Exact group quotes, room activity and optional list previews arrive at API 88.
// Named topic memory and on-demand native memory detail arrive at API 89.
// Source-owned room message reactions arrive at API 90.
// Session message board reply inboxes arrive at API 91.
// Source-owned Bot server tools and exact local rules arrive at API 92.
// Editable source-owned Bot handoffs and versioned input adoption arrive at API 93.
// Machine-authenticated callback Project Go-cache attachment arrives at API 94.
// Source-owned Bot sidebar actions arrive at API 95.
// Source-side Bot routine schedule previews arrive at API 96.
// Bots tower floor placements arrive at API 97.
// On-demand Project memory pages in Bots arrive at API 98.
// Project memory management and explicit review arrive at API 99.
// Grok Build as a Bot agent arrives at API 100.
// Group descriptions (what a group works on) arrive at API 101.
// Gemini as a Bot agent arrives at API 102.
// The lead Bot manages its team by role from API 103.
// Editing queued Bot messages arrives at API 104.
// Personal Bot workspaces can share a source-owned group from API 105.
// OpenAI Realtime dictation arrives at API 106.
const APILevel = 106

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
