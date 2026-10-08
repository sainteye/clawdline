package cloudops

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
)

func init() {
	register(
		// MARK: the work system, and what a person is looking at while it runs
		//
		// Participation reads remain available while the v1 board and Backlog
		// reads are retired. The v2 Board uses its own Cloud words below.

		op{name: "work.proposals", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "project") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				if !ok {
					return plan{}, false
				}
				project, projectOK := b.str("project")
				if !projectOK || len(project) > 200 {
					return plan{}, false
				}
				p.project = project
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/work/proposals",
					Query: someOf(map[string]string{"project": p.project})}
			}},

		op{name: "work.decisions", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/work/decisions"}
			}},
		op{name: "work.decision", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "id") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				id, valid := b.nonEmpty("id")
				if !ok || !valid || !isTaskID(id) {
					return plan{}, false
				}
				p.id = id
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/work/decisions/" + p.id}
			}},
		op{name: "work.decision-answer",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "id", "answer") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				id, valid := b.nonEmpty("id")
				answer, answerOK := b.nonEmpty("answer")
				if !ok || p.request == "" || !valid || !isTaskID(id) || !answerOK ||
					strings.TrimSpace(answer) == "" || len(answer) > 4096 || !printable(answer) {
					return plan{}, false
				}
				p.id, p.text = id, answer
				return p, true
			},
			route: func(p plan) LocalRequest {
				payload, _ := json.Marshal(map[string]string{"answer": p.text})
				return LocalRequest{Method: "POST", Path: "/v1/work/decisions/" + p.id,
					Body: payload, Header: asDevice()}
			}},

		op{name: "work.digests", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "kind") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				if !ok {
					return plan{}, false
				}
				// Which kinds there are is the route's rule (`invalid_kind`),
				// not this bridge's: a second copy of a closed set is a second
				// thing to keep right, and the route's refusal is the one the
				// page already reads.
				kind, kindOK := b.str("kind")
				if !kindOK || len(kind) > 32 {
					return plan{}, false
				}
				p.kind = kind
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/work/digests",
					Query: someOf(map[string]string{"kind": p.kind})}
			}},

		// Work-system v2 is the console's current Board. These are separate
		// words because a machine descriptor must be able to say exactly which
		// reads and person-only writes it implements. Every write is stamped as
		// a paired device: a Cloud viewer is a person using this machine, not an
		// Agent or the machine's orchestrator.
		op{name: "work.v2.item", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "id") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				id, idOK := b.nonEmpty("id")
				if !ok || !idOK || len(id) > 256 {
					return plan{}, false
				}
				p.id = id
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/work/v2/items/" + segment(p.id)}
			}},
		op{name: "work.v2.gate-export", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "id") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				id, valid := b.nonEmpty("id")
				if !ok || !valid || len(id) > 256 {
					return plan{}, false
				}
				p.id = id
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/work/v2/items/" + segment(p.id) + "/gate-export", Header: asDevice()}
			}},

		op{name: "work.v2.items", read: true,
			decode: func(b body) (plan, bool) {
				legacy := b.has("type", "session", "request", "project")
				if !legacy && !b.has("type", "session", "request", "project", "cursor") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				project, projectOK := b.str("project")
				cursor := ""
				if !legacy {
					var cursorOK bool
					cursor, cursorOK = b.str("cursor")
					if !cursorOK || len(cursor) > 1024 {
						return plan{}, false
					}
				}
				if !ok || !projectOK || len(project) > 256 {
					return plan{}, false
				}
				p.project, p.cursor = project, cursor
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/work/v2/items",
					Query: someOf(map[string]string{"project": p.project, "cursor": p.cursor})}
			}},

		op{name: "work.v2.search", read: true,
			decode: func(b body) (plan, bool) {
				legacy := b.has("type", "session", "request", "project", "status", "query")
				if !legacy && !b.has("type", "session", "request", "project", "status", "query", "cursor") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				project, projectOK := b.str("project")
				status, statusOK := b.str("status")
				query, queryOK := b.str("query")
				cursor := ""
				if !legacy {
					var cursorOK bool
					cursor, cursorOK = b.str("cursor")
					if !cursorOK || len(cursor) > 1024 {
						return plan{}, false
					}
				}
				if !ok || !projectOK || len(project) > 256 || !statusOK ||
					(status != "open" && status != "done" && status != "all") ||
					!queryOK || len(query) > 1024 {
					return plan{}, false
				}
				p.project, p.status, p.query, p.cursor = project, status, query, cursor
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/work/v2/items",
					Query: someOf(map[string]string{"project": p.project, "status": p.status, "q": p.query, "cursor": p.cursor})}
			}},

		op{name: "work.v2.proposals", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "state") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				state, stateOK := b.str("state")
				if !ok || !stateOK || len(state) > 32 {
					return plan{}, false
				}
				p.kind = state
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/work/v2/proposals",
					Query: someOf(map[string]string{"state": p.kind})}
			}},

		op{name: "work.v2.session-todos", read: true,
			decode: func(b body) (plan, bool) {
				if !b.hasOneOf([]string{"type", "session", "request", "terminal"},
					[]string{"type", "session", "request", "terminal", "summary"}) {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				terminal, terminalOK := b.nonEmpty("terminal")
				if !ok || !terminalOK || len(terminal) > 256 {
					return plan{}, false
				}
				p.target = terminal
				if _, exists := b["summary"]; exists {
					summary, valid := b.boolean("summary")
					if !valid {
						return plan{}, false
					}
					if summary {
						p.kind = "1"
					}
				}
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/work/v2/session-todos/" + segment(p.target),
					Query: someOf(map[string]string{"summary": p.kind})}
			}},
		op{name: "work.v2.human-interventions", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "terminal") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				conversation, valid := b.nonEmpty("terminal")
				if !ok || !valid || len(conversation) > 256 {
					return plan{}, false
				}
				p.target = conversation
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/work/v2/human-interventions/" + segment(p.target)}
			}},

		// `size: "thumb"` is the picture a Board card or a to-do row draws,
		// and the only size there is besides the whole one. A page built
		// before it sends no `size` and is answered the full image, as it
		// always was.
		op{name: "work.v2.image", read: true, shape: shapeImage,
			decode: func(b body) (plan, bool) {
				if !b.hasOneOf([]string{"type", "session", "request", "id"},
					[]string{"type", "session", "request", "id", "size"}) {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				id, idOK := b.nonEmpty("id")
				if !ok || !idOK || len(id) > 256 {
					return plan{}, false
				}
				if _, sized := b["size"]; sized {
					if size, _ := b.str("size"); size != "thumb" {
						return plan{}, false
					}
					p.kind = "thumb"
				}
				p.id = id
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/work/v2/images/" + segment(p.id),
					Query: someOf(map[string]string{"size": p.kind})}
			}},

		op{name: "work.v2.create",
			decode: decodeWorkV2Document("item"),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/work/v2/items", Body: p.document, Header: asDevice()}
			}},

		op{name: "work.v2.edit",
			decode: decodeWorkV2NamedDocument("id", "item"),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "PATCH", Path: "/v1/work/v2/items/" + segment(p.id),
					Body: p.document, Header: asDevice()}
			}},
		op{name: "work.v2.gate-decision",
			decode: decodeWorkV2NamedDocument("id", "item"),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/work/v2/items/" + segment(p.id) + "/gate-decision",
					Body: p.document, Header: asDevice()}
			}},
		op{name: "work.v2.gate-purge",
			decode: decodeWorkV2NamedDocument("id", "item"),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/work/v2/items/" + segment(p.id) + "/gate-purge",
					Body: p.document, Header: asDevice()}
			}},

		op{name: "work.v2.assign",
			decode: decodeWorkV2NamedDocument("id", "item"),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/work/v2/items/" + segment(p.id) + "/assign",
					Body: p.document, Header: asDevice()}
			}},
		op{name: "work.v2.convert",
			decode: decodeWorkV2NamedDocument("id", "item"),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/work/v2/items/" + segment(p.id) + "/convert",
					Body: p.document, Header: asDevice()}
			}},

		op{name: "work.v2.persona-suggestion",
			decode: decodeWorkV2NamedDocument("id", "item"),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/work/v2/items/" + segment(p.id) + "/persona-suggestion",
					Body: p.document, Header: asDevice()}
			}},

		op{name: "work.v2.remind",
			decode: decodeWorkV2NamedDocument("id", "item"),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/work/v2/items/" + segment(p.id) + "/remind",
					Body: p.document, Header: asDevice()}
			}},

		op{name: "work.v2.cancel",
			decode: decodeWorkV2NamedDocument("id", "item"),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/work/v2/items/" + segment(p.id) + "/cancel",
					Body: p.document, Header: asDevice()}
			}},

		op{name: "work.v2.complete",
			decode: decodeWorkV2NamedDocument("id", "item"),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/work/v2/items/" + segment(p.id) + "/complete",
					Body: p.document, Header: asDevice()}
			}},

		// The person's read receipt for a deploying or done item; the local
		// route judges `item.phase` and that a person, not an Agent, saw it.
		op{name: "work.v2.seen",
			decode: decodeWorkV2NamedDocument("id", "item"),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/work/v2/items/" + segment(p.id) + "/seen",
					Body: p.document, Header: asDevice()}
			}},

		op{name: "work.v2.image-create",
			decode: decodeWorkV2NamedImageDocument,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/work/v2/items/" + segment(p.id) + "/images",
					Body: p.document, Header: asDevice()}
			}},

		op{name: "work.v2.image-delete",
			decode: decodeWorkV2ImageDelete,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "DELETE", Path: "/v1/work/v2/items/" + segment(p.id) + "/images/" + segment(p.item),
					Body: p.document, Header: asDevice()}
			}},

		op{name: "work.v2.proposal-resolve",
			decode: decodeWorkV2Action("decision", "accept", "reject"),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/work/v2/proposals/" + segment(p.id) + "/" + segment(p.kind),
					Body: p.document, Header: asDevice()}
			}},

		op{name: "work.v2.todo-create",
			decode: decodeWorkV2TerminalDocument,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/work/v2/session-todos/" + segment(p.target),
					Body: p.document, Header: asDevice()}
			}},

		op{name: "work.v2.todo-image-create",
			decode: decodeWorkV2TodoImageDocument,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/work/v2/session-todos/" + segment(p.target) + "/" +
					segment(p.id) + "/images", Body: p.document, Header: asDevice()}
			}},

		op{name: "work.v2.todo-action",
			decode: decodeWorkV2TodoAction,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/work/v2/session-todos/" + segment(p.target) + "/" +
					segment(p.id) + "/" + segment(p.kind), Body: p.document, Header: asDevice()}
			}},
		op{name: "work.v2.human-intervention-action",
			decode: decodeWorkV2HumanInterventionAction,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/work/v2/human-interventions/" + segment(p.target) + "/" +
					segment(p.id) + "/" + segment(p.kind), Body: p.document, Header: asDevice()}
			}},

		// The Projects page's catalog and worktree lifecycle. `places` above is
		// carried separately.
		op{name: "project-hide",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "path") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				path, valid := b.nonEmpty("path")
				if !ok || !valid || !printable(path) || len(path) > 4096 || !filepath.IsAbs(path) {
					return plan{}, false
				}
				p.path = path
				return p, true
			},
			route: func(p plan) LocalRequest {
				payload, _ := json.Marshal(map[string][]string{"paths": {p.path}})
				return LocalRequest{Method: "DELETE", Path: "/v1/places", Body: payload, Header: asDevice()}
			}},
		op{name: "project-icon-copy",
			decode: decodeWorkV2NamedDocument("id", "item"),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "PUT", Path: "/v1/projects/" + segment(p.id) + "/icon", Body: p.document, Header: asDevice()}
			},
		},
		op{name: "projects", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/projects"}
			}},
		op{name: "project-file-list", read: true,
			decode: decodeProjectFileList,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/projects/" + segment(p.project) + "/files"}
			}},
		op{name: "project-file-read", read: true,
			decode: decodeProjectFileRead,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/projects/" + segment(p.project) + "/files/" + segment(p.id)}
			}},
		op{name: "project-tree-list", read: true,
			decode: decodeProjectTreeList,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/projects/" + segment(p.project) + "/tree",
					Query: map[string]string{"directory": p.path}}
			}},
		op{name: "project-tree-read", read: true,
			decode: decodeProjectTreeRead,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/projects/" + segment(p.project) + "/tree/file",
					Query: map[string]string{"path": p.path}}
			}},
		// Unify (docs/project-files.md): the plan is a read a paired device
		// may make with remote writes off; apply is a write and needs the gate.
		op{name: "project-unify-plan", read: true,
			decode: decodeProjectFileList,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/projects/" + segment(p.project) + "/unify"}
			}},
		op{name: "project-unify-apply",
			decode:  decodeProjectUnifyApply,
			partial: stoppedUnify,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/projects/" + segment(p.project) + "/unify",
					Body: p.document, Header: asDevice()}
			}},
		op{name: "project-file-save",
			decode: decodeProjectFileSave,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "PUT", Path: "/v1/projects/" + segment(p.project) + "/files/" + segment(p.id),
					Body: p.document, Header: asDevice()}
			}},

		// Project settings sync (docs/project-sync.md): a source offers, a
		// mirror applies. The viewer is the courier; neither machine reaches
		// the other, and the relay carries only ciphertext.
		op{name: "project-manifest", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/project-sync/manifest"}
			}},
		op{name: "project-entry", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "repo") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				repo, repoOK := b.nonEmpty("repo")
				if !ok || !repoOK || len(repo) > 512 {
					return plan{}, false
				}
				p.id = repo
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/project-sync/entry", Query: map[string]string{"repo": p.id}}
			}},
		op{name: "project-mirror", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/project-sync/mirror"}
			}},
		op{name: "project-mirror-apply",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "item") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				document, documentOK := b.object("item", projectSyncCloudBodyLimit)
				if !ok || p.request == "" || !documentOK {
					return plan{}, false
				}
				p.document = document
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/project-sync/mirror", Body: p.document, Header: asDevice()}
			}},
		op{name: "project-mirror-detach",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "repo") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				repo, repoOK := b.nonEmpty("repo")
				if !ok || p.request == "" || !repoOK || len(repo) > 512 {
					return plan{}, false
				}
				p.id = repo
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "DELETE", Path: "/v1/project-sync/mirror", Query: map[string]string{"repo": p.id}, Header: asDevice()}
			}},

		op{name: "project-worktree-lifecycle", read: true,
			decode: decodeProject,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET",
					Path: "/v1/projects/" + segment(p.project) + "/worktrees"}
			}},

		op{name: "project-worktree-lifecycle-refresh",
			decode: decodeProjectRefresh,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST",
					Path: "/v1/projects/" + segment(p.project) + "/worktrees/refresh",
					Body: []byte("{}")}
			}},

		op{name: "timeline", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "project", "entry", "cursor",
					"environment", "category", "upcoming") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				if !ok {
					return plan{}, false
				}
				project, projectOK := b.str("project")
				entry, entryOK := b.str("entry")
				cursor, cursorOK := b.str("cursor")
				environment, environmentOK := b.str("environment")
				category, categoryOK := b.str("category")
				upcoming, upcomingOK := b.boolean("upcoming")
				if !projectOK || !entryOK || !cursorOK || !environmentOK || !categoryOK || !upcomingOK ||
					len(project) > 200 || len(entry) > 200 || len(cursor) > 20 ||
					len(environment) > 32 || len(category) > 32 {
					return plan{}, false
				}
				p.project, p.entry, p.cursor = project, entry, cursor
				p.environment, p.category, p.upcoming = environment, category, upcoming
				return p, true
			},
			// `upcoming` is sent every time and both values mean something —
			// the route's default is on and the page's filter turns it off —
			// so it is not in `someOf`. The rest are left out when empty,
			// because this route reads an absent parameter and an empty one
			// differently (`environment` defaults to production; an empty one
			// would be refused `bad_environment`).
			route: func(p plan) LocalRequest {
				query := someOf(map[string]string{
					"project": p.project, "entry": p.entry, "cursor": p.cursor,
					"environment": p.environment, "category": p.category,
				})
				query["upcoming"] = "false"
				if p.upcoming {
					query["upcoming"] = "true"
				}
				return LocalRequest{Method: "GET", Path: "/v1/timeline", Query: query}
			}},

		// The capacity block on the Settings page (docs/limits.md §4.5 "the
		// screen"): every row of the register and the dead letters. A capacity
		// push names that block, and the person reads it on the phone that got
		// the push, so the one route crosses whole. Machine-wide and without
		// a parameter, as the local route is; a read, since the route writes
		// nothing.
		op{name: "capacity", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/capacity"}
			}},

		// The machine-wide launch defaults shown on Settings. This is deliberately
		// narrower than /v1/settings: a paired phone receives and changes only
		// these values, never the machine's other configuration.
		op{name: "default-models", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/settings/default-models"}
			}},

		op{name: "default-models-update",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "changes") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				raw, ok := b["changes"].(map[string]any)
				if !ok || len(raw) > 3 {
					return plan{}, false
				}
				for key, value := range raw {
					if key != "codex_default_model" && key != "claude_default_model" && key != "codex_default_effort" {
						return plan{}, false
					}
					if value != nil {
						if _, ok := value.(string); !ok {
							return plan{}, false
						}
					}
				}
				document, err := json.Marshal(raw)
				if err != nil || len(document) > defaultModelsCloudBodyLimit {
					return plan{}, false
				}
				p.document = document
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/settings/default-models",
					Body: p.document, Header: asDevice()}
			}},

		// The two Work v2 gate defaults shown by every Settings page. Like the
		// default-model route above, this is deliberately narrower than
		// /v1/settings: a paired device can neither read nor write another
		// machine setting through this word.
		op{name: "work-gate-settings", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/settings/work-gates"}
			}},

		op{name: "work-gate-settings-update",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "changes") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				raw, ok := b["changes"].(map[string]any)
				if !ok || len(raw) > 2 {
					return plan{}, false
				}
				for key, value := range raw {
					if key != "planning_gate" && key != "verify_gate" {
						return plan{}, false
					}
					if value != nil {
						if _, ok := value.(bool); !ok {
							return plan{}, false
						}
					}
				}
				document, err := json.Marshal(raw)
				if err != nil || len(document) > defaultModelsCloudBodyLimit {
					return plan{}, false
				}
				p.document = document
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/settings/work-gates",
					Body: p.document, Header: asDevice()}
			}},

		// The dashboard behind the session counts: this machine's CPU and
		// memory and each session's share. Machine-wide and parameterless, as
		// the local route is; a read, since the route writes nothing.
		op{name: "machine-usage", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/machine/usage"}
			}},

		// Whether this machine trails the cloud's latest build
		// (docs/updates.md): machine-wide and parameterless, as the local
		// route is; a read, since the route answers from the last background
		// check and writes nothing.
		op{name: "update", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/update"}
			}},

		// The Settings page's 「立即更新」 (docs/updates.md): install the newest
		// release of this machine's channel. A command, so it needs the
		// machine's Cloud-command switch like any other write; nothing but the
		// request crosses, so a phone can neither name a version nor force one.
		op{name: "update-apply",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/update/apply",
					Body: []byte("{}"), Header: asDevice()}
			}},

		op{name: "agent", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "agent", "limit") {
					return plan{}, false
				}
				agent, ok := b.nonEmpty("agent")
				if !ok {
					return plan{}, false
				}
				limit, ok := b.integer("limit")
				if !ok || limit < 1 || limit > 1000 {
					return plan{}, false
				}
				// The id travels into the answer's name: a session has many
				// agents and one answer channel between them, so a reader who
				// opens two would have the first settled by the second's
				// conversation.
				p, ok := sessionPlan(b, "agent:"+agent)
				if !ok {
					return plan{}, false
				}
				p.id, p.limit = agent, limit
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/sessions/" + segment(p.session) + "/agents/" + segment(p.id),
					Query: map[string]string{"limit": strconv.FormatInt(p.limit, 10)}}
			}},

		op{name: "shell", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "shell", "bytes") {
					return plan{}, false
				}
				shell, ok := b.nonEmpty("shell")
				if !ok {
					return plan{}, false
				}
				// Bytes and not entries: a command has no turns, so the only
				// honest bound on its output is how much of the tail to take.
				window, ok := b.integer("bytes")
				if !ok || window < 1<<10 || window > 1<<20 {
					return plan{}, false
				}
				p, ok := sessionPlan(b, "shell:"+shell)
				if !ok {
					return plan{}, false
				}
				p.id, p.byteWindow = shell, window
				return p, true
			},
			// The Shell panel's read (internal/transport/http/shells.go). The
			// window is the local route's `sessions.shell_output_bytes`, so
			// the two bounds are one.
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/sessions/" + segment(p.session) + "/shells/" + segment(p.id),
					Query: map[string]string{"bytes": strconv.FormatInt(p.byteWindow, 10)}}
			}},
	)
}
