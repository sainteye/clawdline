package cloudops

import (
	"bytes"
	"encoding/json"
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
		strings.ContainsRune(path, 0) || strings.Contains(path, `\`) || !printable(path) {
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
	case "md", "markdown", "txt", "html":
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
	// The selected execution remains pinned through the local route's final
	// check. Empty is the older unpinned read shape.
	executionGeneration string

	// The rest are what a hand-written decoder hands its route (start,
	// resume, dispatch, document, the peer words, push-subscribe, the
	// webhook binder), and the few values a generated decoder carries
	// `into` the plan for the answer side: a picture's id, an info read's
	// parts, a menu answer's key, its question and whether it names typed
	// words, a viewer event batch.
	id, scope, task, path         string
	videoOffset                   int64
	videoRevision                 string
	place, past, assistant, model string
	parts, key                    string
	// expect is a menu answer's question, session.MenuFingerprint's hex.
	expect string
	// persona is the catalog id a start or a resume names, or "" for none.
	persona string
	// namesTyped is whether a menu answer named the words an `enter` submits
	// (`typed`) at all; the words themselves travel in the local request.
	namesTyped bool
	document   []byte
	// local is the request a generated decoder built, which the op's route
	// hands on (routeLocal).
	local LocalRequest
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

var catalog = map[string]op{}

// register adds words to the catalog. A word the vocabulary table states the
// decoder of is given that generated decoder and its route here, and must
// not bring its own: one word, one decoder.
func register(ops ...op) {
	for _, o := range ops {
		if _, generated := generatedDecoders[o.name]; generated {
			if o.decode != nil || o.route != nil {
				panic("cloudops: " + o.name + " has a generated decoder and registers its own")
			}
			o = withGeneratedDecoder(o)
		} else if o.decode == nil {
			panic("cloudops: " + o.name + " has no decoder")
		}
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

// pinnedSessionPlan retains the legacy read shape while carrying the optional
// execution pin used by the read_transcript rail.
func pinnedSessionPlan(b body, name string) (plan, bool) {
	p, ok := sessionPlan(b, name)
	if !ok {
		return plan{}, false
	}
	if raw, named := b["expected_generation"]; named {
		generation, valid := raw.(string)
		if !valid || !executionGenerationValid(generation) {
			return plan{}, false
		}
		p.executionGeneration = generation
	}
	return p, true
}

func executionGenerationValid(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, r := range value {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
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
		if word != "document" && word != "peer-inbox" && session != MachineReplySession {
			return "", "", false
		}
		return session, "read:" + request, true
	}
	if !hasRequest {
		return "", "", false
	}
	switch word {
	case "native-terminal-input", "send", "answer", "key", "end", "archive-session", "focus", "interrupt", "smart-title", "title", "shell-kill":
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

// usageID is `usageID` in internal/transport/http/usage.go, spelled a second
// time because this package cannot import a transport: letters, digits and
// `-`, `_`, `.`, starting with a letter or digit, at most 200, and never `..`.
// A looser copy would ask the route something it refuses as `bad_request`; a
// stricter one would refuse on a phone an id the machine's own page reads.
var usageID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`)

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
