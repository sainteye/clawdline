package cloudops

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"unicode/utf8"

	"github.com/sainteye/clawdline-go/internal/domain/capacity"
)

// The layers a refusal can be decided at, as the wire spells them
// (CloudRefusalLayer). A browser shows them, so they are not free-form.
const (
	layerPreflight = "mac_preflight"
	layerRoute     = "mac_route"
	layerTransport = "mac_transport"
	// layerReply is the one decided after this machine already did the thing:
	// the answer exists and could not be put on the wire. A browser must not
	// read it as "it did not happen" (`net/cloud-failure.js`'s MAC_LAYERS).
	layerReply = "mac_reply"
)

// Refusal is a typed no, in the shape the whole Cloud path already has: a code
// a browser may branch on, a sentence a person may read, and the layer that
// decided it.
type Refusal struct {
	Status  int
	Code    string
	Message string
	// Detail is filtered by code before it reaches a viewer (§11.6), so a path
	// or a title cannot cross inside it by accident.
	Detail map[string]any
	// Layer defaults to mac_preflight: a refusal decided before this machine's
	// own routes were asked.
	Layer string
}

// detailWhitelist is §11.6: the only `detail` fields each code may carry.
// Anything else is dropped here rather than at the reader, because the reader
// is on somebody's phone and this is the last place that is us.
var detailWhitelist = map[string]map[string]bool{
	"command_clock_uncertain": {"reason": true, "clears_in_ms": true},
	"key_id_mismatch":         {"key_id": true, "expected_key_id": true},
	"replay":                  {"highest_seq": true},
	"cloud_ingress_busy":      {"retry_after": true, "lane": true, "limit": true},
	"cloud_read_busy":         {"retry_after": true, "lane": true, "limit": true},
	// The answer was built and the channel it belongs on had no room. The
	// hosted console already draws both of these (`core/failure-text.js`);
	// only the read carries detail, because the copied client's own
	// whitelist has no entry for the command one and would drop it.
	"command_answer_undeliverable": {},
	"no_whisper":                   {"reason": true},
	"terminal_closed":              {"app": true},
	"would_lose_work":              {"lost": true},
	// Two of this daemon's own, named here so the reason a picture or a
	// document stayed home can be shown rather than guessed at.
	"image_too_large_for_cloud": {"byte_count": true, "limit_bytes": true},
	"document_too_large":        {"byte_count": true, "limit_bytes": true},
}

// errorObject is the `error` a payload carries: the code and the sentence,
// the layer and the sequence, and whatever this code may show (§11.6) —
// whether the route put it in `detail` or beside the code.
//
// **Nothing else a route wrote crosses.** A local refusal is written for a
// reader on this machine, and a field put beside its code — a route, a
// directory, a blocked close's mover with its task's title — would otherwise
// be sealed to every paired phone. The whitelist is the same one `detail`
// has, applied to the top level too, and a blocked close's reasons cross as
// what blocks it and nothing more (closeReasons).
func errorObject(base map[string]any, code, layer string, sequence uint64, detail map[string]any) map[string]any {
	allowed := detailWhitelist[code]
	out := map[string]any{}
	for k, v := range base {
		switch {
		case k == "message":
			out[k] = v
		case k == "reasons" && code == "close_blocked":
			out[k] = closeReasons(v)
		case allowed[k]:
			out[k] = v
		}
	}
	out["code"] = code
	if _, ok := out["message"]; !ok {
		out["message"] = "This command could not be completed."
	}
	if layer == "" {
		layer = layerPreflight
	}
	out["layer"] = layer
	out["seq"] = sequence
	filtered := map[string]any{}
	for k, v := range detail {
		if allowed[k] {
			filtered[k] = v
		}
	}
	if len(filtered) > 0 {
		out["detail"] = filtered
	}
	return out
}

// closeReasons is a blocked close's reasons as they may cross: what kind of
// thing is in the way and which one, by id. The mover's own record — a task's
// title, a session's label — stays on this machine.
func closeReasons(value any) []any {
	raw, _ := value.([]any)
	out := []any{}
	for _, item := range raw {
		reason, ok := item.(map[string]any)
		if !ok {
			continue
		}
		kept := map[string]any{}
		for _, key := range []string{"kind", "code", "subject_id", "subject_kind"} {
			if v, ok := reason[key].(string); ok {
				kept[key] = v
			}
		}
		out = append(out, kept)
	}
	return out
}

