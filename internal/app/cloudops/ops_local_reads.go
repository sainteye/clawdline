package cloudops

import ()

func init() {
	register(
		// MARK: reads with a local capability
		op{name: "sessions.list", read: true},

		op{name: "skills", read: true},

		op{name: "transcript", read: true,
			divergence: "`priority` is accepted and dropped: this daemon reads a transcript on " +
				"one lane, so a foreground read is not overtaken by a background one",
		},

		op{name: "info", read: true,
			divergence: "`parts` travels as a query; full and summary share metadata here, " +
				"while remote-permission explicitly inspects the live RC worker",
		},

		op{name: "git", read: true},
		op{name: "outputs", read: true},
		op{name: "output-file", read: true},

		op{name: "git-diff", read: true},

		op{name: "screen", read: true},

		op{name: "image", read: true, shape: shapeImage},

		op{name: "documents", read: true, shape: shapeDocuments},

		op{name: "document", read: true, shape: shapeDocument,
			decode: func(b body) (plan, bool) {
				if !b.hasOneOf([]string{"type", "session", "request", "scope", "task", "path"},
					[]string{"type", "session", "request", "scope", "task", "path", "expected_generation"}) {
					return plan{}, false
				}
				id, ok := b.nonEmpty("session")
				if !ok {
					return plan{}, false
				}
				request, ok := requestName(b["request"])
				if !ok {
					return plan{}, false
				}
				scope, _ := b.str("scope")
				task, taskOK := b.str("task")
				path, pathOK := documentPath(b["path"])
				if !taskOK || !pathOK {
					return plan{}, false
				}
				switch {
				case scope == "project" && task == "":
				case scope == "task" && isTaskID(task):
				default:
					return plan{}, false
				}
				p, valid := pinnedSessionPlan(b, "read:"+request)
				if !valid || p.session != id {
					return plan{}, false
				}
				p.request, p.scope, p.task, p.path = request, scope, task, path
				return p, true
			},
			route: func(p plan) LocalRequest {
				route := "/v1/sessions/" + segment(p.target) + "/documents/" + p.scope
				if p.scope == "task" {
					route += "/" + segment(p.task)
				}
				return LocalRequest{Method: "GET", Path: route + "/" + escapedDocumentPath(p.path)}
			}},

		op{name: "places", read: true},

		// The built-in personas a start or a resume may name (docs/personas.md):
		// machine-wide and parameterless, as the local route is. Only the names,
		// the summaries and the bots cross; the texts stay on the machine.
		op{name: "personas", read: true},

		// The complete squad catalog and private effective settings cross only
		// the paired machine relay. The local route repeats its own read gate.
		op{name: "squad.catalog", read: true},
		op{name: "squad.definition", read: true},
		op{name: "squad.scopes", read: true},
		op{name: "squad.skill-sources", read: true},
		op{name: "squad.settings", read: true},
		op{name: "squad.settings.update"},
		op{name: "squad.motion.update"},
		op{name: "squad.catalog.update"},
		op{name: "squad.role-draft"},

		op{name: "squad-session-bindings", read: true},
		op{name: "squad-session-snapshot", read: true},

		op{name: "squad-event-head", read: true},

		op{name: "squad-events", read: true},

		op{name: "past-sessions", read: true},

		op{name: "schedules", read: true},

		// The Clawdfather coordination panel reads the same broker state on
		// Cloud as it does on this machine. Each read is a fresh machine query;
		// a descriptor snapshot would hide a newly granted lease or safe point.
		op{name: "coordination.leases", read: true},
		op{name: "coordination.waits", read: true},
		op{name: "coordination.pauses", read: true},

		// push-key is the first request of a registration and the one the
		// whole feature stopped on: a phone that pressed "notify me" got as
		// far as the iOS permission dialog and then asked this for the
		// application server key, which the relay seam refused in the browser.
		//
		// A read, as the Swift bridge classifies it (`CloudV2ReadCatalog`,
		// `.pushKey` is a `liveQuery`): asking for the key is what mints one,
		// and a machine nobody has ever asked to be notified by grows no key
		// material, but nothing about a session changes either way.
		op{name: "push-key", read: true},

		op{name: "board", read: true,
			divergence: "the envelope and cards are the Swift app's; the report, collection, " +
				"catalog-search and session selectors are refused by name " +
				"(board_selector_not_implemented), and item writes are refused pending " +
				"docs/board-design.md",
		},

		op{name: "board-command"},
	)
}
