package cloudops

import (
	"bytes"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/sainteye/clawdline/internal/domain/session"
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

// The Cloud command refuses before forwarding at the same sentence boundary
// as the local /v1/intents route.
const intentTextLimit = 4 << 10

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
	// expect is a menu answer's question, session.MenuFingerprint's hex.
	expect string
	// typed is the end of the words an `enter` submits, and namesTyped whether
	// the body named them at all: a send of pictures alone names "".
	typed                          string
	namesTyped                     bool
	project, item, audience, entry string
	status, query                  string
	environment, category, cursor  string
	// kind is a digest's daily-or-weekly, named rather than folded into `id`.
	kind   string
	images []string
	// conversations is a restore's or a dismissal's list; allConversations
	// is a dismissal that named none, which means every one on offer.
	conversations                   []string
	allConversations                bool
	upcoming, acceptLoss            bool
	closeability                    string
	rate, limit, byteWindow, offset int64
	document                        []byte
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
	// guard refuses one decoded request this machine will not serve over
	// Cloud although the word is served: a menu answer that names no question.
	guard func(p plan) *Refusal
	// anyClass is the one word whose envelope class is not `ctl`: a dispatch
	// is a class of its own, which the relay bills separately.
	anyClass bool
	// shape turns an answer that is not JSON — a picture, a document — into
	// something an envelope can carry.
	shape func(p plan, res LocalResponse) (json.RawMessage, Refusal)
	// sessions marks the one read that is answered by this machine's Session
	// publisher rather than by a local route (Bridge.Sessions): its answer is
	// the rows it puts back on their own channels, which no route can send.
	sessions bool
	// divergence is how this daemon's answer differs from the one the hosted
	// console was written against, for a word that is routed anyway. It is a
	// sentence and not a flag because the differences are not alike, and it is
	// here rather than in a document because whoever decides what to advertise
	// needs it at that moment (see Divergences).
	divergence string
}

// someOf is the query a route is given: the pairs whose value is not empty.
//
// It is not tidiness. The routes under `/v1/work/` refuse a query field that
// is present and empty by name (`workQuery`, "Unknown or repeated query field
// project."), and `/v1/timeline` reads an absent `environment` as production
// and an empty one as a bad one. So "the viewer did not say" has to reach
// these routes as silence, which is what a browser on this machine's own
// network already sends (`URLSearchParams` is only given what was chosen).
func someOf(pairs map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range pairs {
		if value != "" {
			out[key] = value
		}
	}
	return out
}

// decodeWorkPage is the body the two paged board reads share: a Project to
// narrow to, and the opaque cursor the previous page answered. Both may be
// empty, which is the first page of everything.
func decodeWorkPage(word string) func(b body) (plan, bool) {
	return func(b body) (plan, bool) {
		if !b.has("type", "session", "request", "project", "cursor") {
			return plan{}, false
		}
		p, ok := machinePlan(b)
		if !ok {
			return plan{}, false
		}
		project, projectOK := b.str("project")
		cursor, cursorOK := b.str("cursor")
		if !projectOK || !cursorOK || len(project) > 200 || len(cursor) > 200 {
			return plan{}, false
		}
		p.project, p.cursor = project, cursor
		return p, true
	}
}

// decodeProject is the body of the Project worktree lifecycle read: one
// bounded, non-empty Project. It is spelled into the path, where an empty
// segment is a different route, so malformed input is refused here before the
// local router is reached.
func decodeProject(b body) (plan, bool) {
	if !b.has("type", "session", "request", "project") {
		return plan{}, false
	}
	p, ok := machinePlan(b)
	if !ok {
		return plan{}, false
	}
	project, ok := b.nonEmpty("project")
	if !ok || len(project) > 200 {
		return plan{}, false
	}
	p.project = project
	return p, true
}

