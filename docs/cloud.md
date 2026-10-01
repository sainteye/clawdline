# Clawdline Cloud from the Go daemon's side

Clawdline Cloud is an optional way to read and act on a machine from the hosted console, including a phone. The Go daemon owns sessions, tasks, commands and local authorization. It opens an outbound connection to the Cloud relay; the hosted React console is built from this repository's `web/console`. Cloud is off by default, and a Cloud failure does not stop the local daemon.

This page describes the **public repository's machine and browser code**. The account control plane, relay deployment, billing, retention and production operations belong to a private service repository and cannot be verified from this checkout. Its private `PROTOCOL.md` is the service-side contract; [cloud-wire.md](cloud-wire.md) records the public-side wire implementation and test evidence. Local tests are not proof of current production behavior.

## Components and trust boundary

| Component | Responsibility in this repository |
| --- | --- |
| [`internal/domain/cloud`](../internal/domain/cloud) | Canonical JSON, signed encrypted envelopes, keys, pairing primitives, replay protection and command ledger rules. Protocol vectors check the wire bytes. |
| [`internal/adapters/cloud`](../internal/adapters/cloud) and [`internal/adapters/cloudkeys`](../internal/adapters/cloudkeys) | Account API client, relay socket, roster and pinned-device records, outbound spool, identity and owner-only key files. The Go key store uses a `0700` directory and `0600` files, not the retired Swift app's Keychain. |
| [`internal/transport/cloud`](../internal/transport/cloud) | Opens the daemon's Cloud line, routes decrypted requests, manages pairing, and publishes machine and session snapshots. |
| [`internal/app/cloudops`](../internal/app/cloudops) | Defines the implemented request vocabulary and dispatches authorized requests through the daemon's local route handler and gate. |
| [`web/console/src/cloud`](../web/console/src/cloud) | Pairs and verifies a browser, reads decrypted snapshots, and sends supported requests through the relay. |

An envelope carries routing metadata the relay needs, while its payload is encrypted with the account content key and signed with a device key. The machine checks the sender's signature and local trust or revocation state before handling a request. The browser must hold the pairing handover's keys to verify and decrypt machine publications. This is the encryption boundary implemented here; the private service's data handling and deployed configuration need separate evidence. See [cloud-wire.md](cloud-wire.md) for exact envelope fields, channel rules and cryptographic construction.

## Starting the line and pairing

`clawdline cloud login` registers this machine and waits for account approval. `clawdline cloud on` sets `cloud_enabled`; restart `serve` to open the line. The daemon checks the switch before loading Cloud identity or keys. `clawdline cloud status` and local `GET /v1/cloud/status` distinguish off, unconfigured, connected and reconnecting states and expose typed failure information and counters. Default endpoints are defined in [`settings.go`](../internal/adapters/cloud/settings.go); configured endpoints can differ.

The machine can create a one-time pairing link with `clawdline cloud pair`, or accept a browser-originated offer with `clawdline cloud pair --offer <code>`. Pairing transfers the content key and trust information through the handover protocol. Compare the fingerprints shown at both ends. `clawdline cloud devices` lists viewers known to the machine, and `clawdline cloud revoke <device-id>` revokes one locally. Local pairing and revoke routes require this machine's own credential. The [remote access guide](user/remote-access.md) gives setup steps.

The account roster and the machine's local pinned-device record are distinct evidence. A locally revoked device is refused even if it appears in the account roster; a roster read failure is not treated as an empty roster. Browser pairing is also distinct from account sign-in: a browser listed under an account does not thereby have this machine's content key.

## Publications, reads and commands

The publisher sends a machine descriptor on `orch/<machine>`, one complete row per session on `s/<machine>/<session>`, and an inventory marker after the rows. It polls the daemon's local routes, skips unchanged session content, and periodically restates values so an idle machine does not appear stale. A `sessions.snapshot` request can ask the machine to restate rows after a reconnect. The publisher preserves previously known rows when a partial local scan cannot prove they disappeared. The descriptor includes the supported command words. See [`publish.go`](../internal/transport/cloud/publish.go) for current timing, freshness and inventory rules.

An incoming request arrives on `ctl/<machine>`. The transport verifies and decrypts it, then [`Service`](../internal/transport/cloud/cloud.go) hands it to `cloudops`. The answer is sealed for `t/<machine>/<session>`, including the reserved machine reply session for machine-wide reads. `cloudops.Implemented()` is the machine's current operation list; the hosted console's [`carry.ts`](../web/console/src/cloud/carry.ts) maps routes to that vocabulary. Unsupported or machine-only routes, including terminal control, are not general Cloud capabilities.

Reading and acting have different gates. `cloud_commands` is checked for each effectful request, and the account roster must grant the relevant capability, such as `send_prompt` or `start_session`. `clawdline cloud commands on|off` changes the machine switch without restarting the daemon. A Cloud command uses the same in-process HTTP handler and authorization gate as a direct request.

The connection backs off and reconnects. An outbound spool owns publish order and capacity, while inbound queues and per-channel answer reserves produce typed busy or refusal answers where possible. An accepted relay write is not the same as an executed command or an observed answer. Logs and status counters separate published, acknowledged, answered, refused and undeliverable work. Protocol and capacity details are in [cloud-wire.md](cloud-wire.md), [limits.md](limits.md), and the transport code; this overview does not promise delivery after every network or process failure.

## Hosted console and operations

The hosted and daemon consoles share source but require different builds. The hosted build must set `VITE_HOSTED_CONSOLE` so its entry point selects `CloudGate`; an ordinary local build selects `DoorGate`. [Deploying the hosted console](hosted-console.md) documents the build, production ancestry check, `BUILD.json` stamp, served-bundle `CloudGate` check and rollback procedure. A build stamp alone does not show that the served bundle is the Cloud one. This page makes no claim that the currently deployed bundle or private service matches this checkout.

For a machine that appears offline, inspect `clawdline cloud status` or the local status route first: the switch, identity, connection state, last error and publication counters narrow down which side has evidence. Then inspect the hosted build using [hosted-console.md](hosted-console.md). The status route and logs expose this machine's observations; they do not prove that a particular browser decrypted or displayed a frame. [cloud-cutover.md](cloud-cutover.md) records local preflight and stand-in tests, not a fresh production validation.

## Limits of this overview

- Public code and tests establish local behavior and wire compatibility against fixtures. They do not establish the private service's current implementation, capacity, retention, pricing or live availability.
- The Go key store is owner-only files, not a platform keystore. Protect and back up the daemon's state directory accordingly; this repository does not claim hardware-backed storage.
- Pairing grants access to encrypted content. Revoking a device stops future requests according to the machine's checks; it cannot erase content that device already decrypted or copied.
- The hosted console only carries operations present in the machine's advertised vocabulary. Version gaps can leave a request unsupported or waiting for its timeout; check both builds before assuming a feature is available.

For the wider system layout, see [architecture.md](architecture.md). For user-facing setup and alternatives, see [remote access](user/remote-access.md). Historical Swift implementation details remain in dated records and the archive, not in this current overview.
