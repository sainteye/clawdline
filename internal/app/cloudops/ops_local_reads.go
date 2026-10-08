package cloudops

import (
	"github.com/sainteye/clawdline/internal/adapters/store"
)

func init() {
	register(
		// MARK: reads with a local capability

		op{name: "transcript", read: true,
			divergence: "`priority` is accepted and dropped: this daemon reads a transcript on " +
				"one lane, so a foreground read is not overtaken by a background one",
			decode: func(b body) (plan, bool) {
				if !b.hasOneOf([]string{"type", "session", "limit"},
					[]string{"type", "session", "limit", "priority"},
					[]string{"type", "session", "limit", "before"},
					[]string{"type", "session", "limit", "priority", "before"},
					[]string{"type", "session", "limit", "expected_generation"},
					[]string{"type", "session", "limit", "priority", "expected_generation"},
					[]string{"type", "session", "limit", "before", "expected_generation"},
					[]string{"type", "session", "limit", "priority", "before", "expected_generation"}) {
					return plan{}, false
				}
				p, ok := sessionPlan(b, "transcript")
				if !ok {
					return plan{}, false
				}
				limit, ok := b.integer("limit")
				if !ok || limit < 1 || limit > 1000 {
					return plan{}, false
				}
				p.limit = limit
				if raw, named := b["expected_generation"]; named {
					value, ok := raw.(string)
					if !ok || !executionGenerationValid(value) {
						return plan{}, false
					}
					p.executionGeneration = value
				}
				if _, named := b["before"]; named {
					before, ok := b.integer("before")
					if !ok || before < 1 {
						return plan{}, false
					}
					p.before = before
					p.name = "transcript.before." + itoa(before)
				}
				p.priority = "foreground"
				if raw, named := b["priority"]; named {
					// A stale hosted tab can only be a person-driven reader:
					// automatic refreshes learned to send `background` in the
					// same release that added this field.
					value, ok := raw.(string)
					if !ok || (value != "foreground" && value != "background") {
						return plan{}, false
					}
					p.priority = value
				}
				return p, true
			},
			route: func(p plan) LocalRequest {
				// This daemon's transcript is a route of its own with the
				// session in the query, not a segment under /v1/sessions.
				query := map[string]string{"session": p.target, "limit": itoa(p.limit)}
				if p.before > 0 {
					query["before"] = itoa(p.before)
				}
				if p.executionGeneration != "" {
					query["expected_generation"] = p.executionGeneration
				}
				return LocalRequest{Method: "GET", Path: "/v1/transcript", Query: query}
			}},

		op{name: "info", read: true,
			divergence: "`parts` travels as a query and this daemon's info route answers the " +
				"same body for both halves, so a summary is a full answer here",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "parts") {
					if !b.has("type", "session", "parts", "expected_generation") {
						return plan{}, false
					}
				}
				parts, ok := b.str("parts")
				if !ok || (parts != "full" && parts != "summary") {
					return plan{}, false
				}
				// The two halves are separate names because they are separate
				// answers: a full request settled by a summary would be cached
				// as complete while missing exactly what the summary omits.
				p, ok := sessionPlan(b, "info."+parts)
				if !ok {
					return plan{}, false
				}
				p.parts = parts
				if raw, named := b["expected_generation"]; named {
					value, ok := raw.(string)
					if !ok || !executionGenerationValid(value) {
						return plan{}, false
					}
					p.executionGeneration = value
				}
				return p, true
			},
			route: func(p plan) LocalRequest {
				req := LocalRequest{Method: "GET", Path: "/v1/sessions/" + segment(p.target) + "/info"}
				if p.parts == "summary" {
					req.Query = map[string]string{"parts": "summary"}
				}
				return req
			}},

		op{name: "git", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session") {
					return plan{}, false
				}
				return sessionPlan(b, "git")
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/sessions/" + segment(p.target) + "/git"}
			}},

		op{name: "git-diff", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "path") {
					return plan{}, false
				}
				p, ok := sessionPlan(b, "")
				if !ok {
					return plan{}, false
				}
				request, ok := requestName(b["request"])
				if !ok {
					return plan{}, false
				}
				path, ok := b.nonEmpty("path")
				if !ok || !printable(path) {
					return plan{}, false
				}
				p.request, p.name, p.path = request, "read:"+request, path
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/sessions/" + segment(p.target) + "/git/diff",
					Query: map[string]string{"path": p.path}}
			}},

		op{name: "screen", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session") {
					return plan{}, false
				}
				return sessionPlan(b, "screen")
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/sessions/" + segment(p.target) + "/screen"}
			}},

		op{name: "image", read: true, shape: shapeImage,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "id") {
					return plan{}, false
				}
				// The id is the opaque one the transcript already published,
				// and it is checked by the store rather than here: this bridge
				// knows what a read looks like, not what an artifact id looks
				// like. A picture's own id is part of its name because a
				// transcript holds many pictures, they are asked for together,
				// and they come back on one channel in whatever order the disk
				// gives them.
				id, ok := b.nonEmpty("id")
				if !ok {
					return plan{}, false
				}
				p, ok := sessionPlan(b, "image."+id)
				if !ok {
					return plan{}, false
				}
				p.id = id
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/artifacts/images/" + segment(p.id)}
			}},

		op{name: "documents", read: true, shape: shapeDocuments,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session") {
					return plan{}, false
				}
				return sessionPlan(b, "documents")
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/sessions/" + segment(p.target) + "/documents"}
			}},

		op{name: "document", read: true, shape: shapeDocument,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "scope", "task", "path") {
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
				return plan{session: id, target: id, request: request, name: "read:" + request,
					scope: scope, task: task, path: path}, true
			},
			route: func(p plan) LocalRequest {
				route := "/v1/sessions/" + segment(p.target) + "/documents/" + p.scope
				if p.scope == "task" {
					route += "/" + segment(p.task)
				}
				return LocalRequest{Method: "GET", Path: route + "/" + escapedDocumentPath(p.path)}
			}},

		op{name: "places", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/places"}
			}},

		// The built-in personas a start or a resume may name (docs/personas.md):
		// machine-wide and parameterless, as the local route is. Only the names,
		// the summaries and the bots cross; the texts stay on the machine.
		op{name: "personas", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/personas"}
			}},

		// The complete squad catalog and private effective settings cross only
		// the paired machine relay. The local route repeats its own read gate.
		op{name: "squad.catalog", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/squad/catalog", Header: asDevice()}
			}},
		op{name: "squad.definition", read: true,
			decode: func(b body) (plan, bool) {
				if !b.hasOneOf([]string{"type", "session", "request", "definition_id"},
					[]string{"type", "session", "request", "definition_id", "version"}) {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				if !ok {
					return plan{}, false
				}
				p.id, ok = b.nonEmpty("definition_id")
				if !ok || len(p.id) > 256 || !printable(p.id) {
					return plan{}, false
				}
				if v, exists := b["version"]; exists {
					p.key, ok = v.(string)
					if !ok || p.key == "" || len(p.key) > 256 || !printable(p.key) {
						return plan{}, false
					}
				}
				return p, true
			},
			route: func(p plan) LocalRequest {
				query := map[string]string{}
				if p.key != "" {
					query["version"] = p.key
				}
				return LocalRequest{Method: "GET", Path: "/v1/squad/definitions/" + segment(p.id),
					Query: query, Header: asDevice()}
			}},
		op{name: "squad.scopes", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/squad/scopes", Header: asDevice()}
			}},
		op{name: "squad.skill-sources", read: true,
			decode: func(b body) (plan, bool) {
				if !b.hasOneOf(
					[]string{"type", "session", "request", "provider"},
					[]string{"type", "session", "request", "provider", "place_id"},
					[]string{"type", "session", "request", "provider", "id", "folder"},
					[]string{"type", "session", "request", "provider", "place_id", "id", "folder"}) {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				if !ok {
					return plan{}, false
				}
				p.assistant, ok = b.nonEmpty("provider")
				if !ok || (p.assistant != "project" && p.assistant != "claude-code" && p.assistant != "codex") {
					return plan{}, false
				}
				if _, has := b["place_id"]; has {
					p.place, ok = b.nonEmpty("place_id")
					if !ok || len(p.place) > 256 || !printable(p.place) {
						return plan{}, false
					}
				}
				if _, has := b["id"]; has {
					p.id, ok = b.nonEmpty("id")
					if !ok || len(p.id) != 64 || !printable(p.id) {
						return plan{}, false
					}
					p.key, ok = b.str("folder")
					if !ok || (p.key != "true" && p.key != "false") {
						return plan{}, false
					}
				}
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/squad/skill-sources",
					Query: someOf(map[string]string{"provider": p.assistant, "place_id": p.place, "id": p.id, "folder": p.key}), Header: asDevice()}
			}},
		op{name: "squad.settings", read: true,
			decode: func(b body) (plan, bool) {
				p, ok := squadCloudScopePlan(b)
				if !ok {
					return plan{}, false
				}
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/squad/settings",
					Query: squadCloudQuery(p), Header: asDevice()}
			}},
		op{name: "squad.settings.update",
			decode: squadCloudWritePlan("changes"),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "PUT", Path: "/v1/squad/settings",
					Body: p.document, Header: asDevice()}
			}},
		op{name: "squad.motion.update",
			decode: squadCloudWritePlan("changes"),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "PUT", Path: "/v1/squad/motion",
					Body: p.document, Header: asDevice()}
			}},
		op{name: "squad.catalog.update",
			decode: squadCloudWritePlan("changes"),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/squad/catalog",
					Body: p.document, Header: asDevice()}
			}},

		op{name: "squad-session-bindings", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/squad/session-bindings", Header: asDevice()}
			}},
		op{name: "squad-session-snapshot", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "conversation") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				if !ok {
					return plan{}, false
				}
				p.id, ok = b.nonEmpty("conversation")
				return p, ok && printable(p.id)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/squad/session-snapshots/" + segment(p.id), Header: asDevice()}
			}},

		op{name: "squad-event-head", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/squad/events/head", Header: asDevice()}
			}},

		op{name: "squad-events", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "after", "limit") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				if !ok {
					return plan{}, false
				}
				after, afterOK := b.integer("after")
				limit, limitOK := b.integer("limit")
				if !afterOK || after < 0 || !limitOK || limit < 1 || limit > store.MaxSquadEventPageRows {
					return plan{}, false
				}
				p.after, p.limit = after, limit
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/squad/events",
					Query: map[string]string{"after": itoa(p.after), "limit": itoa(p.limit)}, Header: asDevice()}
			}},

		op{name: "past-sessions", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "place", "assistant") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				if !ok {
					return plan{}, false
				}
				place, placeOK := b.nonEmpty("place")
				// An empty assistant is the Claude-only spelling of this route
				// and is not a missing field: the key is required, its value
				// may be "". The route below leaves the segment off for it,
				// which is the same request the console makes locally.
				assistant, assistantOK := b.str("assistant")
				if !placeOK || !assistantOK {
					return plan{}, false
				}
				p.place, p.assistant = place, assistant
				return p, true
			},
			route: func(p plan) LocalRequest {
				route := "/v1/places/" + segment(p.place) + "/sessions"
				if p.assistant != "" {
					route += "/" + segment(p.assistant)
				}
				return LocalRequest{Method: "GET", Path: route}
			}},

		op{name: "schedules", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/orchestrator/schedules"}
			}},

		// The Clawdfather coordination panel reads the same broker state on
		// Cloud as it does on this machine. Each read is a fresh machine query;
		// a descriptor snapshot would hide a newly granted lease or safe point.
		op{name: "coordination.leases", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/orchestrator/leases"}
			}},
		op{name: "coordination.waits", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/orchestrator/waits"}
			}},
		op{name: "coordination.pauses", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/orchestrator/pauses"}
			}},

		// push-key is the first request of a registration and the one the
		// whole feature stopped on: a phone that pressed "notify me" got as
		// far as the iOS permission dialog and then asked this for the
		// application server key, which the relay seam refused in the browser.
		//
		// A read, as the Swift bridge classifies it (`CloudV2ReadCatalog`,
		// `.pushKey` is a `liveQuery`): asking for the key is what mints one,
		// and a machine nobody has ever asked to be notified by grows no key
		// material, but nothing about a session changes either way.
		op{name: "push-key", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/push/key"}
			}},

		op{name: "board", read: true,
			divergence: "the envelope and cards are the Swift app's; the report, collection, " +
				"catalog-search and session selectors are refused by name " +
				"(board_selector_not_implemented), and item writes are refused pending " +
				"docs/board-design.md",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "project", "item") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				if !ok {
					return plan{}, false
				}
				project, projectOK := b.str("project")
				item, itemOK := b.str("item")
				if !projectOK || !itemOK || len(project) > 200 || len(item) > 200 {
					return plan{}, false
				}
				p.project, p.item = project, item
				return p, true
			},
			route: func(p plan) LocalRequest {
				query := map[string]string{}
				if p.project != "" {
					query["project"] = p.project
				}
				if p.item != "" {
					query["item"] = p.item
				}
				return LocalRequest{Method: "GET", Path: "/v1/board", Query: query}
			}},

		op{name: "board-command",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "command") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				command, commandOK := b["command"].(map[string]any)
				commandRequest, requestOK := requestName(command["requestId"])
				document, documentOK := b.object("command", defaultModelsCloudBodyLimit)
				if !ok || p.request == "" || !commandOK || !requestOK || commandRequest != p.request || !documentOK {
					return plan{}, false
				}
				p.document = document
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/board",
					Body: p.document, Header: asDevice()}
			}},
	)
}
