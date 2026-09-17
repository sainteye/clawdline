// Package cloudops is what this daemon does once a Cloud command has been
// decrypted: the closed vocabulary of operations a paired viewer may name,
// what each one is allowed to mean, and which of this machine's own routes
// answers it.
//
// It is deliberately apart from the transport. Nothing here opens a socket,
// holds a key, or knows what a relay is. A request arrives as plaintext with
// the envelope's identity fields beside it, and an answer leaves as the bytes
// that go inside the next envelope — so this package can be tested by writing
// down a request a browser really sends and reading what comes back, with no
// relay, no pairing and no clock.
//
// The seam downwards is LocalRouter, one method wide. Everything a Cloud
// request can ask for is something a paired browser on this machine's own
// network can already ask for, so the answer is the existing route's answer:
// the same permission checks, the same validation, the same refusal words.
// This package is a translation, not a second daemon.
//
// The authority is the Swift app's `Sources/CloudAppBridge.swift`, which is
// the producer the hosted console at app.clawdline.com was written against;
// where its vocabulary and this daemon's local routes disagree, the wire wins
// and the route mapping absorbs the difference (docs/cloud-wire.md §10).
package cloudops

import (
	"context"
	"strings"
)

// Class is the envelope class a request arrived under. The relay bills it and
// the envelope pins it, so it is a fact about the request rather than a
// preference: a read that arrives as `dispatch` is malformed however well its
// body reads.
type Class string

const (
	ClassCtl      Class = "ctl"
	ClassDispatch Class = "dispatch"
)

// MachineReplySession is the session a machine-scoped request names instead of
// a session id, and the channel its answer comes back on. It is a literal the
// hosted console also holds (`MACHINE_REPLY_SESSION` in `net/cloud-client.js`),
// so it is spelled here exactly once.
const MachineReplySession = "__clawdline_machine__"

// Command is one decrypted Cloud request, as a transport hands it over.
//
// Plaintext is the bytes inside `ct`, not a parsed object: the parse belongs
// here, because what counts as well formed is this vocabulary's question and
// a transport that parsed it first would have to decide that question twice.
type Command struct {
	// Channel is the envelope's `ch`, which for an inbound request is
	// `ctl/<machine>`. Empty skips the address check, for a caller that has
	// already made it.
	Channel string
	Class   Class
	// Sender is the viewer device id the envelope was signed by.
	Sender string
	// Sequence is the envelope's `seq`, carried into every refusal as `seq` so
	// a browser can tie a notice to the request it wrote.
	Sequence uint64
	// Plaintext is the decrypted `ct`.
	Plaintext []byte
}

// Answer is what the bridge decided: the bytes of one payload and the channel
// they belong on.
//
// A refusal is an answer too. That is the whole shape of this transport: a
// browser told `not_found` can say so, and a browser told nothing at all waits
// forever behind a skeleton.
type Answer struct {
	// Session names the channel — `t/<machine>/<session>` — that this payload
	// is published on. Empty means there is nowhere safe to publish it, and
	// the refusal is a local notice instead (see replyTo in the Swift bridge):
	// a malformed body cannot be trusted to name the waiter it would settle.
	Session string
	// Name is the payload's `read` field, which is how a viewer waiting for
	// one answer refuses to be settled by another.
	Name string
	// Status is the HTTP-shaped status the payload carries.
	Status int
	// Code is the refusal's code, empty on success. It is not part of the
	// payload — it is already inside it — but every caller logs it.
	Code string
	// Payload is the JSON object to seal. Empty when Session is empty.
	Payload []byte
}

// Published reports whether this answer has a channel to go out on.
func (a Answer) Published() bool { return a.Session != "" && len(a.Payload) > 0 }

// OK reports whether the operation succeeded.
func (a Answer) OK() bool { return a.Status >= 200 && a.Status < 300 }

// LocalRequest is one of this daemon's own routes, named the way its HTTP
// adapter names it. It is not an outbound HTTP call: the router below turns it
// into an in-process dispatch, because a daemon that answered its own requests
// over a socket would be paying for a second authentication of itself.
type LocalRequest struct {
	Method string
	Path   string
	Query  map[string]string
	Header map[string]string
	Body   []byte
}

