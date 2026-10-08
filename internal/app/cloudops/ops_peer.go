package cloudops

// A viewer may ask the named source machine to publish peer work, but the
// viewer key never becomes an Agent principal. The machine's local grant,
// execution and signing key are checked by the peer route itself.

import (
	"encoding/json"

	"github.com/sainteye/clawdline/internal/domain/agenthandoff"
)

func init() {
	register(op{name: "peer-send", decode: func(b body) (plan, bool) {
		if !b.has("type", "session", "request", "header", "body") {
			return plan{}, false
		}
		p, ok := actionPlan(b, false)
		if !ok || p.request == "" {
			return plan{}, false
		}
		text, ok := b.nonEmpty("body")
		if !ok || len(text) > agenthandoff.MaxBodyBytes {
			return plan{}, false
		}
		encoded, err := json.Marshal(b["header"])
		if err != nil {
			return plan{}, false
		}
		var request agenthandoff.Request
		if json.Unmarshal(encoded, &request) != nil || request.RequestID != p.request ||
			request.Source.MachineID == "" || request.Source.SessionID == "" {
			return plan{}, false
		}
		p.document = jsonBody(map[string]any{"request": request, "body": text})
		return p, true
	}, route: func(p plan) LocalRequest {
		return LocalRequest{Method: "POST", Path: "/v1/cloud/peer/send", Body: p.document}
	}})
	register(op{name: "peer-control", decode: func(b body) (plan, bool) {
		if !b.has("type", "session", "request", "control") {
			return plan{}, false
		}
		p, ok := actionPlan(b, false)
		if !ok || p.request == "" {
			return plan{}, false
		}
		control, ok := b["control"].(map[string]any)
		if !ok || len(control) == 0 || len(control) > 14 {
			return plan{}, false
		}
		action, _ := control["action"].(string)
		if action == "" {
			return plan{}, false
		}
		p.document = jsonBody(control)
		return p, len(p.document) <= 16<<10
	}, route: func(p plan) LocalRequest {
		return LocalRequest{Method: "POST", Path: "/v1/cloud/peer/control", Body: p.document}
	}})
	register(op{name: "peer-inbox", read: true, decode: func(b body) (plan, bool) {
		if !b.has("type", "session", "request", "expected_generation") &&
			!b.has("type", "session", "request", "expected_generation", "before") {
			return plan{}, false
		}
		p, ok := sessionPlan(b, "peer-inbox")
		request, requestOK := requestName(b["request"])
		generation, generationOK := b.nonEmpty("expected_generation")
		if !ok || !requestOK || !generationOK || !executionGenerationValid(generation) {
			return plan{}, false
		}
		p.request, p.name = request, "read:"+request
		p.executionGeneration = generation
		if before, exists := b["before"]; exists {
			id, ok := before.(string)
			if !ok || id == "" || len(id) > 128 {
				return plan{}, false
			}
			p.key = id
		}
		return p, true
	}, route: func(p plan) LocalRequest {
		query := map[string]string{"machine_id": p.id, "session_id": p.target,
			"execution_generation": p.executionGeneration}
		if p.key != "" {
			query["before"] = p.key
		}
		return LocalRequest{Method: "GET", Path: "/v1/cloud/peer/inbox", Query: query}
	}})
	register(op{name: "peer-outbox", read: true, decode: func(b body) (plan, bool) {
		if !b.has("type", "session", "request", "target_request") {
			return plan{}, false
		}
		p, ok := machinePlan(b)
		id, idOK := b.nonEmpty("target_request")
		if !ok || !idOK || len(id) > 128 {
			return plan{}, false
		}
		p.id = id
		return p, true
	}, route: func(p plan) LocalRequest {
		return LocalRequest{Method: "GET", Path: "/v1/cloud/peer/outbox/" + segment(p.id)}
	}})
}
