// Package cloud is this daemon's side of the relay wire: one WebSocket to
// `/v1/connect?role=machine`, the two-step challenge handshake behind it, and
// the machinery that keeps a line alive across a network that does not stay up.
//
// The domain package `internal/domain/cloud` owns everything that has a
// known answer — canonical JSON, the ten-field envelope, the signature string,
// the keys, the clock guard, the replay window. This package owns everything
// that has a *clock*: dialling, handshaking, backing off, spooling what could
// not be sent, and saying out loud what state the line is in.
//
// The shape is the Swift app's, deliberately. `Sources/CloudTransport.swift`
// (2,705 lines), `CloudOutboundSpool.swift` and `CloudCommandLedger.swift` are
// already deployed against the relay that is running today, and the places
// where they look over-careful are usually the places where they were taught
// something. Where this port differs, the difference is written down in
// `docs/cloud-wire.md` §15 with the reason; nothing here is a redesign.
//
// Three rules carried over intact, because each of them was a bug once:
//
//   - **A refusal is not a retry.** `forbidden` and the three `revoked`
//     spellings mean the account no longer accepts this machine, and so does a
//     401/403 at the upgrade or from the token endpoint; reconnecting with the
//     same credential is a loop that ends in a rate limit. An in-band
//     `unauthorized`, `token_expired` or `token_superseded` asks for a new
//     token, and everything else — `over_capacity`, `bad_gateway`, `internal`,
//     and `bad_request` too — is retried on the backoff ladder, as the Swift
//     transport does (`CloudTransport.swift:2238-2240`). `failure.go` names
//     each of them for the person reading the status.
//   - **A sequence number is spent when it is sent, not when it is
//     acknowledged.** Re-sending a spooled envelope after a reconnect re-sends
//     the *same* bytes with the same seq, because the relay's replay window
//     would otherwise see a second identity for one command. This is the
//     opposite of the ordinary "retry with a fresh id" instinct and it is the
//     whole reason a spooled envelope is sealed once and kept, not re-sealed.
//   - **A snapshot channel is coalesced, a command channel is not.** `s/`,
//     `t/` and `orch/` carry whole snapshots, never diffs (docs/cloud-wire.md
//     §2.2), so a newer one on the same channel replaces an older queued one
//     and nothing is lost. `ctl/`, `ctlr/` and `ho/` carry commands, where
//     dropping the older one loses an instruction.
//
// Nothing in this package connects to anything on its own. The line is off
// until `cloud_enabled` is true in this app's settings file *and* a relay URL
// and a device token exist; `docs/remote.md` design principle 3 is that this
// machine never reaches the user's account without the user saying so.
package cloud
