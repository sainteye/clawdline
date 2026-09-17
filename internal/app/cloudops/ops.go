package cloudops

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
)

// body is one decoded request. It is a map rather than a struct per word
// because the check that matters is the *key set*: every operation compares
// the body's keys for exact equality, so an added field makes an older machine
// refuse a newer console's request and a newer machine refuse an older
// console's. That is the mechanism this wire has instead of a version number —
// a new field is a new word — and it only works if the keys are looked at.
type body map[string]any

// decodeBody parses the plaintext with numbers kept as written, because
// `limit`, `cursor`, `bytes` and `rate` are integers with ranges, and a float64
// round-trip would quietly accept `1e3` where the wire says an integer.
func decodeBody(plaintext []byte) (body, error) {
	dec := json.NewDecoder(bytes.NewReader(plaintext))
	dec.UseNumber()
	var out body
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errTrailing
	}
	return out, nil
}

type decodeError string

func (e decodeError) Error() string { return string(e) }

const errTrailing = decodeError("more than one JSON value")

func (b body) has(want ...string) bool {
	if len(b) != len(want) {
		return false
	}
	for _, key := range want {
		if _, ok := b[key]; !ok {
			return false
		}
	}
	return true
}

// hasOneOf is the two shapes a page already open on a previous hosted build
// may send: the older one without `request`, the newer one with it. Accepting
// both is deliberate — reloading a page must improve delivery semantics, not
// become a prerequisite for sending at all.
func (b body) hasOneOf(sets ...[]string) bool {
	for _, set := range sets {
		if b.has(set...) {
			return true
		}
	}
	return false
}

func (b body) str(key string) (string, bool) {
	v, ok := b[key].(string)
	return v, ok
}

func (b body) nonEmpty(key string) (string, bool) {
	v, ok := b.str(key)
	return v, ok && v != ""
}

func (b body) integer(key string) (int64, bool) {
	n, ok := b[key].(json.Number)
	if !ok {
		return 0, false
	}
	v, err := n.Int64()
	return v, err == nil
}

func (b body) boolean(key string) (bool, bool) {
	v, ok := b[key].(bool)
	return v, ok
}

// object keeps a sub-document as the bytes it will travel as, because the
// words that carry one — a board command, a schedule, a snippet, a diagnostic
// report — hand it straight to a route that owns its shape. This bridge knows
// what a request looks like, not what a schedule looks like.
func (b body) object(key string, limit int) ([]byte, bool) {
	v, ok := b[key].(map[string]any)
	if !ok {
		return nil, false
	}
	out, err := json.Marshal(v)
	if err != nil || len(out) > limit {
		return nil, false
	}
	return out, true
}

// requestName is the opaque correlation id a viewer names its answer channel
// with. Printable, bounded, and never interpreted here: it is the browser's
// word and this machine only quotes it back.
func requestName(value any) (string, bool) {
	name, ok := value.(string)
	if !ok || name == "" || len(name) > 128 {
		return "", false
	}
	return name, printable(name)
}

func sessionName(value any) (string, bool) {
	name, ok := value.(string)
	if !ok || name == "" || len(name) > 256 {
		return "", false
	}
	return name, printable(name)
}

func printable(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// documentPath is a cheap copy of the local route's lexical boundary, before
// any filesystem is touched. internal/adapters/documents applies the same
// checks again against its resolved root; this one exists to keep a malformed
// relay read from reaching that router at all.
func documentPath(value any) (string, bool) {
	path, ok := value.(string)
	if !ok || path == "" || len(path) > 512 || strings.HasPrefix(path, "/") ||
		strings.ContainsRune(path, 0) || !printable(path) {
		return "", false
	}
	parts := strings.Split(path, "/")
	if len(parts) == 0 || len(parts) > documentsMaximumDepth {
		return "", false
	}
	for _, part := range parts {
		if part == "" || strings.HasPrefix(part, ".") {
			return "", false
		}
	}
	switch strings.ToLower(extensionOf(parts[len(parts)-1])) {
	case "md", "markdown", "txt":
		return path, true
	}
	return "", false
}

// documentsMaximumDepth is internal/adapters/documents' MaximumDepth.
const documentsMaximumDepth = 6

func extensionOf(name string) string {
	dot := strings.LastIndexByte(name, '.')
	if dot < 0 {
		return ""
	}
	return name[dot+1:]
}

// isTaskID is the broker's own id shape: a lowercase UUID.
func isTaskID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
		}
	}
	return true
}

