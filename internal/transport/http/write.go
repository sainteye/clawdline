package http

import (
	"encoding/json"
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