// LocalResponse is what that route answered. ContentType matters for exactly
// two reads — a picture and a document answer with bytes and say what they are
// in a header, and an envelope has no headers.
type LocalResponse struct {
	Status      int
	Body        []byte
	ContentType string
}

// LocalRouter is the whole seam between this package and the daemon it speaks
// for. One method: the tests supply a fake, and the wiring supplies the real
// one (internal/transport/cloud).
type LocalRouter interface {
	Do(ctx context.Context, req LocalRequest) (LocalResponse, error)
}

// Authority is what this machine can currently say about one sender, re-read
// at the point of no return rather than trusted from admission — the Swift
// app's CloudCommandEffectAuthorization.
//
// Every field is a permission that can be withdrawn between the moment a
// request is admitted and the moment it would have an effect: a device removed
// from the roster, a switch turned off, a clock that stopped being trusted.
type Authority struct {
	// ClockReady is the guarded clock's admission (internal/domain/cloud's
	// EpochGuard). A machine still confirming the time refuses rather than
	// acting on a deadline it cannot evaluate.
	ClockReady bool
	// ClockReason and ClockClearsInMS travel on the refusal's detail, which is
	// whitelisted per code (§11.6): only these two for this one.
	ClockReason     string
	ClockClearsInMS int64
	// RosterReadable separates "this device is not paired" from "the list of
	// paired devices could not be read". An unreadable roster is never an
	// empty one.
	RosterReadable bool
	// RosterAllowsSender is whether this sender is still on it.
	RosterAllowsSender bool
	// WriteGateAllows is the machine's own switch: may a remote device cause
	// an effect here at all. False by default, everywhere.
	WriteGateAllows bool
}

// Bridge answers Cloud requests out of this machine's local routes.
//
// The zero value refuses every command and answers no read: a Bridge with no
// router has nothing to ask, and a Bridge with no gate has not been told that
// anybody may write. Both defaults are the safe ones on purpose.
type Bridge struct {
	// MachineID is this machine's Cloud id. A request addressed to another
	// machine's channel is refused rather than answered, because the viewer
	// listens on the channel it addressed and an answer on ours reaches
	// nobody.
	MachineID string
	// Router is this daemon's own routes. Nil answers every routed operation
	// with `router_unavailable` rather than pretending it succeeded.
	Router LocalRouter
	// AllowCommands is the machine's remote-write switch, read per request
	// because a person may turn it off while one is in flight. Nil is off.
	AllowCommands func() bool
	// Authority is the roster and the clock, re-read at the point of no
	// return. Nil trusts the transport's own admission for the roster and the
	// clock — which is what the Swift app's default does — and still consults
	// AllowCommands for the write gate.
	Authority func(ctx context.Context, sender string, requiresWriteGate bool) Authority
}

// Vocabulary is every operation word this bridge knows, sorted. It is what a
// machine descriptor's `commands` array should carry, because the hosted
// console asks a machine only for words that list says it has
// (`_machineImplements` in `net/cloud-client.js`).
func Vocabulary() []string { return opNames(func(o op) bool { return true }) }

// Implemented is the subset with a local capability behind it on this daemon.
// A word outside this list is answered `unknown_command`, which is the code
// the hosted console learns from: it stops asking this machine for that word
// (`machineLacks` in `net/cloud-client.js`).
func Implemented() []string { return opNames(func(o op) bool { return o.route != nil }) }

// Knows reports whether the word is in the vocabulary at all.
func Knows(name string) bool { _, ok := catalog[name]; return ok }