// payload is `{"read":…,"status":…}` with either a body or an error, which is
// what every answer on this transport is. The key is `read` for a command's
// answer too: it is the name of the channel the waiter is waiting on, and the
// hosted console reads exactly this field.
func payload(name string, status int, body json.RawMessage, failure map[string]any) []byte {
	out := map[string]any{"read": name, "status": status}
	if failure != nil {
		out["error"] = failure
	} else if len(body) > 0 {
		out["body"] = body
	}
	bytes, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return bytes
}

// settle is the one place an Answer is built: either a payload on the channel
// the request named, or the same outcome with nowhere to send it.
//
// Nowhere to send it is not silence and not a failure. It is the case where
// the body named no waiter this machine may safely settle — an older page's
// send, a malformed read with no request id — and the caller records it.
func (b Bridge) settle(p plan, status int, code string, ok json.RawMessage, failure map[string]any) Answer {
	if p.name == "" || p.session == "" {
		return Answer{Status: status, Code: code, Subject: p.session}
	}
	return Answer{Session: p.session, Name: p.name, Status: status, Code: code, Subject: p.session,
		Payload: payload(p.name, status, ok, failure)}
}

// notice is a refusal decided with no plan at all.
func (b Bridge) notice(_ Command, r Refusal) Answer {
	return Answer{Status: r.Status, Code: r.Code}
}

// refuse answers before any body was decoded, on the channel the request's own
// identity fields safely name — and nowhere at all when they name none.
func (b Bridge) refuse(cmd Command, parsed body, word string, r Refusal) Answer {
	session, name, ok := refusalReply(parsed, word, cmd.Class)
	if !ok {
		return b.notice(cmd, r)
	}
	return b.settle(plan{session: session, name: name}, r.Status, r.Code, nil,
		errorObject(map[string]any{"message": r.Message}, r.Code, r.Layer, cmd.Sequence, r.Detail))
}

// publish answers a decoded request: a refusal this bridge decided, or a body
// it built itself.
func (b Bridge) publish(cmd Command, p plan, r Refusal, ok json.RawMessage) Answer {
	if r.Code == "" {
		return b.settle(p, 200, "", ok, nil)
	}
	return b.settle(p, r.Status, r.Code, nil,
		errorObject(map[string]any{"message": r.Message}, r.Code, r.Layer, cmd.Sequence, r.Detail))
}

// answer turns one local route's response into a payload.
//
// A refusal is forwarded rather than translated: the typed `{"error":{"code"}}`
// this machine already answers a paired browser with is what crosses, which is
// what lets the console branch on exactly the codes it branches on over the
// tunnel. `git-panel.js` shows a different sentence for `not_a_repo` than for
// anything else, and it can only do that if `not_a_repo` survives the trip.
func (b Bridge) answer(cmd Command, p plan, o op, res LocalResponse) Answer {
	if res.Status >= 200 && res.Status < 300 {
		if o.shape != nil {
			body, refusal := o.shape(p, res)
			if refusal.Code != "" {
				return b.publish(cmd, p, refusal, nil)
			}
			return b.settle(p, res.Status, "", body, nil)
		}
		var parsed map[string]any
		if err := json.Unmarshal(res.Body, &parsed); err != nil || parsed == nil {
			// A 2xx whose body is not an object is not an answer this
			// transport can carry, and calling it one would put a browser in
			// front of an empty card with nothing to say about it.
			return b.publish(cmd, p, Refusal{Status: 502, Code: "read_failed",
				Message: "This read could not be answered.", Layer: layerRoute}, nil)
		}
		return b.settle(p, res.Status, "", res.Body, nil)
	}
	base := refusalOf(res.Body)
	code, _ := base["code"].(string)
	if code == "" {
		code = "command_failed"
	}
	detail, _ := base["detail"].(map[string]any)
	return b.settle(p, res.Status, code, nil, errorObject(base, code, layerRoute, cmd.Sequence, detail))
}

