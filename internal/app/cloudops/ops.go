package cloudops

import (
	"bytes"
	"encoding/json"
	"github.com/sainteye/clawdline/internal/adapters/projectfiles"
	"github.com/sainteye/clawdline/internal/domain/session"
	"github.com/sainteye/clawdline/internal/domain/squad"
	"regexp"
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
	// persona is the catalog id a start or a resume names, or "" for none.
	persona string
	// typed is the end of the words an `enter` submits, and namesTyped whether
	// the body named them at all: a send of pictures alone names "".
	typed                          string
	namesTyped                     bool
	project, item, audience, entry string
	status, query                  string
	// until is the work samples' end, a Unix time; query holds their since.
	until                         string
	environment, category, cursor string
	// kind is a digest's daily-or-weekly, named rather than folded into `id`.
	kind   string
	images []string
	// conversations is a restore's or a dismissal's list; allConversations
	// is a dismissal that named none, which means every one on offer.
	conversations                          []string
	allConversations                       bool
	upcoming, acceptLoss                   bool
	closeability                           string
	rate, limit, byteWindow, offset, after int64
	document                               []byte
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
	// partial reads a refusal that is also an answer: a route that stopped
	// part-way and says what it did. It returns that body when the response
	// is one, and the body crosses whole beside the refusal's status instead
	// of as an `error`, whose fields the reader filters by code (§11.6).
	partial func(res LocalResponse) (json.RawMessage, bool)
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

func projectFileID(b body) (string, bool) {
	id, ok := b.nonEmpty("file")
	if !ok || len(id) != 32 {
		return "", false
	}
	for _, c := range id {
		if c < '0' || c > '9' && c < 'a' || c > 'f' {
			return "", false
		}
	}
	return id, true
}

func decodeProjectFileList(b body) (plan, bool) {
	if !b.has("type", "session", "request", "project") {
		return plan{}, false
	}
	p, ok := machinePlan(b)
	project, projectOK := b.nonEmpty("project")
	if !ok || !projectOK || len(project) > 200 {
		return plan{}, false
	}
	p.project = project
	return p, true
}

func decodeProjectFileRead(b body) (plan, bool) {
	if !b.has("type", "session", "request", "project", "file") {
		return plan{}, false
	}
	p, ok := machinePlan(b)
	project, projectOK := b.nonEmpty("project")
	file, fileOK := projectFileID(b)
	if !ok || !projectOK || len(project) > 200 || !fileOK {
		return plan{}, false
	}
	p.project, p.id = project, file
	return p, true
}

func decodeProjectTreeList(b body) (plan, bool) {
	if !b.has("type", "session", "request", "project", "directory") {
		return plan{}, false
	}
	p, ok := machinePlan(b)
	project, projectOK := b.nonEmpty("project")
	directory, pathOK := b.str("directory")
	_, valid := projectfiles.TreePath(directory, false)
	if !ok || !projectOK || len(project) > 200 || !pathOK || valid != nil {
		return plan{}, false
	}
	p.project, p.path = project, directory
	return p, true
}

func decodeProjectTreeRead(b body) (plan, bool) {
	if !b.has("type", "session", "request", "project", "path") {
		return plan{}, false
	}
	p, ok := machinePlan(b)
	project, projectOK := b.nonEmpty("project")
	path, pathOK := b.nonEmpty("path")
	_, valid := projectfiles.TreePath(path, true)
	if !ok || !projectOK || len(project) > 200 || !pathOK || valid != nil {
		return plan{}, false
	}
	p.project, p.path = project, path
	return p, true
}

func decodeProjectFileSave(b body) (plan, bool) {
	if !b.has("type", "session", "request", "project", "file", "item") {
		return plan{}, false
	}
	p, ok := actionPlan(b, false)
	project, projectOK := b.nonEmpty("project")
	file, fileOK := projectFileID(b)
	document, documentOK := b.object("item", projectfiles.MaxWriteBytes)
	if !ok || p.request == "" || !projectOK || len(project) > 200 || !fileOK || !documentOK {
		return plan{}, false
	}
	p.project, p.id, p.document = project, file, document
	return p, true
}

// decodeProjectUnifyApply carries only the plan version the person read; the
// machine recomputes the plan and refuses plan_changed when disk moved on.
func decodeProjectUnifyApply(b body) (plan, bool) {
	if !b.has("type", "session", "request", "project", "item") {
		return plan{}, false
	}
	p, ok := actionPlan(b, false)
	project, projectOK := b.nonEmpty("project")
	document, documentOK := b.object("item", 4<<10)
	if !ok || p.request == "" || !projectOK || len(project) > 200 || !documentOK {
		return plan{}, false
	}
	p.project, p.document = project, document
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

func squadCloudScopePlan(b body) (plan, bool) {
	if !b.hasOneOf(
		[]string{"type", "session", "request"},
		[]string{"type", "session", "request", "place_id"},
		[]string{"type", "session", "request", "scope_id"}) {
		return plan{}, false
	}
	p, ok := machinePlan(b)
	if !ok {
		return plan{}, false
	}
	if v, exists := b["place_id"]; exists {
		p.place, ok = v.(string)
		if !ok || p.place == "" || len(p.place) > 256 || !printable(p.place) {
			return plan{}, false
		}
	}
	if v, exists := b["scope_id"]; exists {
		p.scope, ok = v.(string)
		if !ok || p.scope == "" || len(p.scope) > 256 || !printable(p.scope) {
			return plan{}, false
		}
	}
	return p, true
}

func squadCloudQuery(p plan) map[string]string {
	out := map[string]string{}
	if p.place != "" {
		out["place_id"] = p.place
	}
	if p.scope != "" {
		out["scope_id"] = p.scope
	}
	return out
}

func squadCloudWritePlan(field string) func(body) (plan, bool) {
	return func(b body) (plan, bool) {
		if !b.has("type", "session", "request", field) {
			return plan{}, false
		}
		p, ok := actionPlan(b, false)
		if !ok || p.request == "" {
			return plan{}, false
		}
		p.document, ok = b.object(field, squad.MaxSquadRequestBytes)
		return p, ok
	}
}

// personaName is a start's or a resume's optional `persona`: absent is none,
// and present it is a non-empty, bounded, printable name, held to the same
// shape as a session name before it becomes a path segment. Whether the catalog
// has it is the local route's to say (`unknown_persona`), so the code a page
// reads is the one the machine's own network would have given it.
func personaName(b body) (string, bool) {
	value, present := b["persona"]
	if !present {
		return "", true
	}
	return sessionName(value)
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
	case "send", "answer", "key", "end", "archive-session", "focus", "interrupt", "smart-title", "shell-kill":
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
	Message: "Cloud dispatch has no pinned wire shape on this machine.", fixedCopy: true}

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

// defaultModelsCloudBodyLimit matches the HTTP routes' 64 KiB JSON body
// ceiling. It bounds the small Settings objects before the bridge copies them
// into local requests: the two default-model changes, the two work-gate
// changes, and one Board command.
const defaultModelsCloudBodyLimit = 64 << 10

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
		!actionOK || !documentOK || (action != "send" && action != "complete" && action != "reopen" && action != "delete") {
		return plan{}, false
	}
	p.target, p.id, p.kind, p.document = terminal, id, action, document
	return p, true
}

func decodeWorkV2HumanInterventionAction(b body) (plan, bool) {
	if !b.has("type", "session", "request", "terminal", "id", "action", "item") {
		return plan{}, false
	}
	p, ok := actionPlan(b, false)
	conversation, conversationOK := b.nonEmpty("terminal")
	id, idOK := b.nonEmpty("id")
	action, actionOK := b.nonEmpty("action")
	document, documentOK := b.object("item", workV2CloudBodyLimit)
	if !ok || p.request == "" || !conversationOK || len(conversation) > 256 || !idOK || len(id) > 256 ||
		!actionOK || !documentOK || (action != "read" && action != "resolve" && action != "reopen") {
		return plan{}, false
	}
	p.target, p.id, p.kind, p.document = conversation, id, action, document
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
		Message: "This machine answers a menu over Clawdline Cloud only when the answer names the question it was chosen for. Reload the page and answer again.", fixedCopy: true}
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

// workUntil is the spelling of the work samples' `until`: a Unix time in
// seconds. The route refuses one that is not after since.
var workUntil = regexp.MustCompile(`^[0-9]{1,12}$`)

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
