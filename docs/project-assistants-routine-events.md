# Bot routine events and webhook input

Status: callable modules only. No route, tool, schema or stored routine accepts an event or webhook
trigger yet; `RoutineTrigger.Validate` still answers `pa_unsupported` for both. This page records
what the two modules do so the integration can wire them without re-deriving the pinned behavior.

The pinned source is Codync `host/src/routines/schedule.rs` (`Trigger::Event`, `validate`,
`matches`; webhook and event triggers have no next time) and `host/src/routines/hooks.rs`
(`authorized`, `delivery_id`, `event`, `github`).

## Event specs: `internal/domain/projectassistant/routine_events.go`

`RoutineEventSpec{Source, Event, Filters}` is the body of the upstream tagged trigger
`{"type":"event","source":…,"event":…,"filters":{…}}`. It is not a second schedule format; the
integration adds it as the `event` variant of the existing trigger.

- `Validate(RoutineEventLimits)` refuses an unknown source (`pa_unsupported`,
  `routine_event_source`), an empty or over-bound event name, too many filters, an empty or
  over-bound key, an over-bound value, and any non-UTF-8 string (`pa_invalid`). The bounds are the
  caller's: `RoutineEventLimits` has no default and refuses zero or negative values, except that
  zero filters is a valid bound. Upstream uses 200 / 20 / 200 / 4000 bytes.
- `RoutineEventSources()` returns the eight pinned sources. A source is a label the sender
  supplies; a generic webhook may claim any of them, so it never grants anything.
- `Matches(RoutineEvent)` is upstream `matches`: `source` equal, `event` equal unless the spec
  says `*`, and every filter. A filter compares one top-level field, which must be a JSON string
  equal to the value; `messageContains` requires a string `text` containing the value, and an
  empty `messageContains` matches any string `text`. Numbers, booleans, objects, arrays and null
  never match, and nested fields are not reachable.

## Webhook input: `internal/adapters/projectassistantwebhook`

`Receive(secret, http.Header, body, Limits)` checks one delivery in upstream order and returns an
`Input{DeliveryID, Ignored, Event}`:

1. **Bound.** A body over `Limits.BodyBytes` is `webhook_too_large`, never truncated. `ReadBody`
   reads at most one byte past the bound, so an oversize request is refused without being held.
2. **Authentication** over the original bytes, before any parsing. `Authorization: Bearer <secret>`
   is compared as SHA-256 digests in constant time; `X-Hub-Signature-256: sha256=<hex>` is
   HMAC-SHA256 of the exact body, compared with `hmac.Equal`. As upstream, a wrong bearer still
   lets a valid signature through. Every failure is `webhook_unauthorized`: an empty, short or
   non-printable secret, no proof, a repeated proof header, a different algorithm prefix, a
   malformed digest or a wrong one. The `Reason` tells these apart for diagnostics only; it never
   contains the secret, the proof or the body, and the sender should see only the code.
3. **Delivery id**: the first present of `X-Delivery-Id`, `X-GitHub-Delivery`, `Idempotency-Key`.
   It must be one non-empty value of printable ASCII within `Limits.DeliveryIDBytes`, or the
   delivery is `webhook_invalid`. It identifies the delivery, not the body and not a run.
4. **Decoding.** An empty or whitespace body is `{}`. Anything else must be one valid UTF-8 JSON
   object, decoded with numbers kept exact. With `X-GitHub-Event` the object becomes
   `{source:"github", event:"<name>.<action>", repo, sender, text, url, payload}` with upstream's
   field precedence, and an authenticated `ping` is `Ignored` before its body is parsed.

Malformed or unsupported data (`webhook_invalid`, `webhook_too_large`) is kept apart from
authentication (`webhook_unauthorized`); an unauthenticated sender learns nothing about its body
or delivery id. `webhook_limits_invalid` is a caller bug.

### Deliberate differences from upstream

- A non-object JSON body or a non-JSON body is refused. Upstream wraps them as `{"payload": …}`
  and `{"text": …}`; the task frozen for this increment requires refusal.
- Invalid UTF-8 is refused instead of being replaced.
- An empty, non-printable or over-bound `X-GitHub-Event`, and any repeated proof, event or
  delivery header, is refused instead of being taken at its first or only value.
- A signature digest is strict hexadecimal. Rust's `from_str_radix` also accepts a leading `+`
  inside a byte pair; this module does not.
- The secret must meet a caller-supplied minimum length and be printable ASCII. Upstream keys are
  64 hexadecimal characters, which satisfies any sensible minimum.

### What this module does not do

It makes no network or provider call, stores no credential, and does not queue, deduplicate,
admit, retry or execute anything. Durable delivery dedupe, pausing, busy handling and the run
itself belong to the routine engine when it is wired; nothing here proves them.