// refusalOf reads this daemon's refusal, which has two spellings.
//
// The gate and the documents route answer `{"error":{"code","message"}}`, which
// is the Swift app's shape and the one the hosted console reads. Most other
// routes answer `{"error":"<code>","detail":"<sentence>"}` — flat, with the
// code where the object would be. Both are this machine's own word for what
// happened, and a viewer that got `command_failed` for a `session_unknown`
// would be shown a shrug where there was an explanation.
//
// **Whatever else the route put beside it comes too.** A blocked close carries
// `reasons`, and a card that can only say "refused" sends a person to the
// terminal to find out why — which is the round trip this daemon exists to
// remove.
func refusalOf(answer []byte) map[string]any {
	fallback := map[string]any{"code": "command_failed",
		"message": "This command could not be completed."}
	var parsed map[string]any
	if err := json.Unmarshal(answer, &parsed); err != nil || parsed == nil {
		return fallback
	}
	if nested, ok := parsed["error"].(map[string]any); ok {
		return nested
	}
	code, ok := parsed["error"].(string)
	if !ok || code == "" {
		return fallback
	}
	base := map[string]any{"code": code}
	if message, ok := parsed["detail"].(string); ok && message != "" {
		base["message"] = message
	}
	for key, value := range parsed {
		if key == "error" || key == "detail" {
			continue
		}
		base[key] = value
	}
	return base
}

// MARK: the three answers that are not JSON on the wire

// cloudEnvelopeCiphertextLimit is the relay's per-account cap, which every tier
// shares (`max_envelope_bytes`).
const cloudEnvelopeCiphertextLimit = 16 << 20

// aeadTagBytes and imageAnswerOverhead are the two costs between a picture's
// bytes and the envelope's ceiling: AES-GCM's tag, and the JSON around the
// base64 — the two answer fields, the id, the media type, the byte count and
// the quoting, measured at 176 bytes with every field at its maximum, with a
// kibibyte allowed as slack.
const (
	aeadTagBytes        = 16
	imageAnswerOverhead = 1 << 10
)

// imageMaxEncodedBytes is the largest PNG this transport carries in one
// answer. **Derived, not chosen**, from two ceilings, and the smaller wins:
//
//  1. the relay caps one envelope's ciphertext, and the tag and the JSON
//     wrapper come off that;
//  2. this machine's own spool caps what one wire channel may have owed to
//     it (`cloud.spool_channel_bytes`), measured on the payload, and the JSON
//     wrapper comes off that too.
//
// Base64 costs four bytes for every three and the budget is rounded down to a
// multiple of four, so the encoded length is exact rather than approximately
// right.
//
// **The second ceiling was missing and it is the one that binds.** The relay
// allows 16 MiB and a channel holds 4, so every picture between them passed
// this door and died at the spool — where, until 2026-09-21, it died silently.
// A door that admits what the next room refuses is not a door; the byte count
// a person is shown here is now one this machine can actually carry.
func imageMaxEncodedBytes() int {
	budget := cloudEnvelopeCiphertextLimit - aeadTagBytes - imageAnswerOverhead
	if channel := int(capacity.Default(capacity.CloudSpoolChannelBytes)) - imageAnswerOverhead; channel < budget {
		budget = channel
	}
	return (budget / 4) * 3
}

// documentMaximumBytes is internal/adapters/documents' MaximumBytes, which is
// also the hosted page's DOCUMENT_MAX_BYTES: an answer larger than this is a
// byte count nobody can read.
const documentMaximumBytes = 2 * 1024 * 1024

// documentsMaximumListed is that package's MaximumListed.
const documentsMaximumListed = 200

// shapeImage is the picture itself, base64 in a payload field, or the sentence
// saying why it stayed home.
//
// **The bytes are the answer and there is nothing shorter to send.** A URL
// cannot be: this machine is not reachable from the console, which is the entire
// reason a relay exists. So the only question left is how large a picture may
// be, and that is the relay's per-envelope cap arithmetic and nothing else.
func shapeImage(p plan, res LocalResponse) (json.RawMessage, Refusal) {
	if res.ContentType != "image/png" {
		return nil, Refusal{Status: 415, Code: "image_media_type_unsupported",
			Message: "That artifact is not a PNG and does not cross this connection.", Layer: layerRoute}
	}
	if len(res.Body) == 0 {
		return nil, Refusal{Status: 502, Code: "image_empty",
			Message: "That image arrived with no bytes in it.", Layer: layerRoute}
	}
	if limit := imageMaxEncodedBytes(); len(res.Body) > limit {
		return nil, Refusal{Status: 413, Code: "image_too_large_for_cloud",
			Message: "That image is larger than one answer on this connection can carry.", Layer: layerRoute,
			Detail: map[string]any{"byte_count": len(res.Body), "limit_bytes": limit}}
	}
	return mustJSON(map[string]any{
		"id": p.id, "media_type": res.ContentType, "byte_count": len(res.Body),
		"data": base64.StdEncoding.EncodeToString(res.Body),
	}), Refusal{}
}

