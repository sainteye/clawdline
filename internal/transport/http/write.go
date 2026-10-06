package http

import (
	"encoding/json"
	"net/http"

	"github.com/sainteye/clawdline/internal/contract"
	"github.com/sainteye/clawdline/internal/productcopy"
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
	writeRefusalWithSource(w, status, code, detail, true)
}

// writeRawRefusal keeps a detail assembled from runtime or external content
// ineligible for catalog lookup, even if its bytes equal a fixed sentence.
func writeRawRefusal(w http.ResponseWriter, status int, code, detail string) {
	writeRefusalWithSource(w, status, code, detail, false)
}

func writeRefusalWithSource(w http.ResponseWriter, status int, code, detail string, fixed bool) {
	key := ""
	if fixed {
		key = fixedRefusalKey(detail)
	}
	markFixedRefusalKey(w, key)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(contract.Refusal{Error: code, Detail: detail, DetailKey: key})
}

func fixedRefusalKey(detail string) string {
	return productcopy.HTTPRefusalKey(detail)
}

// The Cloud in-process recorder implements this narrow interface. Neither
// JSON text nor a header supplied by another handler can assert provenance.
func markFixedRefusalKey(w http.ResponseWriter, key string) {
	if key == "" {
		return
	}
	if recorder, ok := w.(interface{ ClawdlineFixedRefusalKey(string) }); ok {
		recorder.ClawdlineFixedRefusalKey(key)
	}
}

// writeRefusalAbout is writeRefusal for the two refusals that can name what
// they were pointed at: a route this daemon does not own, and an upstream it
// could not reach.
func writeRefusalAbout(w http.ResponseWriter, status int, code, detail string, about contract.Refusal) {
	writeRefusalAboutWithSource(w, status, code, detail, about, true)
}

func writeRawRefusalAbout(w http.ResponseWriter, status int, code, detail string, about contract.Refusal) {
	writeRefusalAboutWithSource(w, status, code, detail, about, false)
}

func writeRefusalAboutWithSource(w http.ResponseWriter, status int, code, detail string, about contract.Refusal, fixed bool) {
	about.Error = code
	about.Detail = detail
	about.DetailKey = ""
	if fixed {
		about.DetailKey = productcopy.HTTPRefusalKey(detail)
	}
	markFixedRefusalKey(w, about.DetailKey)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(about)
}

func closeRefusalWire(code, detail string, reasons []contract.CloseReason) contract.CloseRefusal {
	return closeRefusalWireWithSource(code, detail, reasons, true)
}

func closeRefusalWireRaw(code, detail string, reasons []contract.CloseReason) contract.CloseRefusal {
	return closeRefusalWireWithSource(code, detail, reasons, false)
}

func closeRefusalWireWithSource(code, detail string, reasons []contract.CloseReason, fixed bool) contract.CloseRefusal {
	key := ""
	if fixed {
		key = productcopy.HTTPRefusalKey(detail)
	}
	return contract.CloseRefusal{
		Error: code, Detail: detail, DetailKey: key, Reasons: reasons,
	}
}
