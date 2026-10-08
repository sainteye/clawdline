package cloudops

// A paired browser can ask a machine it already controls to carry a pairing
// offer to a second machine. The offer reaches the helper inside the encrypted
// Cloud command; the Cloud API never receives the plaintext. The helper's own
// pairing route checks the offer's account and starts only the fixed task.
func init() {
	register(
		op{name: "pair-agent-start",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "offer", "machine_id", "machine_name") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				offer, validOffer := b.nonEmpty("offer")
				machineID, validID := b.nonEmpty("machine_id")
				name, validName := b.str("machine_name")
				if !ok || p.request == "" || !validOffer || len(offer) > 4096 ||
					!validID || len(machineID) > 204 || !validName || len(name) > 256 {
					return plan{}, false
				}
				p.document = jsonBody(map[string]any{"offer": offer, "machine_id": machineID, "machine_name": name})
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/cloud/pairing/agent", Body: p.document,
					Header: map[string]string{"Idempotency-Key": p.request}}
			}},
		op{name: "pair-agent-status", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "task_id") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				id, validID := b.nonEmpty("task_id")
				if !ok || !validID || len(id) != 36 {
					return plan{}, false
				}
				p.id = id
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/cloud/pairing/agent/" + segment(p.id)}
			}},
	)
}