// Handle answers one decrypted request.
//
// The order is the Swift bridge's `consume`, and each step is a different
// question: is this addressed to us, is it a word we know, may this sender
// cause an effect, does the body mean anything, and only then — what does this
// machine say. A step that refuses names its own code; none of them is silent.
func (b Bridge) Handle(ctx context.Context, cmd Command) Answer {
	parsed, parseErr := decodeBody(cmd.Plaintext)
	word, _ := parsed.str("type")

	// Before anything about the request: is it even ours. The viewer listens
	// on the channel it addressed, which is not one this machine publishes, so
	// an answer of ours would be read by nobody. The notice is the reply.
	if b.MachineID != "" && cmd.Channel != "ctl/"+ChannelSegment(b.MachineID) {
		return b.notice(cmd, Refusal{Status: 409, Code: "wrong_machine",
			Message: "This Cloud request addresses another Mac."})
	}
	if parseErr != nil || word == "" {
		return b.refuse(cmd, parsed, "", Refusal{Status: 400, Code: "malformed_command",
			Message: "This Cloud command is malformed."})
	}
	o, known := catalog[word]
	if !known {
		return b.refuse(cmd, parsed, word, Refusal{Status: 400, Code: "unknown_command",
			Message: "This Mac does not know that Cloud command."})
	}
	if o.read {
		return b.serveRead(ctx, cmd, parsed, o)
	}
	return b.serveCommand(ctx, cmd, parsed, o)
}

// serveRead answers one of the effect-free words.
//
// A read is not behind the write switch, and that difference is the reason
// reads are a separate list: a command types into somebody's session, while a
// transcript read types into nothing. A paired device reads a transcript over
// this machine's own network with that switch off, and a viewer that can see
// every session row but not the messages inside one would be showing less than
// the same device sees over the tunnel, for no reason anybody chose.
func (b Bridge) serveRead(ctx context.Context, cmd Command, parsed body, o op) Answer {
	// A read rides the command channel and therefore its class, which is what
	// the relay bills and what the envelope pins. `dispatch` is a command
	// class and never a read.
	if cmd.Class != ClassCtl {
		return b.refuse(cmd, parsed, o.name, Refusal{Status: 400, Code: "malformed_read",
			Message: "This Cloud read is malformed."})
	}
	plan, ok := o.decode(parsed)
	if !ok {
		return b.refuse(cmd, parsed, o.name, Refusal{Status: 400, Code: "malformed_read",
			Message: "This Cloud read is malformed."})
	}
	if o.route == nil {
		// A word this vocabulary knows and this daemon cannot answer. The body
		// decoded, so unlike the Swift bridge's unknown-word branch this one
		// knows exactly which waiter is owed the news, and publishes it there
		// rather than announcing a notice the browser never hears. The code is
		// the Swift bridge's, because it is the code the hosted console learns
		// from: `machineLacks` stops it asking this machine for the word again.
		return b.publish(cmd, plan, Refusal{Status: 400, Code: "unknown_command",
			Message: "This Mac does not know that Cloud command."}, nil)
	}
	return b.route(ctx, cmd, plan, o)
}

// serveCommand answers one of the words that can change something.
//
// The gate comes before the body. A device that may not write is told so
// whether or not it spelled its request correctly, and a body it was never
// allowed to have acted on is not parsed for its benefit.
func (b Bridge) serveCommand(ctx context.Context, cmd Command, parsed body, o op) Answer {
	if !o.readLevel && !b.allowCommands() {
		return b.refuse(cmd, parsed, o.name, Refusal{Status: 403, Code: "cloud_commands_disabled",
			Message: "Cloud commands are disabled on this Mac."})
	}
	// A command rides the class its envelope was sealed under, and every word
	// but `dispatch` rides `ctl`. A body that reads perfectly under the wrong
	// class is malformed: the class is what the relay bills and what the
	// envelope pins, so it is a fact about the request rather than a detail.
	if !o.anyClass && cmd.Class != ClassCtl {
		return b.refuse(cmd, parsed, o.name, Refusal{Status: 400, Code: "malformed_command",
			Message: "This Cloud command is malformed."})
	}
	plan, ok := o.decode(parsed)
	if !ok {
		return b.refuse(cmd, parsed, o.name, Refusal{Status: 400, Code: "malformed_command",
			Message: "This Cloud command is malformed."})
	}
	// Admission may have waited. Re-read every revocable authority here, at
	// the point of no return, instead of reusing what was true when this
	// arrived.
	if refusal, denied := b.authorize(ctx, cmd.Sender, !o.readLevel); denied {
		return b.publish(cmd, plan, refusal, nil)
	}
	if o.refusal != nil {
		return b.publish(cmd, plan, *o.refusal, nil)
	}
	if o.route == nil {
		return b.publish(cmd, plan, Refusal{Status: 400, Code: "unknown_command",
			Message: "This Mac does not know that Cloud command."}, nil)
	}
	return b.route(ctx, cmd, plan, o)
}

