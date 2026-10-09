package cloudops

import (
	"strconv"
)

func init() {
	register(
		// MARK: reads this daemon has no local capability for

		// The rows again, for a page that has just (re)connected.
		//
		// The relay replays each channel's last envelope from memory, that
		// memory dies with the account's object, and this machine re-sends an
		// unchanged row only on its heartbeat — so a page opened after an
		// eviction sees whichever rows happened to change, and the list is
		// short until the heartbeats come round. The copied client asks this
		// word of every machine that lists it in `features`, once per
		// connection that did not take over a live socket (`_recoverSessions`
		// in `net/cloud-client.js`); `orchestrator` is present, and true, only
		// when that page holds no `orch/` snapshot of this machine. The answer
		// is `{"sessions":[ids],"complete":bool}` after the rows went out.
		op{name: sessionsSnapshotWord, read: true, sessions: true,
			decode: func(b body) (plan, bool) {
				if !b.hasOneOf([]string{"type", "session", "request"},
					[]string{"type", "session", "request", "orchestrator"},
					[]string{"type", "session", "request", "initial"},
					[]string{"type", "session", "request", "orchestrator", "initial"}) {
					return plan{}, false
				}
				if value, present := b["orchestrator"]; present {
					if asked, ok := value.(bool); !ok || !asked {
						return plan{}, false
					}
				}
				if value, present := b["initial"]; present {
					if _, ok := value.(bool); !ok {
						return plan{}, false
					}
				}
				return machinePlan(b)
			}},

		// A first recovery is a separate word so a console deployed before its
		// daemon never adds a field to sessions.snapshot that the older exact-key
		// decoder must refuse. Only a machine advertising this word is asked it.
		op{name: sessionsSnapshotInitialWord, read: true, sessions: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			}},

		op{name: "board.items", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "project", "audience", "cursor", "limit") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				if !ok {
					return plan{}, false
				}
				project, projectOK := b.nonEmpty("project")
				audience, audienceOK := b.str("audience")
				cursor, cursorOK := b.integer("cursor")
				limit, limitOK := b.integer("limit")
				if !projectOK || len(project) > 200 || !audienceOK || !cursorOK || !limitOK ||
					cursor < 0 || cursor > 100_000 || limit < 1 || limit > 200 {
					return plan{}, false
				}
				switch audience {
				case "human", "agent", "archive", "all":
				default:
					return plan{}, false
				}
				p.project, p.audience = project, audience
				p.offset, p.limit = cursor, limit
				return p, true
			},
			// The Swift app sends all four, always (`CloudLocalRoute.swift:123-128`):
			// board and board.items share one path and differ only by query shape.
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/board", Query: map[string]string{
					"project": p.project, "audience": p.audience,
					"cursor": strconv.FormatInt(p.offset, 10), "limit": strconv.FormatInt(p.limit, 10),
				}}
			}},

		// The token bill (docs/token-ledger.md, "What a person and a session
		// see"): one conversation, one child task, one Board item. Each is the
		// local route's own question with its id in the path, so each takes
		// exactly the ids that route takes and nothing else. The answer is the
		// route's, whole — a `reason` of `not_yet_read` or `transcript_missing`
		// rides inside a 200, and an unknown id is the route's own 404.
		usageRead("usage.session", "sessions"),
		usageRead("usage.task", "tasks"),
		usageRead("usage.item", "items"),

		// Whether compacting early paid (docs/token-ledger.md "Did compacting
		// early pay"): the local route's own question, with its one query
		// field, `since`, taken exactly as the route takes it and nothing else.
		op{name: "usage.compare-compaction", read: true,
			decode: func(b body) (plan, bool) {
				if !b.hasOneOf([]string{"type", "session", "request"}, []string{"type", "session", "request", "since"}) {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				if !ok {
					return plan{}, false
				}
				if _, named := b["since"]; named {
					since, ok := b.str("since")
					if !ok || !compareSince.MatchString(since) {
						return plan{}, false
					}
					p.query = since
				}
				return p, true
			},
			route: func(p plan) LocalRequest {
				out := LocalRequest{Method: "GET", Path: "/v1/usage/compare-compaction"}
				if p.query != "" {
					out.Query = map[string]string{"since": p.query}
				}
				return out
			}},

		// Whether handing over paid (docs/token-ledger.md "Did handing over
		// pay"): the same one query field, `since`, as the comparison above.
		op{name: "usage.compare-handoff", read: true,
			decode: func(b body) (plan, bool) {
				if !b.hasOneOf([]string{"type", "session", "request"}, []string{"type", "session", "request", "since"}) {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				if !ok {
					return plan{}, false
				}
				if _, named := b["since"]; named {
					since, ok := b.str("since")
					if !ok || !compareSince.MatchString(since) {
						return plan{}, false
					}
					p.query = since
				}
				return p, true
			},
			route: func(p plan) LocalRequest {
				out := LocalRequest{Method: "GET", Path: "/v1/usage/compare-handoff"}
				if p.query != "" {
					out.Query = map[string]string{"since": p.query}
				}
				return out
			}},

		// What each unit of work added (docs/token-ledger.md "One unit of
		// work"): the same one query field, `since`, as the comparison.
		op{name: "usage.work-units", read: true,
			decode: func(b body) (plan, bool) {
				if !b.hasOneOf([]string{"type", "session", "request"}, []string{"type", "session", "request", "since"}) {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				if !ok {
					return plan{}, false
				}
				if _, named := b["since"]; named {
					since, ok := b.str("since")
					if !ok || !compareSince.MatchString(since) {
						return plan{}, false
					}
					p.query = since
				}
				return p, true
			},
			route: func(p plan) LocalRequest {
				out := LocalRequest{Method: "GET", Path: "/v1/usage/work-units"}
				if p.query != "" {
					out.Query = map[string]string{"since": p.query}
				}
				return out
			}},

		// The raw samples a before/after report is folded from
		// (docs/token-ledger.md "Did a change make one unit of work
		// cheaper"): the local route's own question, with its two query
		// fields, `since` as the comparison spells it and `until` a Unix
		// time, taken exactly as the route takes them and nothing else.
		op{name: "usage.work-samples", read: true,
			decode: func(b body) (plan, bool) {
				if !b.hasOneOf([]string{"type", "session", "request"}, []string{"type", "session", "request", "since"},
					[]string{"type", "session", "request", "until"}, []string{"type", "session", "request", "since", "until"}) {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				if !ok {
					return plan{}, false
				}
				if _, named := b["since"]; named {
					since, ok := b.str("since")
					if !ok || !compareSince.MatchString(since) {
						return plan{}, false
					}
					p.query = since
				}
				if _, named := b["until"]; named {
					until, ok := b.str("until")
					if !ok || !workUntil.MatchString(until) {
						return plan{}, false
					}
					p.until = until
				}
				return p, true
			},
			route: func(p plan) LocalRequest {
				out := LocalRequest{Method: "GET", Path: "/v1/usage/work-samples"}
				if p.query != "" || p.until != "" {
					out.Query = map[string]string{}
				}
				if p.query != "" {
					out.Query["since"] = p.query
				}
				if p.until != "" {
					out.Query["until"] = p.until
				}
				return out
			}},

		// Things waiting to be verified (docs/verifications.md). The phone is
		// where the person reads them, so every route crosses: two machine
		// reads, and five commands that carry `X-Clawdline-Actor: device` for
		// the reason the schedule writes do — a press on a phone is the person,
		// and a note it writes is signed as the person, not as this machine's
		// own hand. Each sub-document goes to the route whole; the route owns
		// its shape and refuses a field it does not read by name.
		op{name: "verification.list", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/verifications"}
			}},

		op{name: "verification.get", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "id") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				id, idOK := b.str("id")
				if !ok || !idOK || !verificationID.MatchString(id) {
					return plan{}, false
				}
				p.id = id
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/verifications/" + segment(p.id)}
			}},

		op{name: "verification.create",
			decode: decodeVerificationWrite(false),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/verifications", Body: p.document, Header: asDevice()}
			}},

		op{name: "verification.note",
			decode: decodeVerificationWrite(true),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/verifications/" + segment(p.id) + "/notes",
					Body: p.document, Header: asDevice()}
			}},

		op{name: "verification.close",
			decode: decodeVerificationWrite(true),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/verifications/" + segment(p.id) + "/close",
					Body: p.document, Header: asDevice()}
			}},

		op{name: "verification.criterion",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "id", "index", "verification") {
					return plan{}, false
				}
				p, ok := decodeVerificationWrite(true)(body{"type": b["type"], "session": b["session"],
					"request": b["request"], "id": b["id"], "verification": b["verification"]})
				index, indexOK := b.integer("index")
				if !ok || !indexOK || index < 0 || index >= verificationCriteriaMaximum {
					return plan{}, false
				}
				p.offset = index
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/verifications/" + segment(p.id) + "/criteria/" +
					strconv.FormatInt(p.offset, 10), Body: p.document, Header: asDevice()}
			}},

		// One record, by its id, and never more: the wire has no word that
		// deletes a list. `force` is the person saying an open one may go.
		op{name: "verification.delete",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "id", "force") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				id, idOK := b.str("id")
				force, forceOK := b.boolean("force")
				if !ok || p.request == "" || !idOK || !verificationID.MatchString(id) || !forceOK {
					return plan{}, false
				}
				p.id, p.acceptLoss = id, force
				return p, true
			},
			route: func(p plan) LocalRequest {
				out := LocalRequest{Method: "DELETE", Path: "/v1/verifications/" + segment(p.id), Header: asDevice()}
				if p.acceptLoss {
					out.Query = map[string]string{"force": "1"}
				}
				return out
			}},

		// The sentences somebody wrote once, read from a phone.
		//
		// **The whole machine's list, and no session in the question.** The
		// producer sends `{type, session, request}` and nothing else
		// (`net/cloud-client.js`, `_freshSnippets`), because the rows ride the
		// `orch/<machine>` inventory rather than answering one session — so the
		// wire has nowhere to put a session and this read must not invent one.
		op{name: "snippets", read: true,
			divergence: "the whole machine's list, unfiltered: the local route takes `?session=` " +
				"and answers that session's two groups plus the project this machine resolved " +
				"for it — registry prefix first, then the checkout an isolated worktree was cut " +
				"from — and this wire carries no session, so the viewer groups the rows by " +
				"matching `project` against the session's own `cwd` exactly " +
				"(`view/snippets-data.js`, `snippetGroups`). A session standing in a " +
				"subdirectory of its project, or in a worktree, therefore sees its global " +
				"snippets and an empty project group where the same session on this machine's " +
				"own network sees both",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/snippets"}
			}},

		op{name: "schedule", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "id") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				if !ok {
					return plan{}, false
				}
				id, ok := b.nonEmpty("id")
				if !ok {
					return plan{}, false
				}
				p.id = id
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/orchestrator/schedules/" + segment(p.id)}
			}},
	)
}
