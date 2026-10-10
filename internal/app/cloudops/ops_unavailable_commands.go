package cloudops

import (
	"encoding/json"
)

func init() {
	register(
		// MARK: commands this daemon refuses or has no local capability for

		op{name: "dispatch", refusal: &cloudDispatchUnpinned, anyClass: true,
			decode: func(b body) (plan, bool) {
				// Only enough to name the answer channel. The refusal is the
				// whole answer, and parsing a task this machine will not run
				// would be pretending otherwise. A dispatch that named no
				// request — which is what the hosted console sends today — is
				// still refused, as a notice with nowhere to publish rather
				// than as silence.
				if raw, named := b["session"]; named {
					if id, ok := raw.(string); !ok || id != MachineReplySession {
						return plan{}, false
					}
				}
				request, ok := requestName(b["request"])
				if !ok {
					return plan{}, true
				}
				return plan{session: MachineReplySession, request: request,
					name: "action:" + request}, true
			}},

		op{name: "diagnostics.report", readLevel: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "report") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				// A `report` that is not an object still reaches the store,
				// which names it `report_not_json`, so anything JSON is taken
				// here and judged there.
				raw, err := json.Marshal(b["report"])
				if err != nil {
					return plan{}, false
				}
				p.document = raw
				return p, true
			}},

		op{name: "diagnostics.events", readLevel: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "batch") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				batch, ok := b.object("batch", ViewerEventsBatchBytesLimit)
				if !ok {
					return plan{}, false
				}
				p.document = batch
				return p, true
			}},
	)
}