// plan is one decoded request: where its answer goes, and every field the
// route builder needs. One struct rather than a type per word, because the
// fields are the wire's own names and a reader comparing this file with
// CloudAppBridge.swift should not have to learn twenty small types first.
type plan struct {
	// session is the channel the answer is published on, and name is the
	// payload's `read`. Both empty means this request named no safe answer
	// channel, and a refusal for it is a notice.
	session string
	name    string
	// request is the viewer's correlation id, when it named one.
	request string
	// target is the session a session-scoped operation acts on, which is not
	// the same field as session: a machine-scoped request's session is the
	// machine reply channel.
	target string

	id, scope, task, path             string
	place, past, assistant, model     string
	parts, priority, text, key, audio string
	project, item, audience, entry    string
	environment, category, cursor     string
	images                            []string
	upcoming, acceptLoss              bool
	closeability                      string
	rate, limit, byteWindow, offset   int64
	document                          []byte
}

// op is one word of the vocabulary.
type op struct {
	name string
	// read is whether this word is effect-free. Reads and commands are gated
	// differently, and that difference is the reason they are separate lists.
	read bool
	// readLevel marks a command a paired device may send with remote writes
	// switched off: registering for notifications, handing over a diagnostic
	// report. Neither may inherit the separate switch for typing into a
	// session.
	readLevel bool
	// decode turns a well-formed body into a plan, or refuses it.
	decode func(b body) (plan, bool)
	// route names this daemon's own route. Nil is the honest answer for a word
	// this daemon has no local capability for.
	route func(p plan) LocalRequest
	// refusal is a word this machine deliberately will not serve over Cloud
	// even though it can serve it locally.
	refusal *Refusal
	// anyClass is the one word whose envelope class is not `ctl`: a dispatch
	// is a class of its own, which the relay bills separately.
	anyClass bool
	// shape turns an answer that is not JSON — a picture, a document — into
	// something an envelope can carry.
	shape func(p plan, res LocalResponse) (json.RawMessage, Refusal)
	// divergence is how this daemon's answer differs from the one the hosted
	// console was written against, for a word that is routed anyway. It is a
	// sentence and not a flag because the differences are not alike, and it is
	// here rather than in a document because whoever decides what to advertise
	// needs it at that moment (see Divergences).
	divergence string
}

var catalog = map[string]op{}

func register(ops ...op) {
	for _, o := range ops {
		catalog[o.name] = o
	}
}

// divergences is every routed word whose answer is not what the hosted
// console expects, and how. Read it before advertising a word: a machine that
// lists a word it answers differently is worse for the person than one that
// does not list it, because the second draws nothing and the first draws
// something wrong.
func divergences() map[string]string {
	out := map[string]string{}
	for name, o := range catalog {
		if o.route != nil && o.divergence != "" {
			out[name] = o.divergence
		}
	}
	return out
}

func opNames(keep func(op) bool) []string {
	out := []string{}
	for name, o := range catalog {
		if keep(o) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// sessionPlan is the answer channel for a word that acts on one session: the
// session's own transcript channel, under the name the console waits on.
func sessionPlan(b body, name string) (plan, bool) {
	id, ok := b.nonEmpty("session")
	if !ok {
		return plan{}, false
	}
	return plan{session: id, target: id, name: name}, true
}

// machinePlan is the answer channel for a word about the machine rather than a
// session. Every machine request is answered on the one machine reply channel,
// so neither a malformed nor an unknown body can name a session.
func machinePlan(b body) (plan, bool) {
	id, ok := b.str("session")
	if !ok || id != MachineReplySession {
		return plan{}, false
	}
	request, ok := requestName(b["request"])
	if !ok {
		return plan{}, false
	}
	return plan{session: id, request: request, name: "read:" + request}, true
}

// actionPlan is a command's answer channel: `action:<request>`, on the
// session's channel for a session command and on the machine's for the rest.
// A body with no `request` names no channel, which is not a failure — an older
// page sends none and is answered by the effect rather than by a payload.
func actionPlan(b body, sessionScoped bool) (plan, bool) {
	id, ok := b.nonEmpty("session")
	if !ok {
		return plan{}, false
	}
	if !sessionScoped && id != MachineReplySession {
		return plan{}, false
	}
	p := plan{session: id}
	if sessionScoped {
		p.target = id
	}
	raw, named := b["request"]
	if !named {
		// No channel, and nothing is wrong with that.
		return plan{target: p.target}, true
	}
	request, ok := requestName(raw)
	if !ok {
		return plan{}, false
	}
	p.request = request
	p.name = "action:" + request
	return p, true
}

// refusalReply names the answer channel for a refusal decided before the body
// was decoded — only from a closed command vocabulary and the same lexical
// identity fields a decoder would consume, so a malformed body cannot turn
// into an arbitrary publish on somebody's transcript.
func refusalReply(b body, word string, class Class) (string, string, bool) {
	if b == nil || word == "" {
		return "", "", false
	}
	if word == "dispatch" {
		// A dispatch names no session; with a request it is answered where
		// every machine request is.
		request, ok := requestName(b["request"])
		if !ok {
			return "", "", false
		}
		if raw, named := b["session"]; named {
			if id, ok := raw.(string); !ok || id != MachineReplySession {
				return "", "", false
			}
		}
		return MachineReplySession, "action:" + request, true
	}
	if class != ClassCtl {
		return "", "", false
	}
	request, hasRequest := requestName(b["request"])
	session, hasSession := sessionName(b["session"])
	if !hasSession {
		return "", "", false
	}
	o, known := catalog[word]
	// A read without a request — a transcript, an agent, a picture — is
	// announced as a notice rather than published: a malformed body cannot be
	// trusted to name the waiter it would settle. A read that names one is
	// answered, and `document` is the one such read that names a session.
	if known && o.read {
		if !hasRequest {
			return "", "", false
		}
		if word != "document" && session != MachineReplySession {
			return "", "", false
		}
		return session, "read:" + request, true
	}
	if !hasRequest {
		return "", "", false
	}
	switch word {
	case "send", "answer", "key", "end", "focus", "shell-kill":
		return session, "action:" + request, true
	}
	// Every machine command, and any word this Mac does not know, is answered
	// only on the one machine reply channel.
	if session != MachineReplySession {
		return "", "", false
	}
	return session, "action:" + request, true
}

func segment(value string) string { return ChannelSegment(value) }

// escapedDocumentPath percent-encodes each segment and keeps the separators,
// which is what the documents route splits on before it decodes.
func escapedDocumentPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		parts[i] = segment(part)
	}
	return strings.Join(parts, "/")
}

