package http

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/sainteye/clawdline/internal/contract"
)

// writeJSON sends one contract value as the whole body.
//
// Every route goes through here so that the header, the encoder and the
// treatment of an encoding failure are decided once. The value is a generated
// type, never a literal map: a map has no name, so nothing downstream can be
// written against it and nothing can notice when it changes.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeRefusal sends the one shape every refusal on this daemon has.
//
// `code` is the machine-readable half and is the only part a caller may branch
// on. `detail` is for a person reading a log and must never be parsed.
func writeRefusal(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(contract.Refusal{Error: code, Detail: detail})
}

// writeRefusalAbout is writeRefusal for the two refusals that can name what
// they were pointed at: a route this daemon does not own, and an upstream it
// could not reach.
func writeRefusalAbout(w http.ResponseWriter, status int, code, detail string, about contract.Refusal) {
	about.Error = code
	about.Detail = detail
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(about)
}

// writeNoSuchRoute is the one answer for a path this daemon has no handler
// for: an unknown top-level /v1 route (the fallback) and an unknown sub-route
// under a prefix handler alike. 501 `not_implemented` naming the route, so a
// console newer than this daemon can say "this machine needs an update"
// instead of "could not load". A route that exists and names a record that
// does not answers 404 instead; the two must never share a status, because
// before 2026-10-06 they did and a newer console could not tell them apart.
func writeNoSuchRoute(w http.ResponseWriter, r *http.Request) {
	log.Printf("not implemented: %s %s", r.Method, r.URL.Path)
	writeRefusalAbout(w, http.StatusNotImplemented, "not_implemented",
		"this daemon does not own that route yet", contract.Refusal{Route: r.URL.Path})
}