// shapeDocument puts only inert UTF-8 text inside the encrypted answer, with a
// count the browser rechecks.
func shapeDocument(p plan, res LocalResponse) (json.RawMessage, Refusal) {
	switch res.ContentType {
	case "text/markdown; charset=utf-8", "text/plain; charset=utf-8":
	default:
		return nil, Refusal{Status: 415, Code: "document_media_type_unsupported",
			Message: "That file is not an inert Markdown or text document.", Layer: layerRoute}
	}
	if len(res.Body) > documentMaximumBytes {
		return nil, Refusal{Status: 413, Code: "document_too_large",
			Message: "That document is too large to carry in one encrypted answer.", Layer: layerRoute,
			Detail: map[string]any{"byte_count": len(res.Body), "limit_bytes": documentMaximumBytes}}
	}
	if !utf8.Valid(res.Body) {
		return nil, Refusal{Status: 415, Code: "document_not_utf8",
			Message: "That document is not valid UTF-8 text.", Layer: layerRoute}
	}
	out := map[string]any{
		"scope": p.scope, "path": p.path, "media_type": res.ContentType,
		"byte_count": len(res.Body), "data": base64.StdEncoding.EncodeToString(res.Body),
	}
	if p.scope == "task" {
		out["task"] = p.task
	}
	return mustJSON(out), Refusal{}
}

// shapeDocuments strips this machine's own address and the duplicate label
// before the listing enters an envelope. The relative path stays, because it is
// the requested identity; no resolved root and no filesystem path is in the
// route's answer or in what is built here.
func shapeDocuments(_ plan, res LocalResponse) (json.RawMessage, Refusal) {
	invalid := func(message string) Refusal {
		return Refusal{Status: 502, Code: "document_listing_invalid", Message: message, Layer: layerRoute}
	}
	var listing map[string]json.RawMessage
	if err := json.Unmarshal(res.Body, &listing); err != nil {
		return nil, invalid("The machine returned an invalid document listing.")
	}
	raw, only := listing["documents"]
	if !only || len(listing) != 1 {
		return nil, invalid("The machine returned an invalid document listing.")
	}
	var source []map[string]any
	if err := json.Unmarshal(raw, &source); err != nil || len(source) > documentsMaximumListed {
		return nil, invalid("The machine returned an invalid document listing.")
	}
	rows := make([]map[string]any, 0, len(source))
	for _, row := range source {
		scope, _ := row["source"].(string)
		path, pathOK := documentPath(row["path"])
		label, _ := row["label"].(string)
		bytes, bytesOK := jsonInt(row["bytes"])
		modified, modifiedOK := row["modified"].(float64)
		url, urlOK := row["url"].(string)
		_ = url
		if !pathOK || label != path || !bytesOK || bytes < 0 || bytes > documentMaximumBytes ||
			!modifiedOK || !urlOK {
			return nil, invalid("The machine returned invalid document metadata.")
		}
		switch scope {
		case "project":
			if len(row) != 6 {
				return nil, invalid("The machine returned unexpected document metadata.")
			}
			rows = append(rows, map[string]any{
				"scope": "project", "path": path, "bytes": bytes, "modified": modified})
		case "task":
			task, taskOK := row["task"].(map[string]any)
			if len(row) != 7 || !taskOK || len(task) != 2 {
				return nil, invalid("The machine returned invalid task document metadata.")
			}
			id, idOK := task["id"].(string)
			title, titleOK := task["title"].(string)
			if !idOK || !isTaskID(id) || !titleOK || len(title) > 300 {
				return nil, invalid("The machine returned invalid task document metadata.")
			}
			rows = append(rows, map[string]any{
				"scope": "task", "task": id, "title": title, "path": path,
				"bytes": bytes, "modified": modified})
		default:
			return nil, invalid("The machine returned an unknown document scope.")
		}
	}
	return mustJSON(map[string]any{"documents": rows}), Refusal{}
}

func jsonInt(value any) (int64, bool) {
	switch v := value.(type) {
	case float64:
		if v != float64(int64(v)) {
			return 0, false
		}
		return int64(v), true
	case json.Number:
		n, err := v.Int64()
		return n, err == nil
	}
	return 0, false
}

