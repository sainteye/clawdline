package orchestrator

import (
	"net/url"
	"sort"
	"strings"
)

// Remediation: the one request that clears a typed refusal, carried inside it
// (broker-design #37).
//
// The Swift app wrote the next step into each refusal's prose and into
// scripts beside it, and the prose drifted from the routes: `c9e89ce6` fixed a
// printed curl that sent the wrong header to the landing route, and the
// script it lived in was later deleted rather than kept right. A remedy here
// is not prose. It is built from Remedies — the route, its method and the
// credential it takes — and the transport's test serves every entry of that
// table against the real router and the real gate, so a remedy naming a route
// that does not exist, or a header the gate does not read, is a failing test
// rather than a caller's wasted request.

// The two credentials a broker route takes, as the gate reads them.
const (
	HeaderMachine    = "X-Clawdline-Orchestrator"
	HeaderTaskSecret = "X-Clawdline-Task-Secret"
)

// Remedy is the request a refusal names as its way out.
type Remedy struct {
	Method string `json:"method"`
	Route  string `json:"route"`
	// Header is the credential's header name; its value is never filled in.
	Header string `json:"header,omitempty"`
	// Query and Body name what the caller supplies, keyed as the route reads
	// them, with the values this refusal already knows filled in.
	Query map[string]string `json:"query,omitempty"`
	Body  map[string]any    `json:"body,omitempty"`
	// Command is the same request as one curl line, for a person or an agent
	// to paste. Placeholders are in angle brackets.
	Command string `json:"command"`
}

// RemedyRoute is one row of the table remedies are built from.
type RemedyRoute struct {
	Method string
	Route  string
	Header string
	// Query and Body are the keys the route reads, in order.
	Query []string
	Body  []string
}

// Remedies is every refusal code that has a request as its way out, and that
// request. The transport's test serves each one (remedy_test.go there).
var Remedies = map[string]RemedyRoute{
	// A dispatch that did not carry the inventory it read: read it.
	"stale_inventory": {Method: "GET", Route: "/v1/orchestrator/inventory", Header: HeaderMachine,
		Query: []string{"project"}},
	// The machine role's holder hands over by succession.
	"succession_required": {Method: "POST", Route: "/v1/orchestrator/coordinator/successions", Header: HeaderMachine,
		Body: []string{"coordinator_id", "expected_generation", "sender_session_id"}},
	// A handoff whose package is not written yet: write it, then send the
	// same request again.
	"bad_task": {Method: "POST", Route: "/v1/orchestrator/handoffs", Header: HeaderMachine,
		Body: []string{"handoff_id", "project_dir", "from_session", "coordinator_plain_handoff"}},
	// A sweep already running: its report is read, not started twice.
	"reclaim_running": {Method: "GET", Route: "/v1/orchestrator/reclaim", Header: HeaderMachine},
}

// RemedyFor builds the remedy for code, filling in what known says.
func RemedyFor(code string, known map[string]any) (Remedy, bool) {
	route, ok := Remedies[code]
	if !ok {
		return Remedy{}, false
	}
	rem := Remedy{Method: route.Method, Route: route.Route, Header: route.Header}
	target := route.Route
	if len(route.Query) > 0 {
		rem.Query = map[string]string{}
		q := url.Values{}
		for _, k := range route.Query {
			v := "<" + k + ">"
			if s, ok := known[k].(string); ok && s != "" {
				v = s
			}
			rem.Query[k] = v
			q.Set(k, v)
		}
		target += "?" + q.Encode()
	}
	parts := []string{"curl --fail-with-body -sS"}
	if route.Method != "GET" {
		parts = append(parts, "-X "+route.Method)
	}
	parts = append(parts, "'http://127.0.0.1:<port>"+target+"'")
	if route.Header != "" {
		placeholder := "<orchestrator token>"
		if route.Header == HeaderTaskSecret {
			placeholder = "<TASK_SECRET>"
		}
		parts = append(parts, "-H '"+route.Header+": "+placeholder+"'")
	}
	if len(route.Body) > 0 {
		rem.Body = map[string]any{}
		keys := append([]string{}, route.Body...)
		sort.Strings(keys)
		fields := []string{}
		for _, k := range keys {
			v, ok := known[k]
			if !ok || v == nil || v == "" {
				v = "<" + k + ">"
			}
			rem.Body[k] = v
			fields = append(fields, k)
		}
		parts = append(parts, "-H 'Content-Type: application/json'",
			"-d '{"+strings.Join(quoteKeys(fields), ",")+"}'")
	}
	rem.Command = strings.Join(parts, " ")
	return rem, true
}

func quoteKeys(keys []string) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = `"` + k + `":<` + k + `>`
	}
	return out
}

// withRemedy adds the remedy for code to a refusal's extras, when there is one.
func withRemedy(extra map[string]any, code string) map[string]any {
	if extra == nil {
		extra = map[string]any{}
	}
	if rem, ok := RemedyFor(code, extra); ok {
		extra["remediation"] = rem
	}
	return extra
}