// decodeProjectRefresh is the command-shaped sibling of decodeProject. A
// refresh changes the machine's cached observation, so it uses an action reply
// and is subject to the remote-write gate.
func decodeProjectRefresh(b body) (plan, bool) {
	if !b.has("type", "session", "request", "project") {
		return plan{}, false
	}
	p, ok := actionPlan(b, false)
	if !ok {
		return plan{}, false
	}
	project, ok := b.nonEmpty("project")
	if !ok || len(project) > 200 {
		return plan{}, false
	}
	p.project = project
	return p, true
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

// conversationList is a list of conversation ids: each one a printable,
// bounded name. How many one request may carry is the route's to refuse
// (sessions.restore_batch), not this bridge's.
func conversationList(value any) ([]string, bool) {
	raw, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		id, ok := sessionName(item)
		if !ok {
			return nil, false
		}
		out = append(out, id)
	}
	return out, true
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
		// No channel, and nothing is wrong with that. The session is kept
		// anyway, because a refusal that cannot be published still has to say
		// what it was about: two menu answers refused on 2026-09-20 were
		// recorded as `answered nobody` with nothing but a code, and finding
		// which of fourteen sessions had been left unable to answer took
		// reading the screen of each one.
		return plan{target: p.target, session: p.session}, true
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
	// The webhook binder preserves the old console's versioned wire shape: its
	// correlation field is request_id and it is always answered on the machine
	// channel. Keep that answer available even when the write gate refuses the
	// command before its full body is decoded.
	if word == "schedule-webhook-bind-v1" {
		request, ok := requestName(b["request_id"])
		if !ok {
			return "", "", false
		}
		return MachineReplySession, "action:" + request, true
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
	case "send", "answer", "key", "end", "focus", "interrupt", "smart-title", "shell-kill":
		return session, "action:" + request, true
	}
	// Every machine command, and any word this machine does not know, is answered
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
	Message: "Cloud dispatch has no pinned wire shape on this machine."}

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

		// MARK: the work system, and what a person is looking at while it runs
		//
		// Five words, not one word with an `area` parameter, and the reason is
		// the one the copied client already wrote down for `board.items`
		// (`legacy/js/net/cloud-client.js`): every read's decoder here compares
		// the body's key set for exact equality, so a parameter that grows is a
		// machine that refuses. One word carrying `area` would also make this
		// machine advertise the whole board in its descriptor the moment it
		// could answer any part of it — `commands` is a list of words, so a
		// word is the finest thing a browser can be told about. Five words let
		// an older machine say exactly which areas it has, let the page learn
		// `unknown_command` per area (`machineLacks`), and let a divergence be
		// stated about one of them. The cost is five entries in a table.
		//
		// All five are the same page's reads (`web/console/src/pages/work/`),
		// and this round carries only what that page reads. `GET
		// /v1/work/items/{id}` has no console reader — the page draws an item
		// out of the board and the Backlog pages — and every `POST
		// /v1/work/*` is a person answering, which is a write and is not here.

		op{name: "work.board", read: true,
			decode: decodeWorkPage("work.board"),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/work/board", Query: someOf(map[string]string{
					"project": p.project, "cursor": p.cursor,
				})}
			}},

		op{name: "work.backlog", read: true,
			decode: decodeWorkPage("work.backlog"),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/work/backlog", Query: someOf(map[string]string{
					"project": p.project, "cursor": p.cursor,
				})}
			}},

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

		op{name: "work.v2.items", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "project") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				project, projectOK := b.str("project")
				if !ok || !projectOK || len(project) > 256 {
					return plan{}, false
				}
				p.project = project
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/work/v2/items",
					Query: someOf(map[string]string{"project": p.project})}
			}},

		op{name: "work.v2.search", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "project", "status", "query") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				project, projectOK := b.str("project")
				status, statusOK := b.str("status")
				query, queryOK := b.str("query")
				if !ok || !projectOK || len(project) > 256 || !statusOK ||
					(status != "open" && status != "done" && status != "all") ||
					!queryOK || len(query) > 1024 {
					return plan{}, false
				}
				p.project, p.status, p.query = project, status, query
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/work/v2/items",
					Query: someOf(map[string]string{"project": p.project, "status": p.status, "q": p.query})}
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
				if !b.has("type", "session", "request", "terminal") {
					return plan{}, false
				}
				p, ok := machinePlan(b)
				terminal, terminalOK := b.nonEmpty("terminal")
				if !ok || !terminalOK || len(terminal) > 256 {
					return plan{}, false
				}
				p.target = terminal
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/work/v2/session-todos/" + segment(p.target)}
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

		op{name: "work.v2.assign",
			decode: decodeWorkV2NamedDocument("id", "item"),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/work/v2/items/" + segment(p.id) + "/assign",
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

		// The Projects page's catalog and worktree lifecycle. `places` above is
		// carried separately.
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

		// The landing ledger, machine-wide. It takes no project because the
		// debt is not one repository's: the question the page asking for it
		// puts is "what have I got out and not recorded", and a reader who
		// had to name a repository first would be shown one repository's
		// rows as if they were the machine's whole answer — the same shape as
		// drawing an unread source as `0`, spelled differently.
		op{name: "landings", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/orchestrator/landings"}
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

		// MARK: reads this daemon has no local capability for

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
					[]string{"type", "session", "request", "orchestrator"}) {
					return plan{}, false
				}
				if value, present := b["orchestrator"]; present {
					if asked, ok := value.(bool); !ok || !asked {
						return plan{}, false
					}
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

		op{name: "answer", decode: decodeAnswer("answer"), route: routeAnswer, guard: answerNamesItsQuestion},
		// The Swift bridge takes `answer` and `key` as one case, and the
		// hosted console still sends either depending on how old the tab is.
		op{name: "key", decode: decodeAnswer("key"), route: routeAnswer, guard: answerNamesItsQuestion},

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

		// Stopping the turn a session is working on: the one Escape its own
		// working line asks for. Not a Swift word — that app had no stop — and
		// not `key`, whose allowlist is a menu's answers and which this bridge
		// refuses unless the answer names its question; a stop has no question.
		op{name: "interrupt",
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
				return LocalRequest{Method: "POST", Path: "/v1/sessions/" + segment(p.target) + "/interrupt", Body: []byte("{}")}
			}},

		op{name: "smart-title",
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
				return LocalRequest{Method: "POST", Path: "/v1/sessions/" + segment(p.target) + "/smart-title", Body: []byte("{}")}
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

		// The sessions a reboot took away (docs/session-restore.md). The
		// read is the list; the two commands carry the viewer's request as
		// their Idempotency-Key, as `resume` does, because a restore is one
		// resume per conversation and a retried envelope must not open a
		// second tab.
		op{name: "restorable-sessions", read: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request") {
					return plan{}, false
				}
				return machinePlan(b)
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "GET", Path: "/v1/sessions/restorable"}
			}},

		op{name: "restore-sessions",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "conversations") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				ids, ok := conversationList(b["conversations"])
				if !ok || len(ids) == 0 {
					return plan{}, false
				}
				p.conversations = ids
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/sessions/restorable/restore",
					Body: jsonBody(map[string]any{"conversations": p.conversations})}
			}},

		op{name: "dismiss-restorable",
			decode: func(b body) (plan, bool) {
				if !b.hasOneOf([]string{"type", "session", "request"},
					[]string{"type", "session", "request", "conversations"}) {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				raw, named := b["conversations"]
				if !named {
					p.allConversations = true
					return p, true
				}
				ids, ok := conversationList(raw)
				if !ok {
					return plan{}, false
				}
				p.conversations = ids
				return p, true
			},
			route: func(p plan) LocalRequest {
				if p.allConversations {
					return LocalRequest{Method: "POST", Path: "/v1/sessions/restorable/dismiss", Body: []byte("{}")}
				}
				return LocalRequest{Method: "POST", Path: "/v1/sessions/restorable/dismiss",
					Body: jsonBody(map[string]any{"conversations": p.conversations})}
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

		op{name: "intents",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "text") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				text, ok := b.str("text")
				if !ok || len([]byte(text)) > intentTextLimit {
					return plan{}, false
				}
				p.text = text
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/intents",
					Body: jsonBody(map[string]any{"text": p.text})}
			}},

		op{name: "schedule-create",
			decode: decodeScheduleWrite(false),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/orchestrator/schedules",
					Body: p.document, Header: asDevice()}
			}},

		op{name: "schedule-update",
			decode: decodeScheduleWrite(true),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "PATCH",
					Path: "/v1/orchestrator/schedules/" + segment(p.id),
					Body: p.document, Header: asDevice()}
			}},

		op{name: "schedule-delete",
			decode: decodeNamedSchedule,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "DELETE",
					Path: "/v1/orchestrator/schedules/" + segment(p.id), Header: asDevice()}
			}},

		op{name: "schedule-run",
			decode: decodeNamedSchedule,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST",
					Path:   "/v1/orchestrator/schedules/" + segment(p.id) + "/run",
					Body:   []byte("{}"),
					Header: asDevice()}
			}},

		// The old hosted console's two-phase webhook creation flow. Cloud creates
		// the capability under the browser session, then this encrypted command
		// makes the selected Mac persist the schedule binding and activate it with
		// its machine credential. The trigger URL is deliberately absent here.
		op{name: "schedule-webhook-bind-v1",
			decode: decodeScheduleWebhookBind,
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/orchestrator/schedule-webhooks/bind",
					Body: p.document, Header: asDevice()}
			}},

		// The four snippet writes. The key sets are the producer's, word for
		// word (`net/cloud-client.js`: `createSnippet`, `updateSnippet`,
		// `deleteSnippet`, `orderSnippets`), and each one hands its
		// sub-document straight to the local route that owns its shape — this
		// bridge knows what a request looks like, not what a snippet looks
		// like.
		//
		// They carry `X-Clawdline-Actor: device` for the reason the schedule
		// writes do, said for this door: `snippetWrite` asks which door the
		// caller came in by, and the answer decides the name the write is
		// filed under in the receipt table (`personPrincipal`). Without the
		// header an in-process Cloud request is this machine's own hand —
		// `local` — so a person's press from their phone and a script's write
		// on this machine would share one receipt namespace. It opens nothing:
		// the route needs a device that may send, which it needed anyway.
		op{name: "snippet-create",
			decode: decodeSnippetWrite(false),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/snippets",
					Body: p.document, Header: asDevice()}
			}},

		op{name: "snippet-update",
			decode: decodeSnippetWrite(true),
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "PATCH", Path: "/v1/snippets/" + segment(p.id),
					Body: p.document, Header: asDevice()}
			}},

		op{name: "snippet-delete",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "id") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
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
				return LocalRequest{Method: "DELETE", Path: "/v1/snippets/" + segment(p.id),
					Header: asDevice()}
			}},

		// One group's complete order. `ordering` is the producer's name for
		// the body the local route reads under no name at all, which is the
		// one place these two spellings differ.
		op{name: "snippet-order",
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "ordering") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				document, ok := b.object("ordering", snippetMaximumBytes)
				if !ok {
					return plan{}, false
				}
				p.document = document
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/snippets/order",
					Body: p.document, Header: asDevice()}
			}},

		// The three words that change something about notifications, all
		// read-level: the Swift bridge lists exactly these three beside the
		// two diagnostics words in `readLevelCommandTypes`. The write switch
		// is about typing into somebody's session, and asking to be told when
		// one needs an answer is the reading half arriving by another road —
		// a phone paired read-only is the device this feature exists for.
		//
		// Each carries `X-Clawdline-Actor: device` for the reason the schedule
		// writes do, said for a different door: `/v1/push/unsubscribe` exempts
		// this machine's own token from the check that a subscription belongs
		// to the device removing it, because that exemption is how a script
		// cleans up after itself. A Cloud viewer reaches these routes in
		// process holding exactly that token, so without this header a person
		// on their phone would arrive wearing the script's exemption.
		op{name: "push-subscribe", readLevel: true,
			divergence: "every Cloud viewer is the same device here: an in-process Cloud " +
				"request carries this machine's own local credential, and the store keeps " +
				"one row per device, so a second Cloud browser asking to be notified takes " +
				"the first one's place — where two devices paired to this machine's own " +
				"network keep a row each. It needs the viewer's own identity to reach the " +
				"route, which no word on this wire carries. Measured in " +
				"internal/transport/http's `TestEveryCloudViewerIsTheSameDeviceHere`. " +
				"The web-app origin stored beside it was this daemon's `http://127.0.0.1` " +
				"for a while, which is what an iOS declarative notification resolved its " +
				"address against and is why a notification that arrived opened nothing; " +
				"it is now `cloud_app_origin`, carried to the route beside the credential",
			decode: func(b body) (plan, bool) {
				// `cloud_subscription_id` is present when the browser
				// subscribed with Clawdline Cloud's VAPID key rather than this
				// machine's: the row is then sealed here and forwarded
				// through Cloud (docs/push.md, "Cloud-sent push").
				if !b.hasOneOf([]string{"type", "session", "request", "subscription"},
					[]string{"type", "session", "request", "subscription", "cloud_subscription_id"}) {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				// The browser's own object, carried as the bytes it will
				// travel as. What counts as a usable subscription is the
				// route's question (`push.FromBrowser`), and it is asked
				// there so that an endpoint this machine will POST to is
				// checked in exactly one place — the cloud id with it,
				// which rides beside the object's own keys.
				subscription, ok := b["subscription"].(map[string]any)
				if !ok {
					return plan{}, false
				}
				if cloudID, present := b["cloud_subscription_id"]; present {
					if _, clash := subscription["cloud_subscription_id"]; clash {
						return plan{}, false
					}
					merged := make(map[string]any, len(subscription)+1)
					for key, value := range subscription {
						merged[key] = value
					}
					merged["cloud_subscription_id"] = cloudID
					b = body{"subscription": merged}
				}
				document, ok := b.object("subscription", pushBodyMaximumBytes)
				if !ok {
					return plan{}, false
				}
				p.document = document
				return p, true
			},
			route: func(p plan) LocalRequest {
				return LocalRequest{Method: "POST", Path: "/v1/push/subscribe",
					Body: p.document, Header: asDevice()}
			}},

		op{name: "push-unsubscribe", readLevel: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "id") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
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
				return LocalRequest{Method: "POST", Path: "/v1/push/unsubscribe",
					Body:   jsonBody(map[string]any{"id": p.id}),
					Header: asDevice()}
			}},

		op{name: "push-test", readLevel: true,
			decode: func(b body) (plan, bool) {
				if !b.has("type", "session", "request", "target") {
					return plan{}, false
				}
				p, ok := actionPlan(b, false)
				if !ok || p.request == "" {
					return plan{}, false
				}
				// An empty target is a test with nothing to tap back to,
				// which goes to the list: the key is required, its value may
				// be "". The route's field for it is `session_id`, and the
				// body leaves it out entirely rather than sending an empty
				// one, as the Swift bridge does.
				target, ok := b.str("target")
				if !ok {
					return plan{}, false
				}
				p.target = target
				return p, true
			},
			route: func(p plan) LocalRequest {
				sent := map[string]any{}
				if p.target != "" {
					sent["session_id"] = p.target
				}
				return LocalRequest{Method: "POST", Path: "/v1/push/test",
					Body: jsonBody(sent), Header: asDevice()}
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

// scheduleMaximumBytes is what one schedule weighs on the wire, matching the
// local route's own reader (`scheduleBody` in internal/transport/http). This
// bridge does not know what a schedule looks like; it knows how much of one
// the route on the other side will read.
const scheduleMaximumBytes = 256 << 10

// snippetMaximumBytes is what one snippet write weighs on the wire, matching
// the local route's own reader (`snippetBodyLimit` in internal/transport/http).
// This bridge does not know what a snippet looks like; it knows how much of one
// the route on the other side will read.
const snippetMaximumBytes = 64 << 10

// pushBodyMaximumBytes is what one subscription weighs on the wire, matching
// the local route's own reader (`authBodyLimit`, the bound `/v1/push/` is
// registered under in internal/transport/http). This bridge does not know what
// a subscription looks like; it knows how much of one the route on the other
// side will read.
const pushBodyMaximumBytes = 64 << 10

// workV2CloudBodyLimit matches the one body the local work-system route will
// read. The Cloud bridge carries the person's JSON object without interpreting
// it and refuses a larger envelope before routing it.
const (
	workV2CloudBodyLimit      = 96 << 10
	workV2CloudImageBodyLimit = 18 << 20
	// projectSyncCloudBodyLimit is one project's settings with contents:
	// internal/domain/projectsync.MaxEntryBytes.
	projectSyncCloudBodyLimit = 4 << 20
)

// The header a Cloud write puts on its own local request, and the reason the
// schedule words carry it.
//
// A schedule write has two doors on this machine (internal/app's
// `MachineRefusal`): a person's paired device that may send, which arranges
// any schedule, and this machine's own orchestrator token, which may make,
// change and remove only a schedule that runs **once**. The second door is
// narrow because the orchestrator token is what this machine's own automation
// holds; an automation that could rewrite a daily arrangement could quietly
// rewrite what it itself does every day, and nobody would have decided that.
//
// A Cloud viewer is the first kind and not the second — the Swift producer
// says so in one line, giving a verified Cloud request the identity
// `cloud:<sender>` with `read` and `send` and no orchestrator credential at
// all (`RemoteServer.permission(for:)`). This daemon reaches its own routes
// in process instead, and the credential stamped on them covers every path
// `/v1/orchestrator/*` (`internal/transport/cloud.LocalAuthorizer`) — so
// without this header a person editing a daily schedule from their phone
// would be refused as though they were a cron job, by a sentence that tells
// them to use a paired device that may send, which is what they are.
//
// The header only ever takes authority away: it cannot grant the machine door
// to anyone, it can only close it. That is why the gate may honour it from
// any caller (internal/transport/http's `actorHeader`).
const (
	actorHeader = "X-Clawdline-Actor"
	actorDevice = "device"
)

// asDevice is that header, fresh per request: LocalRequest.Header is written
// to by the router (the idempotency key), so two routes must not share a map.
func asDevice() map[string]string { return map[string]string{actorHeader: actorDevice} }

func decodeWorkV2Document(field string) func(body) (plan, bool) {
	return func(b body) (plan, bool) {
		if !b.has("type", "session", "request", field) {
			return plan{}, false
		}
		p, ok := actionPlan(b, false)
		document, documentOK := b.object(field, workV2CloudBodyLimit)
		if !ok || p.request == "" || !documentOK {
			return plan{}, false
		}
		p.document = document
		return p, true
	}
}

func decodeWorkV2NamedDocument(idField, documentField string) func(body) (plan, bool) {
	return func(b body) (plan, bool) {
		if !b.has("type", "session", "request", idField, documentField) {
			return plan{}, false
		}
		p, ok := actionPlan(b, false)
		id, idOK := b.nonEmpty(idField)
		document, documentOK := b.object(documentField, workV2CloudBodyLimit)
		if !ok || p.request == "" || !idOK || len(id) > 256 || !documentOK {
			return plan{}, false
		}
		p.id, p.document = id, document
		return p, true
	}
}

func decodeWorkV2NamedImageDocument(b body) (plan, bool) {
	if !b.has("type", "session", "request", "id", "item") {
		return plan{}, false
	}
	p, ok := actionPlan(b, false)
	id, idOK := b.nonEmpty("id")
	document, documentOK := b.object("item", workV2CloudImageBodyLimit)
	if !ok || p.request == "" || !idOK || len(id) > 256 || !documentOK {
		return plan{}, false
	}
	p.id, p.document = id, document
	return p, true
}

func decodeWorkV2ImageDelete(b body) (plan, bool) {
	if !b.has("type", "session", "request", "id", "image", "item") {
		return plan{}, false
	}
	p, ok := actionPlan(b, false)
	id, idOK := b.nonEmpty("id")
	image, imageOK := b.nonEmpty("image")
	document, documentOK := b.object("item", workV2CloudBodyLimit)
	if !ok || p.request == "" || !idOK || len(id) > 256 || !imageOK || len(image) > 256 || !documentOK {
		return plan{}, false
	}
	p.id, p.item, p.document = id, image, document
	return p, true
}

func decodeWorkV2Action(field string, allowed ...string) func(body) (plan, bool) {
	return func(b body) (plan, bool) {
		if !b.has("type", "session", "request", "id", field, "item") {
			return plan{}, false
		}
		p, ok := actionPlan(b, false)
		id, idOK := b.nonEmpty("id")
		action, actionOK := b.nonEmpty(field)
		document, documentOK := b.object("item", workV2CloudBodyLimit)
		if !ok || p.request == "" || !idOK || len(id) > 256 || !actionOK || !documentOK {
			return plan{}, false
		}
		known := false
		for _, candidate := range allowed {
			known = known || action == candidate
		}
		if !known {
			return plan{}, false
		}
		p.id, p.kind, p.document = id, action, document
		return p, true
	}
}

func decodeWorkV2TerminalDocument(b body) (plan, bool) {
	if !b.has("type", "session", "request", "terminal", "item") {
		return plan{}, false
	}
	p, ok := actionPlan(b, false)
	terminal, terminalOK := b.nonEmpty("terminal")
	document, documentOK := b.object("item", workV2CloudBodyLimit)
	if !ok || p.request == "" || !terminalOK || len(terminal) > 256 || !documentOK {
		return plan{}, false
	}
	p.target, p.document = terminal, document
	return p, true
}

func decodeWorkV2TodoImageDocument(b body) (plan, bool) {
	if !b.has("type", "session", "request", "terminal", "id", "item") {
		return plan{}, false
	}
	p, ok := actionPlan(b, false)
	terminal, terminalOK := b.nonEmpty("terminal")
	id, idOK := b.nonEmpty("id")
	document, documentOK := b.object("item", workV2CloudImageBodyLimit)
	if !ok || p.request == "" || !terminalOK || len(terminal) > 256 || !idOK || len(id) > 256 || !documentOK {
		return plan{}, false
	}
	p.target, p.id, p.document = terminal, id, document
	return p, true
}

func decodeWorkV2TodoAction(b body) (plan, bool) {
	if !b.has("type", "session", "request", "terminal", "id", "action", "item") {
		return plan{}, false
	}
	p, ok := actionPlan(b, false)
	terminal, terminalOK := b.nonEmpty("terminal")
	id, idOK := b.nonEmpty("id")
	action, actionOK := b.nonEmpty("action")
	document, documentOK := b.object("item", workV2CloudBodyLimit)
	if !ok || p.request == "" || !terminalOK || len(terminal) > 256 || !idOK || len(id) > 256 ||
		!actionOK || !documentOK || (action != "send" && action != "complete" && action != "delete") {
		return plan{}, false
	}
	p.target, p.id, p.kind, p.document = terminal, id, action, document
	return p, true
}

// decodeScheduleWrite is `schedule-create` and `schedule-update`, which differ
// by one key: the id of the schedule being saved.
func decodeScheduleWrite(named bool) func(b body) (plan, bool) {
	want := []string{"type", "session", "request", "schedule"}
	if named {
		want = []string{"type", "session", "request", "id", "schedule"}
	}
	return func(b body) (plan, bool) {
		if !b.has(want...) {
			return plan{}, false
		}
		p, ok := actionPlan(b, false)
		if !ok || p.request == "" {
			return plan{}, false
		}
		if named {
			id, ok := b.nonEmpty("id")
			if !ok {
				return plan{}, false
			}
			p.id = id
		}
		// The object is carried as the bytes it will travel as. What counts as
		// a schedule is the route's question, and it answers it with its own
		// sentence naming the field that is wrong — which is the sentence the
		// form already shows over this machine's own network.
		document, ok := b.object("schedule", scheduleMaximumBytes)
		if !ok {
			return plan{}, false
		}
		p.document = document
		return p, true
	}
}

// decodeSnippetWrite is `snippet-create` and `snippet-update`, which differ by
// one key: the id of the snippet being saved.
func decodeSnippetWrite(named bool) func(b body) (plan, bool) {
	want := []string{"type", "session", "request", "snippet"}
	if named {
		want = []string{"type", "session", "request", "id", "snippet"}
	}
	return func(b body) (plan, bool) {
		if !b.has(want...) {
			return plan{}, false
		}
		p, ok := actionPlan(b, false)
		if !ok || p.request == "" {
			return plan{}, false
		}
		if named {
			id, ok := b.nonEmpty("id")
			if !ok {
				return plan{}, false
			}
			p.id = id
		}
		// The object travels as the bytes it arrived as. What counts as a
		// snippet is `internal/domain/snippet`'s question, answered once there
		// and never here — the sentence a person reads about a title that is
		// too long is the one the sheet already shows over this machine's own
		// network.
		document, ok := b.object("snippet", snippetMaximumBytes)
		if !ok {
			return plan{}, false
		}
		p.document = document
		return p, true
	}
}

// decodeNamedSchedule is `schedule-delete` and `schedule-run`: one id, and
// nothing else to get wrong.
func decodeNamedSchedule(b body) (plan, bool) {
	if !b.has("type", "session", "request", "id") {
		return plan{}, false
	}
	p, ok := actionPlan(b, false)
	if !ok || p.request == "" {
		return plan{}, false
	}
	id, ok := b.nonEmpty("id")
	if !ok {
		return plan{}, false
	}
	p.id = id
	return p, true
}

func scheduleWebhookHookID(value string) bool {
	if len(value) != 30 || !strings.HasPrefix(value, "swh_") {
		return false
	}
	for _, r := range value[4:] {
		if !strings.ContainsRune("0123456789abcdefghjkmnpqrstvwxyz", r) {
			return false
		}
	}
	return true
}

// decodeScheduleWebhookBind keeps the original console's exact wire body.
// `request_id` is also the local receipt key; replace is explicitly null for
// a first binding and a hook id only when replacing a disabled hook.
// `hook_revision`, the revision a moved hook is at, is optional and travels
// only when sent, so a console that sends none binds a new hook as before.
func decodeScheduleWebhookBind(b body) (plan, bool) {
	if !b.hasOneOf([]string{"type", "request_id", "hook_id", "schedule_id", "replace_hook_id"},
		[]string{"type", "request_id", "hook_id", "schedule_id", "replace_hook_id", "hook_revision"}) {
		return plan{}, false
	}
	requestID, requestOK := b.nonEmpty("request_id")
	hookID, hookOK := b.nonEmpty("hook_id")
	scheduleID, scheduleOK := b.nonEmpty("schedule_id")
	if !requestOK || !hookOK || !scheduleOK || !isTaskID(requestID) ||
		!scheduleWebhookHookID(hookID) || !isTaskID(scheduleID) {
		return plan{}, false
	}
	var replace any
	if raw := b["replace_hook_id"]; raw != nil {
		value, ok := raw.(string)
		if !ok || !scheduleWebhookHookID(value) {
			return plan{}, false
		}
		replace = value
	}
	fields := map[string]any{
		"request_id": requestID, "hook_id": hookID,
		"schedule_id": scheduleID, "replace_hook_id": replace,
	}
	if _, sent := b["hook_revision"]; sent {
		revision, ok := b.integer("hook_revision")
		if !ok || revision < 0 {
			return plan{}, false
		}
		fields["hook_revision"] = revision
	}
	document := jsonBody(fields)
	return plan{session: MachineReplySession, name: "action:" + requestID,
		request: requestID, document: document}, true
}

// decodeAnswer is the one case behind two words. The field's name follows the
// word, which is the only difference between them.
func decodeAnswer(word string) func(b body) (plan, bool) {
	field := "answer"
	if word == "key" {
		field = "key"
	}
	return func(b body) (plan, bool) {
		if !b.hasOneOf([]string{"type", "session", field},
			[]string{"type", "session", field, "request"},
			[]string{"type", "session", field, "request", "expect"},
			[]string{"type", "session", field, "request", "typed"}) {
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
		if _, named := b["expect"]; named {
			expect, ok := b.str("expect")
			if !ok || !session.ValidFingerprint(expect) {
				return plan{}, false
			}
			p.expect = expect
		}
		if _, named := b["typed"]; named {
			typed, ok := b.str("typed")
			if !ok {
				return plan{}, false
			}
			p.typed, p.namesTyped = typed, true
		}
		return p, true
	}
}

// answerNamesItsQuestion refuses a menu answer that does not say which
// question it was chosen for (F1).
//
// A digit answers whatever picker is up when it lands. Across Clawdline Cloud
// the reply is slow and can be lost, the row a page drew from is seconds old,
// and a second device can answer first — so an unnamed "1" is how a
// permission prompt gets approved for the next tool call, which nobody read.
// The shapes without `expect` still decode, so the refusal reaches the page
// that sent one by its code rather than as a malformed body; nothing is asked
// of this machine's own route.
//
// `enter` is the one key that answers no question: it submits a send that was
// typed and never submitted, and names those words (`typed`) instead, which
// the machine checks are still in the input line before it presses anything
// (app.SubmitTyped). An `enter` that names neither is refused the same way.
func answerNamesItsQuestion(p plan) *Refusal {
	if p.expect != "" {
		return nil
	}
	if p.key == "enter" && p.namesTyped {
		return nil
	}
	return &Refusal{Status: 428, Code: "menu_unverified",
		Message: "This machine answers a menu over Clawdline Cloud only when the answer names the question it was chosen for. Reload the page and answer again."}
}

// routeAnswer is the menu-answer route, which is not `send`.
//
// The Swift app's own comment records why: answering a menu with `/send` sends
// the wrong option — "Tea" arrived as "Water" — so an answer is one raw key
// outside any bracketed paste, and the route allows a closed set of them. The
// question it names travels with it, and the route types nothing at any other.
func routeAnswer(p plan) LocalRequest {
	body := map[string]any{"key": p.key}
	if p.expect != "" {
		body["expect"] = p.expect
	}
	if p.namesTyped {
		body["typed"] = p.typed
	}
	return LocalRequest{Method: "POST", Path: "/v1/sessions/" + segment(p.target) + "/key",
		Body: jsonBody(body)}
}

// usageID is `usageID` in internal/transport/http/usage.go, spelled a second
// time because this package cannot import a transport: letters, digits and
// `-`, `_`, `.`, starting with a letter or digit, at most 200, and never `..`.
// A looser copy would ask the route something it refuses as `bad_request`; a
// stricter one would refuse on a phone an id the machine's own page reads.
var usageID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`)

// usageRead is one of the token ledger's three reads: `{type, session,
// request, id}` on the machine's reply channel, asked as
// GET /v1/usage/<kind>/<id>.
func usageRead(word, kind string) op {
	return op{name: word, read: true,
		decode: func(b body) (plan, bool) {
			if !b.has("type", "session", "request", "id") {
				return plan{}, false
			}
			p, ok := machinePlan(b)
			id, idOK := b.str("id")
			if !ok || !idOK || !usageID.MatchString(id) || strings.Contains(id, "..") {
				return plan{}, false
			}
			p.id = id
			return p, true
		},
		route: func(p plan) LocalRequest {
			return LocalRequest{Method: "GET", Path: "/v1/usage/" + kind + "/" + segment(p.id)}
		}}
}

// compareSince is the spelling of `since` the comparison route reads
// (app.ParseCompareSince): `<n>d`, `<n>h` or a Unix time in seconds, spelled
// a second time for the reason usageID is. The route still refuses a range it
// does not take — `0d`, more than ten years — with its own `bad_request`;
// this only keeps anything that is not a number and a unit off its query.
var compareSince = regexp.MustCompile(`^[0-9]{1,12}[dh]?$`)

// verificationID is app.VerificationIDShape, spelled a second time for the
// reason usageID is: letters, digits and dashes, at most 64.
var verificationID = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)

// verificationCriteriaMaximum is app's verificationCriteriaLimit: an index at
// or past it names no criterion on any record.
const verificationCriteriaMaximum = 12

// verificationCloudBodyLimit is the largest sub-document a verification word
// carries: a note of 8000 characters, each up to four bytes, escaped.
const verificationCloudBodyLimit = 64 << 10

// decodeVerificationWrite is a verification command's body: the record's id
// when it names one, and the `verification` document the route reads.
func decodeVerificationWrite(named bool) func(b body) (plan, bool) {
	want := []string{"type", "session", "request", "verification"}
	if named {
		want = []string{"type", "session", "request", "id", "verification"}
	}
	return func(b body) (plan, bool) {
		if !b.has(want...) {
			return plan{}, false
		}
		p, ok := actionPlan(b, false)
		if !ok || p.request == "" {
			return plan{}, false
		}
		if named {
			id, ok := b.str("id")
			if !ok || !verificationID.MatchString(id) {
				return plan{}, false
			}
			p.id = id
		}
		document, ok := b.object("verification", verificationCloudBodyLimit)
		if !ok {
			return plan{}, false
		}
		p.document = document
		return p, true
	}
}
