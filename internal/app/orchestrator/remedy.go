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
//
// **A route that is registered is not a route that does anything.** The check
// began as "some mux pattern matches this path", and a prefix registration
// matches a path whose handler answers `501 not_implemented`: the succession
// remedy named `/v1/orchestrator/coordinator/successions`, that route said the
// feature does not exist, and the refusal was still sending people to it. So
// a row may also say, in the table rather than in prose, that this build has
// **no** request that clears its refusal — `Because` then says what is
// missing, `Command` is empty, and `Available` is false. A caller that reads
// the field learns it cannot act; one that follows the curl line has no curl
// line to follow. The transport's test serves every row that names a route and
// fails on a `501`, which is what makes the distinction hold.

// The two credentials a broker route takes, as the gate reads them.
const (
	HeaderMachine    = "X-Clawdline-Orchestrator"
	HeaderTaskSecret = "X-Clawdline-Task-Secret"
)

// Remedy is the request a refusal names as its way out, or — when this build
// has none — the reason there is nothing to send.
type Remedy struct {
	// Available is false when no request on this daemon clears the refusal.
	// It is always written, so a caller branches on a field rather than on
	// whether a string happens to be empty.
	Available bool   `json:"available"`
	Method    string `json:"method,omitempty"`
	Route     string `json:"route,omitempty"`
	// Header is the credential's header name; its value is never filled in.
	Header string `json:"header,omitempty"`
	// Query and Body name what the caller supplies, keyed as the route reads
	// them, with the values this refusal already knows filled in.
	Query map[string]string `json:"query,omitempty"`
	Body  map[string]any    `json:"body,omitempty"`
	// Command is the same request as one curl line, for a person or an agent
	// to paste. Placeholders are in angle brackets. Empty when Available is
	// false: there is no line that would work, and a line that does not work
	// is what this whole mechanism exists to stop.
	Command string `json:"command,omitempty"`
	// Because is why there is nothing to send, when Available is false: what
	// this build is missing, and what a caller may do instead.
	Because string `json:"because,omitempty"`
}

// RemedyRoute is one row of the table remedies are built from. A row with an
// empty Route is a refusal this build cannot clear: Because says why, and no
// request is offered at all.
type RemedyRoute struct {
	Method string
	Route  string
	Header string
	// Query and Body are the keys the route reads, in order.
	Query []string
	Body  []string
	// Because is what is missing, for a row that names no route.
	Because string
}

// Remedies is every refusal code that has a request as its way out, and that
// request. The transport's test serves each one (remedy_test.go there).
var Remedies = map[string]RemedyRoute{
	// A dispatch that did not carry the inventory it read: read it.
	"stale_inventory": {Method: "GET", Route: "/v1/orchestrator/inventory", Header: HeaderMachine,
		Query: []string{"project"}},
	// The machine role's holder cannot hand the role over on this build.
	// Succession is not implemented — `/v1/orchestrator/coordinator/successions`
	// answers 501 — and `/rebind` is not a substitute: it moves the role only
	// after a reading proves the bound session offline, so a live holder
	// asking for it is told `coordinator_online`. There is no request, and
	// saying so is the remedy.
	"succession_required": {Because: "Succession — moving the machine role together with the work — is not " +
		"implemented on this daemon, and /v1/orchestrator/coordinator/rebind is not a way round it: it moves the " +
		"role only after a reading proves the bound session offline, and answers coordinator_online while the " +
		"holder is live. So nothing sent now moves the role. What does work: this session's work can go over as " +
		"an ordinary handoff once it no longer holds the role, and once this session is gone the receiver moves " +
		"the role itself with POST /v1/orchestrator/coordinator/rebind, using the id and generation from " +
		"GET /v1/orchestrator/coordinator."},
	// A handoff whose package is not written yet: write it, then send the
	// same request again.
	"bad_task": {Method: "POST", Route: "/v1/orchestrator/handoffs", Header: HeaderMachine,
		Body: []string{"handoff_id", "project_dir", "from_session", "coordinator_plain_handoff"}},
	// A sweep already running: its report is read, not started twice.
	"reclaim_running": {Method: "GET", Route: "/v1/orchestrator/reclaim", Header: HeaderMachine},
}

// RemedyFor builds the remedy for code, filling in what known says. A row
// that names no route answers the honest empty-handed remedy instead of a
// curl line to a route that would refuse it.
func RemedyFor(code string, known map[string]any) (Remedy, bool) {
	route, ok := Remedies[code]
	if !ok {
		return Remedy{}, false
	}
	if route.Route == "" {
		return Remedy{Available: false, Because: route.Because}, true
	}
	rem := Remedy{Available: true, Method: route.Method, Route: route.Route, Header: route.Header}
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