// mustJSON is a payload body built here rather than forwarded. encoding/json
// sorts a map's keys, so two answers to the same read differ only where their
// content differs.
func mustJSON(value map[string]any) json.RawMessage {
	out, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return out
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// Busy is the answer to a request this machine did not take because the queue
// in front of the bridge was full (limits N20): the Swift bridge's
// `consumeInboundRefusal` for a count cap, and the code the hosted console
// already draws as "this machine is busy" (`cloud_ingress_busy`, 429, retry after a
// second). limit is the queue's depth, which the detail carries.
//
// It decides where the refusal goes and nothing else: no route is asked and no
// body is acted on. A read is answered where its answer would have gone; a
// command where a refusal of it always goes, and not at all when the write
// switch is off, because telling a device to retry what it may never send is
// the wrong sentence — the Swift bridge checks the switch first for the same
// reason. A body that names no safe waiter is a notice, as every other refusal
// of such a body is.
// Undeliverable is the answer to a request this machine did answer and could
// not put on the wire, because the channel that answer belongs on is full
// (limits N22). limit is that channel's byte cap, which the detail carries.
//
// **It is two different sentences, and the difference is whether anything
// happened.** A read has no effect: nothing was done, the answer definitely
// did not leave, and retrying once the channel drains works — so it is
// `cloud_read_busy`, which the hosted console already draws as "this Mac is
// busy" and already treats as retryable. A command's effect has already
// happened and only its receipt was lost; telling that sender to retry would
// run it twice, so it is `command_answer_undeliverable` at layer `mac_reply`,
// which the same console reads as "the reply was lost" and refuses to retry
// (`net/cloud-failure.js`'s RETRYABLE_CODES).
//
// It is built from the request rather than from the answer for the same
// reason Busy is: what may be published, and where, is a question about the
// request's own identity fields, and an answer that could not be sent is not
// evidence about them.
func (b Bridge) Undeliverable(cmd Command, limit int) Answer {
	parsed, parseErr := decodeBody(cmd.Plaintext)
	word, _ := parsed.str("type")
	if b.MachineID != "" && cmd.Channel != "ctl/"+ChannelSegment(b.MachineID) {
		return b.notice(cmd, Refusal{Status: 409, Code: "wrong_machine",
			Message: "This Cloud request addresses another Mac."})
	}
	full := Refusal{Status: 429, Code: "cloud_read_busy",
		Message: "This Mac answered, and the channel that answer goes on is full; try again shortly.",
		Detail:  map[string]any{"lane": "egress", "limit": limit, "retry_after": 5},
		Layer:   layerTransport}
	if parseErr != nil || word == "" {
		return b.notice(cmd, full)
	}
	o, known := catalog[word]
	if known && !o.read {
		// The effect happened. This is a lost receipt, not a refusal, and it
		// carries no detail because the reader would drop it anyway.
		lost := Refusal{Status: 503, Code: "command_answer_undeliverable",
			Message: "This Mac carried out the command and its reply could not be delivered.",
			Layer:   layerReply}
		return b.refuse(cmd, parsed, word, lost)
	}
	if known && o.read && cmd.Class == ClassCtl {
		if p, ok := o.decode(parsed); ok {
			return b.publish(cmd, p, full, nil)
		}
	}
	return b.refuse(cmd, parsed, word, full)
}

func (b Bridge) Busy(cmd Command, limit int) Answer {
	parsed, parseErr := decodeBody(cmd.Plaintext)
	word, _ := parsed.str("type")
	if b.MachineID != "" && cmd.Channel != "ctl/"+ChannelSegment(b.MachineID) {
		return b.notice(cmd, Refusal{Status: 409, Code: "wrong_machine",
			Message: "This Cloud request addresses another machine."})
	}
	busy := Refusal{Status: 429, Code: "cloud_ingress_busy",
		Message: "This request was not accepted because Cloud ingress is full; try again shortly.",
		Detail:  map[string]any{"lane": "ingress", "limit": limit, "retry_after": 1},
		Layer:   layerTransport}
	if parseErr != nil || word == "" {
		return b.notice(cmd, busy)
	}
	o, known := catalog[word]
	if known && o.read && cmd.Class == ClassCtl {
		if p, ok := o.decode(parsed); ok {
			return b.publish(cmd, p, busy, nil)
		}
	}
	if known && !o.read && !o.readLevel && !b.allowCommands() {
		return b.refuse(cmd, parsed, word, Refusal{Status: 403, Code: "cloud_commands_disabled",
			Message: "Cloud commands are disabled on this machine."})
	}
	return b.refuse(cmd, parsed, word, busy)
}