// route asks this machine and turns its answer into a payload.
func (b Bridge) route(ctx context.Context, cmd Command, plan plan, o op) Answer {
	if b.Router == nil {
		return b.publish(cmd, plan, Refusal{Status: 503, Code: "router_unavailable",
			Message: "This Mac's own routes are not reachable from its Cloud bridge.",
			Layer:   layerRoute}, nil)
	}
	req := o.route(plan)
	if req.Header == nil {
		req.Header = map[string]string{}
	}
	// A retried request is not a second effect. The viewer's own request id is
	// the key when it named one, because that is the identity it will retry
	// under; without one, the envelope's sender and sequence are, which are
	// unique per viewer per request by the protocol's own replay rule.
	if _, taken := req.Header["Idempotency-Key"]; !taken && req.Method != "GET" {
		req.Header["Idempotency-Key"] = idempotencyKey(cmd, plan)
	}
	res, err := b.Router.Do(ctx, req)
	if err != nil {
		return b.publish(cmd, plan, Refusal{Status: 502, Code: "route_failed",
			Message: "This Mac could not answer that.", Layer: layerRoute}, nil)
	}
	return b.answer(cmd, plan, o, res)
}

// allowCommands reads the machine's write switch. Nil is off: a daemon that
// was never told anybody may write has not been told anybody may write.
func (b Bridge) allowCommands() bool {
	if b.AllowCommands == nil {
		return false
	}
	return b.AllowCommands()
}

// authorize is the Swift app's refreshAuthorizationRefusal, in its order: the
// clock, then whether the roster could be read at all, then whether it holds
// this sender, then the switch. Each is a different sentence because each
// sends the person somewhere different.
func (b Bridge) authorize(ctx context.Context, sender string, requiresWriteGate bool) (Refusal, bool) {
	a := Authority{ClockReady: true, RosterReadable: true, RosterAllowsSender: true,
		WriteGateAllows: !requiresWriteGate || b.allowCommands()}
	if b.Authority != nil {
		a = b.Authority(ctx, sender, requiresWriteGate)
	}
	switch {
	case !a.ClockReady:
		detail := map[string]any{}
		if a.ClockReason != "" {
			detail["reason"] = a.ClockReason
		}
		if a.ClockClearsInMS > 0 {
			detail["clears_in_ms"] = a.ClockClearsInMS
		}
		return Refusal{Status: 503, Code: "command_clock_uncertain",
			Message: "This Mac is still confirming the time; try again shortly.",
			Detail:  detail}, true
	case !a.RosterReadable:
		return Refusal{Status: 503, Code: "command_roster_unreadable",
			Message: "This Mac could not read its paired devices."}, true
	case !a.RosterAllowsSender:
		return Refusal{Status: 403, Code: "unknown_sender",
			Message: "This Mac does not recognise this device."}, true
	case !a.WriteGateAllows:
		return Refusal{Status: 403, Code: "cloud_commands_disabled",
			Message: "Cloud commands are disabled on this Mac."}, true
	}
	return Refusal{}, false
}

// idempotencyKey is what a retry of this request must collide with.
func idempotencyKey(cmd Command, plan plan) string {
	if plan.request != "" {
		return plan.request
	}
	var out strings.Builder
	out.WriteString("cloud:")
	out.WriteString(cmd.Sender)
	out.WriteString(":")
	out.WriteString(itoa(int64(cmd.Sequence)))
	return out.String()
}

// ChannelSegment is the hosted console's `channelSegment`: percent-encoding
// with the unreserved set JavaScript's encodeURIComponent leaves alone.
//
// It is used for two different things on purpose, exactly as the Swift bridge
// uses it: the segments of a relay channel, and the segments of a local route's
// path. A tmux pane is `%195`, and a path that carried that raw would be a
// different path.
func ChannelSegment(value string) string {
	const safe = "-_.!~*'()"
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			strings.IndexByte(safe, c) >= 0:
			out.WriteByte(c)
		default:
			const hex = "0123456789ABCDEF"
			out.WriteByte('%')
			out.WriteByte(hex[c>>4])
			out.WriteByte(hex[c&0x0f])
		}
	}
	return out.String()
}
