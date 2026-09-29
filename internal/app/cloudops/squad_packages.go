package cloudops

import (
	"github.com/sainteye/clawdline/internal/domain/squadpack"
)

func packageCloudPlan(action bool) func(body) (plan, bool) {
	return func(b body) (plan, bool) {
		if !b.has("type", "session", "request", "package") {
			return plan{}, false
		}
		var p plan
		var ok bool
		if action {
			p, ok = actionPlan(b, false)
		} else {
			p, ok = machinePlan(b)
		}
		if !ok || p.request == "" {
			return plan{}, false
		}
		p.document, ok = b.object("package", squadpack.MaxRequestBytes)
		return p, ok
	}
}

func packagePublicExportPlan(b body) (plan, bool) {
	p, ok := packageCloudPlan(false)(b)
	if !ok {
		return plan{}, false
	}
	v, ok := b["package"].(map[string]any)
	if !ok || len(v) != 2 {
		return plan{}, false
	}
	scopes, ok := v["private_scopes"].([]any)
	if !ok || len(scopes) != 0 || v["confirm_private"] != false {
		return plan{}, false
	}
	return p, true
}

func packagePrivateExportPlan(b body) (plan, bool) {
	p, ok := packageCloudPlan(true)(b)
	if !ok {
		return plan{}, false
	}
	v, ok := b["package"].(map[string]any)
	if !ok || len(v) != 2 {
		return plan{}, false
	}
	scopes, ok := v["private_scopes"].([]any)
	if !ok || len(scopes) == 0 || v["confirm_private"] != true {
		return plan{}, false
	}
	for _, scope := range scopes {
		if s, ok := scope.(string); !ok || s == "" {
			return plan{}, false
		}
	}
	return p, true
}

func init() {
	register(
		op{name: "squad.packages.preview", read: true, decode: packageCloudPlan(false),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/squad-packages/preview",
					Body: p.document, Header: asDevice()}
			}},
		op{name: "squad.packages.adopt", decode: packageCloudPlan(true),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/squad-packages/adopt",
					Body: p.document, Header: asDevice()}
			}},
		op{name: "squad.packages.export", read: true, decode: packagePublicExportPlan,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/squad-packages/export",
					Body: p.document, Header: asDevice()}
			}},
		op{name: "squad.packages.export.private", decode: packagePrivateExportPlan,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/squad-packages/export",
					Body: p.document, Header: asDevice()}
			}},
	)
}