func jsonBody(value map[string]any) []byte {
	out, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	return out
}

// cloudDispatchUnpinned is the Swift bridge's own refusal, kept word for word.
//
// The hosted console sends `{task}` and nothing else. The local broker protocol
// wants a materialized task.json plus a task id and a secret, and no pinned
// wire shape says how those are carried or where that file is authorized to be
// written. This daemon has a dispatch route and still refuses, because
// inventing the missing half here would put a file somewhere nobody agreed on.
var cloudDispatchUnpinned = Refusal{Status: 409, Code: "cloud_dispatch_unpinned",
	Message: "Cloud dispatch has no pinned wire shape on this Mac."}

func init() {
	register(
		// MARK: reads with a local capability

		op{name: "transcript", read: true,
			divergence: "`priority` is accepted and dropped: this daemon reads a transcript on " +
				"one lane, so a foreground read is not overtaken by a background one",
			decode: func(b body) (plan, bool) {
				if !b.hasOneOf([]string{"type", "session", "limit"},
					[]string{"type", "session", "limit", "priority"}) {
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
				return LocalRequest{Method: "GET", Path: "/v1/transcript",
					Query: map[string]string{"session": p.target, "limit": itoa(p.limit)}}
			}},

		op{name: "info", read: true,
			divergence: "`parts` travels as a query and this daemon's info route answers the " +
				"same body for both halves, so a summary is a full answer here",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "parts") {
					return plan{}, false
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

		op{name: "board", read: true,
			divergence: "this daemon's /v1/board is a snapshot of the projects derived from " +
				"what is running on this machine; the hosted console was written against the " +
				"Swift app's board envelope, with items, a revision and an audience",
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

		// MARK: reads this daemon has no local capability for

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
			}},

		op{name: "skills", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session") {
					return plan{}, false
				}
				return sessionPlan(b, "skills")
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
			}},

		op{name: "snippets", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
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
			}},

		// MARK: commands with a local capability

		op{name: "send",
			decode: func(b body) (plan, bool) {
				if !b.hasOneOf([]string{"type", "session", "text", "images"},
					[]string{"type", "session", "request", "text", "images"}) {
					return plan{}, false
				}
				p, ok := actionPlan(b, true)
				if !ok {
					return plan{}, false
				}
				text, textOK := b.str("text")
				raw, imagesOK := b["images"].([]any)
				if !textOK || !imagesOK {
					return plan{}, false
				}
				images := make([]string, 0, len(raw))
				for _, item := range raw {
					url, ok := item.(string)
					if !ok {
						return plan{}, false
					}
					images = append(images, url)
				}
				p.text, p.images = text, images
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/sessions/" + segment(p.target) + "/send",
					Body: jsonBody(map[string]any{"text": p.text, "images": p.images})}
			}},

		op{name: "answer", decode: decodeAnswer("answer"), route: routeAnswer},
		// The Swift bridge takes `answer` and `key` as one case, and the
		// hosted console still sends either depending on how old the tab is.
		op{name: "key", decode: decodeAnswer("key"), route: routeAnswer},

		op{name: "end",
			divergence: "`expected_closeability_version` is carried and this daemon's close " +
				"route does not compare it, so a viewer's compare-and-swap against a stale " +
				"reading is not the guard it is on the Swift app; the route's own obligation " +
				"check still runs",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "accept_loss",
					"expected_closeability_version") {
					return plan{}, false
				}
				p, ok := actionPlan(b, true)
				if !ok || p.request == "" {
					return plan{}, false
				}
				acceptLoss, acceptOK := b.boolean("accept_loss")
				closeability, closeOK := b.str("expected_closeability_version")
				if !acceptOK || !closeOK {
					return plan{}, false
				}
				p.acceptLoss, p.closeability = acceptLoss, closeability
				return p, true
			},
			route: func(p plan) LocalRequest {
				// This daemon spells it `close`. `expected_closeability_version`
				// is carried so the wire stays whole, and the route ignores it
				// today — see docs/cloud-wire.md §10.5.
				out := map[string]any{"force": p.acceptLoss}
				if p.closeability != "" {
					out["expected_closeability_version"] = p.closeability
				}
				return LocalRequest{Method: "POST",
					Path: "/v1/sessions/" + segment(p.target) + "/close", Body: jsonBody(out)}
			}},

		op{name: "focus",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				p, ok := actionPlan(b, true)
				if !ok || p.request == "" {
					return plan{}, false
				}
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/sessions/" + segment(p.target) + "/focus"}
			}},

		op{name: "start",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "place", "assistant", "model") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				place, placeOK := b.nonEmpty("place")
				assistant, assistantOK := b.str("assistant")
				model, modelOK := b.str("model")
				if !placeOK || !assistantOK || !modelOK {
					return plan{}, false
				}
				p.place, p.assistant, p.model = place, assistant, model
				return p, true
			},
			route: func(p plan) LocalRequest {
				route := "/v1/places/" + segment(p.place) + "/start"
				assistant := p.assistant
				if assistant == "" {
					assistant = "claude"
				}
				if p.assistant != "" || p.model != "" {
					route += "/" + segment(assistant)
				}
				if p.model != "" {
					route += "/" + segment(p.model)
				}
				return LocalRequest{Method: "POST", Path: route, Body: []byte("{}")}
			}},

		op{name: "resume",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "place", "past", "assistant") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				place, placeOK := b.nonEmpty("place")
				past, pastOK := b.nonEmpty("past")
				assistant, assistantOK := b.str("assistant")
				if !placeOK || !pastOK || !assistantOK {
					return plan{}, false
				}
				p.place, p.past, p.assistant = place, past, assistant
				return p, true
			},
			route: func(p plan) LocalRequest {
				route := "/v1/places/" + segment(p.place) + "/resume/"
				if p.assistant != "" {
					route += segment(p.assistant) + "/"
				}
				return LocalRequest{Method: "POST", Path: route + segment(p.past), Body: []byte("{}")}
			}},

		op{name: "voice",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "audio", "rate") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				audio, audioOK := b.nonEmpty("audio")
				rate, rateOK := b.integer("rate")
				// Checked rather than resampled, as the route checks it: a body
				// that names 48000 has not made a small mistake.
				if !audioOK || !rateOK || rate != voiceRate {
					return plan{}, false
				}
				p.audio, p.rate = audio, rate
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/voice",
					Body: jsonBody(map[string]any{"audio": p.audio, "rate": p.rate})}
			}},

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
				batch, ok := b.object("batch", 256<<10)
				if !ok {
					return plan{}, false
				}
				p.document = batch
				return p, true
			}},
	)
}

// voiceRate is the one rate this machine transcribes, as
// internal/adapters/whisper spells it.
const voiceRate = 16000

// decodeAnswer is the one case behind two words. The field's name follows the
// word, which is the only difference between them.
func decodeAnswer(word string) func(b body) (plan, bool) {
	field := "answer"
	if word == "key" {
		field = "key"
	}
	return func(b body) (plan, bool) {
		if !b.hasOneOf([]string{"type", "session", field},
			[]string{"type", "session", field, "request"}) {
			return plan{}, false
		}
		p, ok := actionPlan(b, true)
		if !ok {
			return plan{}, false
		}
		key, ok := b.str(field)
		if !ok {
			return plan{}, false
		}
		p.key = key
		return p, true
	}
}

// routeAnswer is the menu-answer route, which is not `send`.
//
// The Swift app's own comment records why: answering a menu with `/send` sends
// the wrong option — "Tea" arrived as "Water" — so an answer is one raw key
// outside any bracketed paste, and the route allows a closed set of them.
func routeAnswer(p plan) LocalRequest {
	return LocalRequest{Method: "POST", Path: "/v1/sessions/" + segment(p.target) + "/key",
		Body: jsonBody(map[string]any{"key": p.key})}
}
