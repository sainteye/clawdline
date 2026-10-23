# Clawdline Cloud from the Go daemon's side

Clawdline Cloud is an optional way to read and act on a machine from the hosted console, including a phone. The Go daemon owns sessions, tasks, commands and local authorization. It opens an outbound connection to the Cloud relay; the hosted React console is built from this repository's `web/console`. Cloud is off by default, and a Cloud failure does not stop the local daemon.

This page describes the **public repository's machine and browser code**. The account control plane, relay deployment, billing, retention and production operations belong to a private service repository and cannot be verified from this checkout. Its private `PROTOCOL.md` is the service-side contract; [cloud-wire.md](cloud-wire.md) specifies the public-side wire format; [the dated implementation record](records/cloud-wire-implementation-2026-09.md) holds test evidence. Local tests are not proof of current production behavior.

## Components and trust boundary

| Component | Responsibility in this repository |
| --- | --- |
| [`internal/domain/cloud`](../internal/domain/cloud) | Canonical JSON, signed encrypted envelopes, keys, pairing primitives, replay protection and command ledger rules. Protocol vectors check the wire bytes. |
| [`internal/adapters/cloud`](../internal/adapters/cloud) and [`internal/adapters/cloudkeys`](../internal/adapters/cloudkeys) | Account API client, relay socket, roster and pinned-device records, outbound spool, identity and owner-only key files. The Go key store uses a `0700` directory and `0600` files, not the retired Swift app's Keychain. |
| [`internal/transport/cloud`](../internal/transport/cloud) | Opens the daemon's Cloud line, routes decrypted requests, manages pairing, and publishes machine and session snapshots. |
| [`internal/app/cloudops`](../internal/app/cloudops) | Defines the implemented request vocabulary and dispatches authorized requests through the daemon's local route handler and gate. |
| [`web/console/src/cloud`](../web/console/src/cloud) | Pairs and verifies a browser, reads decrypted snapshots, and sends supported requests through the relay. |

An envelope carries routing metadata the relay needs, while its payload is encrypted with the target machine's content key and signed with a device key. The machine checks the sender's signature and local trust or revocation state before handling a request. The browser must hold the pairing handover's keys to verify and decrypt machine publications. This is the encryption boundary implemented here; the private service's data handling and deployed configuration need separate evidence. See [cloud-wire.md](cloud-wire.md) for exact envelope fields, channel rules and cryptographic construction.

## Starting the line and pairing

`clawdline cloud login` registers this machine and waits for account approval. `clawdline cloud on` sets `cloud_enabled` and asks the running daemon to apply it immediately. If no daemon is running, the switch applies at its next start. The daemon checks the switch before loading Cloud identity or keys. `clawdline cloud status` and local `GET /v1/cloud/status` distinguish off, unconfigured, connected and reconnecting states and expose typed failure information and counters. Default endpoints are defined in [`settings.go`](../internal/adapters/cloud/settings.go); configured endpoints can differ.

The machine can create a one-time pairing link with `clawdline cloud pair`, or accept a browser-originated offer with `clawdline cloud pair --offer <code>`. Pairing transfers the content key and trust information through the handover protocol. Compare the fingerprints shown at both ends. A browser that retains old account or key records can appear paired while the machine lacks its local pin. The machine list therefore keeps the normal pairing action under each machine’s more menu even when it appears paired. Repeating the authenticated handover repairs that browser without deleting the machine or admitting it from roster membership alone. `clawdline cloud devices` lists viewers known to the machine, and `clawdline cloud revoke <device-id>` revokes one locally. Local pairing and revoke routes require this machine's own credential. The [remote access guide](user/remote-access.md) gives setup steps.

### Per-browser machine grants

A local pin can carry `scope: "readonly"` or `scope: "operate"`. A pin written by an older daemon with no scope retains `operate`; an unknown scope never grants operations. The machine's `cloud_commands` switch remains an independent prerequisite. Account roster membership, revocation and signing-key checks still apply, but legacy account `send_prompt` bits do not override the machine's local grant. A roster cannot expand a readonly pin. Content-key rotation is not part of this grant change. Older daemons ignore the new scope field: rolling back to a pre-scope daemon restores its legacy operation rules. Keep a scope-aware daemon installed when relying on readonly grants; this migration does not make older executables enforce a new policy.

Select scope on the machine with `clawdline cloud pair --scope readonly`, including `--offer <code>` for a browser-originated offer. An omitted scope is not a choice of `operate`: a device this machine already holds a pin for keeps that pin's scope, so pairing a readonly browser again after its keys drift leaves it readonly. Only a device with no pin row takes the old `operate` default. The scope kept is read before the handover leaves and written under the pin store's own lock, so a grant made while the handover is in flight survives. A pin file that cannot be read, or a row whose scope this build does not know, refuses the pairing before any content key leaves. Only an explicit `--scope operate` raises a readonly device. To change an existing active pin, use `clawdline cloud grant <device-id> readonly` or `operate`. `cloud devices` and local `GET /v1/cloud/status` expose the nonsecret effective scope; pairing responses also return it.

Local APIs are `POST /v1/cloud/pairing` with optional `{ "scope": "readonly" }`, `POST /v1/cloud/pairing/offer` with `{ "offer": "…", "scope": "readonly" }`, and `POST /v1/cloud/pairing/agent` with an optional `"scope"` (same rules; it is refused with `invalid_viewer_scope` when the offer is handed to another machine, because the hand-off brief carries no scope and dropping it would hand that machine's default to the browser), and `POST /v1/cloud/devices/grant` with `{ "device": "…", "scope": "readonly" }`. Only the machine's own token can manage grants. In-process Cloud requests are refused even when their router carries that token; remote directly paired operators cannot manage grants either. Missing or revoked pins cannot be promoted through the grant API. Scope changes are idempotent and persisted before lease revalidation; write failures leave the previous in-memory and on-disk grant intact.

A readonly ceiling is persisted **before** releasing a pairing handover. Otherwise a failed final pin write could turn an explicitly readonly browser into a legacy roster-only operator. A failed readonly handover can therefore leave a visible readonly pin; it never grants operate. The reservation only lowers an existing row's ceiling: it keeps that row's key, pairing time and any refusal, written under the pin store's lock, so a failed or cancelled handover never re-admits a browser this machine revoked. Only the pin written after a completed handover restores a revoked browser, which is what pairing it again on this machine means. Ordinary legacy/default pairings continue to pin after successful handover.

Readonly viewers retain Session, document, image and terminal-screen reads and notification registration. Terminal reads still require the existing machine commands switch; this does not widen that switch. Session effects return `viewer_read_only`, separately from `cloud_commands_disabled`; the console closes the Session composer on it (as on the older spelling `cloud_read_only`, still recognised) instead of failing every press. Terminal routes answer the same code, now in the closed `TerminalRefusalCode` list with its own English and Traditional Chinese sentence. Terminal create, input, paste, resize, close and control-lease mutations are also refused. Downgrading voids existing control leases and keeps read streams open. The original viewer's pin is rechecked after terminal lane and pasteboard waits, using request-local authorization rather than the router's local bearer. An effect already committed before a downgrade cannot be recalled.

The [local CLI viewer authorization contract](cloud-local-viewer.md) records the separate viewer device flow and its current integration boundary.

The account roster and the machine's local pinned-device record are distinct evidence. A locally revoked device is refused even if it appears in the account roster; a roster read failure is not treated as an empty roster. Browser pairing is also distinct from account sign-in: a browser listed under an account does not thereby have this machine's content key.

Machine-to-machine Agent messages and work handoffs require a signed, revocable machine pair in each allowed direction. A Cloud account or viewer pairing alone does not grant one machine authority to act as another machine's Agent. Each request still names exact source and target Session executions; the receiver checks the current execution before applying work. The admission and receipt contract is in [cross-machine Agent handoff](cross-machine-agent-handoff.md).

An Agent-owned cross-machine task also needs a machine-targeted start before a target Session exists and a durable result path back to the source Session. Its contract and acceptance scenario are in [remote Agent work](remote-agent-work.md).

The hosted Console's **Cross-machine access** sidebar page lists the machines and one switch per machine pair. Turning it on establishes signed pairs in both directions, allowing all current Sessions on those two machines to exchange Agent messages and hand off work. Turning it off revokes both directions. The page reads both machines and shows a connection only when their fresh, locally pinned records agree; a partial result has a recovery action. An offline, unpaired, unreadable, or write-disabled machine cannot be changed from the page. Pairing this browser as a viewer remains a separate prerequisite in the machine switcher. Legacy Session grants are retained for in-flight compatibility but are not part of this setting flow.

Measured internal queue costs and the unchanged delivery contract are recorded in [live spool sorting](performance/cloud-spool-sorting.md).

The [independent-reader baseline](performance/cloud-independent-reads.md) separates per-viewer reads from same-reader coalescing and documents its local-only measurement boundary.

The [Go CPU attribution](performance/cloud-read-cpu.md) measures the parser and independent-reader cost separately from Node and network waiting.

## Publications, reads and commands

The publisher sends a machine descriptor on `orch/<machine>`, one complete row per session on `s/<machine>/<session>`, a content-free status projection on `ss/<machine>/<session>`, and an inventory marker after the rows. It polls the daemon's local routes, skips unchanged session content row by row on both channels, and periodically restates values so an idle machine does not appear stale. A `sessions.snapshot` request can ask the machine to restate rows after a reconnect. The publisher preserves previously known rows when a partial local scan cannot prove they disappeared. The descriptor includes the supported command words. See [`publish.go`](../internal/transport/cloud/publish.go) for current timing, freshness and inventory rules.

**The row under an opened Session is asked for, not waited for.** Opening a Session needs two things: the conversation, which the `r/` read answers in about 130 ms, and that Session's own complete `s/` row, which carries the name, the work line and the execution the composer writes to. The row arrives only when the machine publishes it, so a page that holds the status projection but not the row had nothing to draw and kept saying it was reading the conversation for as long as the row took. Measured on 2026-10-11: one row recovered from the relay's retained channel took 19.6 seconds, while the conversation in that same open answered in 131 ms. The page now sends `sessions.snapshot` to that one machine as soon as it sees the row missing — the row came back one second after the ask — no more often than `console.opened_row_restate_seconds`, and only while the status row names the same execution, because a different one is a replaced execution that the detail read already refuses as changed. A row still missing after `console.opened_row_quiet_seconds` is news rather than a longer wait: the pane names the machine that has not sent it and offers the press that asks again (`askForOpenedRow` in [`relay-reader.ts`](../web/console/src/cloud/relay-reader.ts) and [`opened-row.ts`](../web/console/src/cloud/opened-row.ts)).

**The status row a read is pinned to is asked for too.** Before a pinned read is sent, the page checks this machine's `ss/` marker and that Session's own status row, and refuses the read itself when they do not agree; the refusal names which check failed (`pinnedSession`). One of those conditions is not a disagreement at all: `no_row`, the marker listing the Session while its row is not held, is a row the relay retains and the page can ask for, exactly as the list already asks for one that a gap in its own pass left out. It now does, once, and pins again; a row that still does not come is the refusal it always was. Measured on one machine's log for 2026-10-11, that condition was the commonest read failure of the day — 23 of them, each shown to the person as an execution that was no longer current, when nothing had changed but this page's hold on one retained row.

**An expired push destination recovers in place.** A transcript generation refusal or a detail
reported as changed asks the original machine for a fresh authenticated Session list, even if the
retained status set still looks current. The page follows only one unique, current row for the
same machine and Session; it can also reread the same execution when the problem was stale
status. Both readers share one automatic attempt per user opening
(`console.execution_recovery_attempts`). Navigation, closure, reconnection and a newer opening
invalidate a late recovery. Missing, ambiguous or unavailable rows retain the existing failure
and manual action. The URL is updated to the verified execution; detail state is rebuilt, while
only unsent editor text and attached images cross that boundary. Messages, answers and close
operations are never replayed automatically. The existing `cloud.read.failed` diagnostic retains
the original refusal condition.

`ss/` contains only machine and terminal ids, `execution_generation`, `snapshot_generation`, assistant, backend, state, source provenance and observation time, freshness, scan completeness, projection time, last record movement time, the registered no-movement threshold, and narrow acceptance, attention, close-blocked, or failed-agent facts. `completed_unconfirmed` requires an unviewed Board item in phase `done`; a `deploying` item is not called complete. `close_blocked` is a measured true or false only with current closeability evidence. `failed_agent_count` is a measured count, including zero, only with a complete, untruncated provider-native agent reading. Absence means unknown; neither field asserts that the Session's work failed. It contains no label, transcript, screen, menu, shell, Git data, working directory, or content-derived summary. Record movement means new bytes in the assistant's own record; it is not a claim of task progress. A carried row without a fresh source loses its execution generation and these status facts. The old `s/` publication remains for compatibility; it still contains the full row and must retain content-level authorization. The private relay's compatible `ss/` support is deployed. The hosted console must adopt `ss/` for the list before the relay's prepared `s/` restriction to `read_transcript` can deploy. The public daemon alone cannot enforce those relay capabilities.

`waiting_for_reply` is a positive signal only when a current waiting row has an actual menu with options: either the screen was captured, or the registry wait and the transcript prove that one AskUserQuestion remains open while the screen is unavailable. The status copies none of the menu text. A waiting state or `work_person_needed` alone does not establish an open question; an absent signal means unknown.

The status inventory is `ss/<machine>/__clawdline_inventory_v1__` with `inventory: {version: 1, sessions: [terminal ids]}`, `at` (Unix seconds), `complete` (boolean), `snapshot_generation` (a 128-bit pass id) and `presentation_generation` (a 128-bit display id). All the rows of one set carry that same pass id; a viewer discards a row with a different pass id. If `ss/` publication fails against an older relay, its retry remains separate from the legacy `s/` inventory, so the older list keeps moving.

**Cold browser reads are per machine and bounded.** The account's encrypted descriptors establish the machine roster; each machine's signed `ss/` marker establishes its Session set. A new browser can receive the roster before the default status replay and before the `orch/` command descriptor. When that marker or a named row is missing, one authenticated `sessions.list` answer can supply the Session set and display facts immediately, unless the machine has explicitly declared that it does not support the read. The answer must be fresh and give every Session a valid execution generation. A complete scan is authoritative by itself; an incomplete scan whose exact ID set equals the current signed `ss/` inventory is complete for that signed set. Otherwise the browser shows only the individually pinned rows it received, explicitly marks the list partial, and never infers a missing or empty Session from it. When a person disables a terminal source, the daemon reports the merged scan incomplete but can still publish a signed marker for the sources that remain enabled. The compact `sessions.list` answer is also complete for enabled sources when all of them answered; a failed enabled source remains partial. This lets a cold browser establish the enabled Session set before the signed marker arrives. Concurrent callers share the answer and the presentation pass reuses it rather than reading twice. The fallback is revalidated at most once per `console.status_marker_retry_seconds` while visible. When a signed status pass is already ready, its list names may make the same named, authenticated read before the descriptor arrives. If the pinned read is refused, the page explicitly subscribes to one missing marker or row, shares that pending request across callers, and releases the subscription after an answer or timeout. Neither absence nor refusal is an empty Session set. Content and transcript reads begin only when a person opens an exact Session; the machine rechecks the execution generation for each pinned read. Each machine's loading, partial, stale, offline and failed states remain independent, and a previous list stays visible without authorizing actions from stale rows.

An opened fleet conversation sends under its pinned machine, Session, and execution generation after a fresh status projection admits that exact target. The separate retained rich-row replay can be incomplete during reconnect and is not an action gate for that conversation. On a page without an exact target, an absent rich row means the Session cannot be verified yet; it does not prove that the machine deleted it.

The single-machine page reads the same per-machine projection as the fleet before consulting the older rich snapshot. Its first list therefore has the fleet's whole verified ID set even when rich `s/` replay is blocked or only one row has arrived. It joins names by exact machine, Session and execution; missing names leave those rows visible and the list marked incomplete. With no signed marker, it has no authoritative row count and never treats the first nonempty batch as final. It reuses the fleet's recently verified display rows on a switch, including rows retained when a later presentation pass has only delivered its first title. The batch's time on this page controls its reuse; a quiet Session's older terminal observation does not expire its title before the batch does. A signed marker may carry a row whose terminal source is unverified, which remains visible with that source state. The older rich snapshot remains a fallback for machines that have no usable projection. Content and writes remain pinned to the exact execution and do not inherit authority from a displayed stale row.

Scheduled Tasks uses a separate machine fan-out because schedules are not in the Go daemon's `orch/` snapshot. The hosted section waits for the machine roster, then sends one named read per paired machine concurrently and paints each machine's answer as it arrives. The named read may precede that machine's `orch/` descriptor; a descriptor that explicitly excludes schedules still prevents it. Pending machines have their own loading line; failed machines remain named as unreadable rather than silently counted as empty. A section remount paints its last answer from memory; a changed roster invalidates that answer before requesting the new set. The visible page reads at most once per minute unless the roster changes or a local action explicitly asks for an update. Optional project labels resolve after the inventory and never delay its first paint.

An opened Session's named `info` and `transcript` reads may likewise precede the `orch/` descriptor when the signed status row names a current execution. An explicit unsupported descriptor stops the read; otherwise the machine checks that exact execution and the viewer's read permission. Older transcript pages and artifacts follow the same rule, so delayed descriptor delivery cannot leave an otherwise readable conversation closed.

**A status row is published when that row changed, not when the set did.** The pass id names the set: the listed terminal ids and the execution each row speaks for. While both stand still it stands still, so an unchanged row is not re-sent and the one the relay retains remains what the newest marker means; a Session that closes leaves the set and a restarted one brings a new execution, and either way the pass id moves and every row states the new one before the marker does. That is the property the marker exists for — a relay-cached row cannot satisfy a marker of a different set merely because it has the same terminal id — and it no longer rests on the id being drawn fresh each pass. Each row then has its own volatile window (`cloud.session_row_volatile_seconds` in [`internal/domain/capacity`](../internal/domain/capacity)) and its own heartbeat. Measured on the running daemon for 2026-10-10: the busiest hour sent 3,725 `ss/` frames for 13 listed Sessions, 287 per Session-hour, because one working Session's elapsed time or token count restated the whole set every 15 to 30 seconds. Replaying half an hour of ten Sessions, two working and eight idle, that publisher sent 1,331 status frames and this one sends 337 — 266 down to 67 per Session-hour, with the idle rows down to their heartbeats (`session_status_coalesce_test.go`).

**Presence and voice use the same availability facts.** A recent authenticated inventory is
usable for the existing five-minute freshness window; silence alone cannot reveal the instant a
machine sleeps. An exact relay `machine_offline` receipt overrides that inventory immediately,
refreshes the machine roster, and excludes the machine from every voice selection path, including
a saved preference and a sole candidate. Expiring the retry backoff permits another request; it
does not prove the machine returned. A delivered receipt or verified live envelope clears that
offline evidence, while retained replay does not. Voice candidates must still have current
inventory data. If none qualifies, source lookup and dictation return
`cloud_voice_host_unavailable` without sending audio to a stale host.

**Nobody connected, no Session rows.** Each relay receipt says how many sockets a publication reached. When the newest `ss/` or `orch/` receipt reached nobody and no viewer has been heard from since, a pass builds no Session list and sends no row; the descriptor alone keeps its 240-second heartbeat, so a viewer that arrives does not see the machine as stale. A page that connects asks `sessions.snapshot` first and is answered by a pass built then, and any envelope from a viewer, or any receipt that reached one, resumes ordinary passes. Measured on 2026-10-17: in ten minutes, 409 publications all reached nobody, and with no local console open each 5-second pass built the whole list (about 90 ms of CPU) to send them (`publish_idle_test.go`).

`presentation_generation` is the hash of the machine's display facts — the work line, work state, shells, attention — and carries none of their words. A viewer re-reads one pinned machine list per machine when it changes, which is where the list's titles, icons and work notes come from. It is additive: a page without it keys that read on `snapshot_generation` as before, and against a machine without it `snapshot_generation` still moves with every display change, so a new page falls back to the same behaviour (`presentationPass` in [`all-machine-sessions.ts`](../web/console/src/cloud/all-machine-sessions.ts)).

**Deploy the console before the daemon.** The new page is correct against both machines, but a page cached before `presentationPass` keys its pinned read on a pass id this daemon keeps still, so on that page a title, icon or work note in the fleet list is re-read only when a Session is listed, closed or restarted until the page is loaded again. Its rows, states, attention and freshness are unaffected.

The machine stores a random 128-bit generation per Cloud machine and terminal id. A fresh row pinned by PID and kernel process start keeps its generation across daemon restarts. Confirmed disappearance removes the old identity; reappearance or a changed process pin gets a new generation. Partial or unreadable source scans do not prove disappearance. A pinned action must compare machine id, terminal id, and generation through `Server.AdmitExecutionTarget` after a fresh authoritative scan, and still check the viewer's current capabilities and revocation. Older clients without a generation keep their legacy route behavior; they cannot claim the stronger pinned admission. On platforms without a kernel process-start reader, generation is absent and pinned operations refuse `execution_source_unknown`.

New Cloud `transcript` reads accept `expected_generation` beside the existing `session` and `limit` (and optional `priority`); new `info` reads accept it beside `session` and `parts`. The bridge supplies the machine id from its authenticated `ctl/<machine>` channel and sends both as local admission headers. The HTTP route checks before and after reading content; the Cloud bridge rechecks revocation before sealing a pinned read's answer. Missing or mismatched generations get typed refusals. Older unpinned request shapes still work with their prior semantics.

The separate encrypted `r/<machine>` request channel carries generation-pinned Session content reads under class `ctl`: info (including a list-only title, CWD and project icon), transcript pages, skills, images, Git, screen, Agent, Shell and documents. Each read names machine, Session and execution generation; the machine checks current pairing and `read_transcript` permission before and after the local route, and rechecks the execution after the read. Replies use the existing exact `t/<machine>/<session>` channel and echo the read name plus machine, Session, generation and request sequence. A viewer must match all proof fields before showing content. An absent `machine.read_content_v1` in `orch/` identifies an older content bridge without blocking an available `ss/` status list.

An incoming request arrives on `ctl/<machine>`. The transport verifies and decrypts it, then [`Service`](../internal/transport/cloud/cloud.go) hands it to `cloudops`. The answer is sealed for `t/<machine>/<session>`, including the reserved machine reply session for machine-wide reads. `cloudops.Implemented()` is the machine's current operation list; the hosted console's [`carry.ts`](../web/console/src/cloud/carry.ts) maps routes to that vocabulary. Unsupported or machine-only routes, including terminal control, are not general Cloud capabilities.

Every signed-in viewer paired with the target machine may send. The machine verifies the viewer's account identity and decrypts its request with that machine's content key before handling it. A machine-local key pin, when present, takes precedence over the account roster; a local or account revocation refuses the viewer. This also admits older browsers that hold the machine key from a pairing completed before local pins existed. `cloud_commands` is checked for each effectful request. `clawdline cloud commands on|off` changes that machine switch without restarting the daemon. A Cloud command uses the same in-process HTTP handler and authorization gate as a direct request.

Conversation picture previews, original reads, capacity boundaries and the relay follow-up are described in
[Conversation image delivery](image-transfers.md).

The separate `cloud_terminal_direct` switch controls the WebRTC carrier for Terminal and Session
reads. It defaults on; `clawdline cloud direct on|off` or this machine's local Settings → Remote
page changes it. The local-only `POST /v1/cloud/direct` route persists the choice and replaces the
Cloud line so existing peers close and the relay remains available. A hosted browser cannot change
this machine switch. Beside it, `cloud_direct_local_network` (default off; Settings → Remote while
direct connections are on, `clawdline cloud direct local on|off`, or the local-only
`POST /v1/cloud/direct/local-network`) lets the direct carrier use RFC 1918 IPv4 host addresses so a
phone on the same network connects without hairpinning through the router; on macOS its first use
shows the Local Network permission prompt once, and a denial leaves the public path or the relay
([terminal-direct-path.md](terminal-direct-path.md)). The hosted Devices connection sheet offers
**Enable same-network connections** after a failed direct negotiation when both encrypted SDPs
contain the same public IPv4 server-reflexive address and the machine reports the opt-in is off.
It says **Possibly on the same network**: sharing a public exit does not prove a shared LAN.
Only a paired viewer with operation permission can enable this one setting through
`direct-local-network-enable`; it cannot disable it or change other Cloud settings. The machine
persists the opt-in without replacing the relay, so the successful setting receipt arrives before
the browser retries direct ICE. Each candidate exchange reads the persisted policy. The prompt
explains that macOS may ask for Local Network access on the target machine. A successful setting
change and a successful direct connection are reported separately; denying system permission
still leaves the encrypted relay available. An EC2 instance can additionally set `cloud_direct_udp_port` and
`cloud_direct_public_ip` together to multiplex direct ICE on one inbound UDP port; see
[Linux setup](linux.md#let-a-cloud-browser-connect-directly-to-ec2).

### AWS Linux direct connection

For Terminal and Session reads from a paired phone, direct WebRTC removes the relay from the live
data path when ICE succeeds. Cloud still carries signalling and other commands, and the encrypted
relay takes over if direct ICE fails. To configure an EC2 Linux machine:

1. Confirm its AWS account, Region, instance, network interface, security group, public IPv4,
   Internet Gateway route and network ACL. A private IPv4 alone is not reachable from a phone on
   mobile data. A public IPv4 may change after an EC2 stop/start; update the setting if it does.
2. Merge the following keys into the Clawdline service user's existing
   `~/.config/clawdline-next/config.json`, preserving other keys and replacing `PUBLIC_IPV4` with
   the instance's actual public address:

   ```json
   {
     "cloud_terminal_direct": true,
     "cloud_direct_udp_port": 3479,
     "cloud_direct_public_ip": "PUBLIC_IPV4"
   }
   ```

3. On the instance's security group, permit **inbound UDP 3479** from the phone's public IP, or
   `0.0.0.0/0` when that address changes. Permit outbound UDP to the phone's dynamic ICE port;
   `1024-65535` is the general rule when its port is unknown. A custom stateless network ACL and
   the host firewall must permit both directions. Do not open HTTP 7727, SSH, a NAT Gateway or a
   broad inbound ephemeral UDP range for this connection.
4. Run `clawdline cloud direct on` as the service user and check `clawdline cloud status` for
   `direct true`. On Linux, `ss -lunp` should show Clawdline listening on UDP 3479 after a
   direct offer. From the paired phone on mobile data, open the machine's Terminal and type a
   harmless command. A daemon log `input` receipt with `outcome=ok_direct` proves that input used
   the direct carrier. A working Terminal without that receipt may be using the relay.

The port and public IP must be configured together. To pause direct use for development, run
`clawdline cloud direct off`; the encrypted relay remains available. Security group rules and an
Internet Gateway have no separate hourly charge, while an existing public IPv4 and Internet data
transfer retain their usual AWS charges. See the [full EC2 procedure](linux.md#let-a-cloud-browser-connect-directly-to-ec2)
for rule commands, a safe transition from older ephemeral rules and further verification.

The connection backs off and reconnects. An outbound spool owns publish order and capacity, while inbound queues and per-channel answer reserves produce typed busy or refusal answers where possible. An accepted relay write is not the same as an executed command or an observed answer. Logs and status counters separate published, acknowledged, answered, refused and undeliverable work. Protocol and capacity details are in [cloud-wire.md](cloud-wire.md), [limits.md](limits.md), and the transport code; this overview does not promise delivery after every network or process failure.

The hosted console also records a content-free `cloud.write.failed` viewer event for a refused write. Its word, code, layer, outcome, connection state and stage distinguish a refusal before publication from one whose envelope may have reached the relay or machine. The event carries no message text or request body and is delivered through `diagnostics.events` to the paired machine's daemon log. A read or mutation waiting for a complete command descriptor follows a replacement Cloud client instead of spending the remaining five-second wait on a retired one. A mutation checks that current client's descriptor again before publishing.

The browser's Cloud line reconnects in the background after a temporary failure. Reaching the consecutive-attempt limit changes it to one probe per maximum backoff interval; it does not leave an open Session waiting indefinitely for a visibility or network event. Returning to the page or regaining network can advance the probe, subject to the minimum spacing between attempts. A person's retry on a retained message card also wakes the line before the card reads the transcript and considers resending. The card still preserves its request key and checks for an already visible turn, so reconnection never licenses a blind duplicate. Permanent protocol and authorization refusals keep their terminal handling; repeated relay `forbidden` refusals remain bounded rather than being probed continuously.

## Which machine the hosted console is on

The header machine menu opens and closes around its top-right origin in 200 ms. Fleet machine
groups expand and collapse their intrinsic content height in 220 ms, keeping Session text at its
normal size. Both transitions reverse on repeated presses and become immediate under reduced
motion. Closed content is inert and hidden from accessibility immediately; menu focus returns to
the trigger when it was inside the menu. Collapsed groups stay current but do not register their
spinner canvases with the drawing clock.

An account with more than one readable machine opens on **all of them**: the fleet list is the
way in, and the machine list — which is where pairing, renaming and forgetting live — is one press
away under the header's name. A tab that has chosen a single machine keeps it, because the choice
is that tab's own (`sessionStorage`); a new tab has made none. An account with one readable machine
still opens its list once, so the first thing a person sees names the machine and offers its
pairing. The rules are `web/console/src/cloud/opening.ts` and are tested there.

On a cold load the gate holds one opening card until the account machine roster answers. The
roster's active ids decide which retained relay rows may appear; a revoked registration or an old
relay snapshot absent from the account cannot restore a machine card. If the account roster cannot
be read, the page asks for a retry instead of opening from possibly stale relay membership. The
opening decision is made before the machine picker is painted, so an automatic fleet or addressed
Session open does not flash the management list first.

When a fleet action needs one machine, its dialog shows each machine's name, platform and a
server or personal computer silhouette. Machine management keeps Rename as a named control beside
Pair and Forget. A confirmed rename changes the account label used by the picker, switcher and
fleet immediately; it does not rename the operating system host.

A cold browser load has two bounded paths. The signed `ss/` inventory marker names the complete
Session set for each machine. If its marker or a row is missing, a complete, fresh, authorized
`sessions.list` answer supplies the set and its titles in one read; an unanswered list falls back
to exact retained marker and row recovery. The fleet keeps its last answer while a later
refresh is pending. An opened conversation is pinned to the execution generation from the
complete `ss/` set or authenticated machine list; its
already authorized presentation can supply the original Session pane's display row when a rich
`s/` replay is absent. The pane then reads `info` and transcript for that exact execution. This
keeps a quiet Session readable without asking every machine to restate its whole rich list.
The detail selects that pinned destination during render, before local selection effects run.
Until its machine and execution match a display row, it shows no conversation; an earlier local
selection cannot supply the detail while a newly opened Session is arriving.

Scheduled Tasks wait for the fleet roster, then ask each eligible machine independently. The
section shows loading, empty, partial failure and last answered rows separately; remounting the
same roster reuses the last answer until its refresh is due. `FleetSessionList`, `status-source`,
`relay-reader` and `ScheduleSection` own these steps respectively.

Underneath the fleet the console still reads **one** machine: the original Session page, its
Projects, its schedules and its terminals are one daemon's. `CloudGate`'s `pointAt` moves every
seam — the `/v1/` fetches, the session stream, the transcript pictures, the terminal host, the kept
cards, the persona catalog — onto another machine in place, and it is the same code whether the
move was made by opening a fleet Session on another machine or by naming one in the header. It used
to be `location.reload()` for the second case, which threw the console away to arrive where it
arrives directly now.

Two things follow, and both were wrong before:

- **The header names the machine being read**, not the machine the tab first chose. The fleet
  repoints the reader under an opened Session, so those two drift apart; the header, the
  one-machine list, the schedule form's machine and the machine-list highlight all read the
  reader's.
- **Naming a machine for "new Session" is a destination, not a move.** The sheet reads that
  machine's Projects (`/v1/places?machine=`) and roles (`/v1/personas?machine=`) and starts there —
  a place id carries its own machine, so the start, the resume and the past list follow it — and
  the fleet list stays on every machine, with the new Session appearing under its machine's
  heading. The voice command, a new work item and the terminal list are still that machine's own
  console features, so naming a machine for one of them does move the console onto it.
  `web/console/src/cloud/machine-tool.ts` holds the rule.

## Hosted console and operations

The hosted and daemon consoles share source but require different builds. The hosted build must set `VITE_HOSTED_CONSOLE` so its entry point selects `CloudGate`; an ordinary local build selects `DoorGate`. [Cloud Session view parity](cloud-session-view-parity.md) records the selected-machine and all-machines status and action seams. [Deploying the hosted console](hosted-console.md) documents the build, production ancestry check, `BUILD.json` stamp, served-bundle `CloudGate` check and rollback procedure. A build stamp alone does not show that the served bundle is the Cloud one. This page makes no claim that the currently deployed bundle or private service matches this checkout.

The Cloud console shows the selected machine's Projects and can register an
existing directory on that machine. Press **+ Add Project** in Projects or the
new Session sheet, browse folders on the selected machine, and add one. The
session start sheet refreshes automatically while open; reopen **Projects** to
refresh that page. The CLI command `clawdline project add` also reports success only when the serving daemon
registered it; `clawdline project list` reads that same explicit list. If the daemon cannot be
reached or authenticated, the command fails without writing another list. [Adding a project](user/projects.md#add-a-project)
gives the full steps.
Relative and absolute directory arguments also work. **Projects** offers **Remove from list** for
each row on both the local and Cloud console; it hides that directory from the Project and start
lists without deleting files or sessions. Re-adding it restores visibility.
The Project list refreshes as soon as removal succeeds; an open session start sheet refreshes automatically.

The proposed [connection health administration](admin-connection-health.md) covers durable cross-source timelines, independent stall capture and agent investigation evidence. It is a product plan, not a shipped monitoring capability.

For a machine that appears offline, inspect `clawdline cloud status` or the local status route first: the switch, identity, connection state, last error and publication counters narrow down which side has evidence. Then inspect the hosted build using [hosted-console.md](hosted-console.md). The status route and logs expose this machine's observations; they do not prove that a particular browser decrypted or displayed a frame. [cloud-cutover.md](cloud-cutover.md) records local preflight and stand-in tests, not a fresh production validation.

## Limits of this overview

- Public code and tests establish local behavior and wire compatibility against fixtures. They do not establish the private service's current implementation, capacity, retention, pricing or live availability.
- The Go key store is owner-only files, not a platform keystore. Protect and back up the daemon's state directory accordingly; this repository does not claim hardware-backed storage.
- Pairing grants access to encrypted content. Revoking a device stops future requests according to the machine's checks; it cannot erase content that device already decrypted or copied.
- The hosted console only carries operations present in the machine's advertised vocabulary. Version gaps can leave a request unsupported or waiting for its timeout; check both builds before assuming a feature is available.

For the wider system layout, see [architecture.md](architecture.md). For user-facing setup and alternatives, see [remote access](user/remote-access.md). Historical Swift implementation details remain in dated records and the archive, not in this current overview.

### Following model picker transitions

A selected Session keeps its exact rich-row subscription after the first row arrives;
list recovery must not unsubscribe that live view. Closing the view releases its hold.
Opening Codex's model picker, answering its model step, and encountering an unread
waiting menu also start a bounded read follow-up. The follow-up uses the registered
transcript follow policy (two seconds initially, then four seconds, for at most ninety
seconds), permits one read at a time, and pauses reads while the page is hidden.

A successful read confirms a request, not the arrival of the next menu. The page follows
until a different menu is observed, retaining Read again across a temporary idle gap.
After the automatic window expires, the manual read remains available. Stale model
options stay disabled during that gap, including after the ordinary press hold expires.
No follow-up replays an answer key. Switching Session or execution cancels the old
follow-up; ordinary unread questions stop following when the Session resumes.

The [static HTML report reader](html-artifacts.md) describes the document format, isolation, compatibility and measurement boundary for self-contained artifacts.


### Recovering a browser that can sign in but cannot read

An account session and a machine's cryptographic pairing are separate. A cached
machine list can survive a missing local pin. A read timeout alone does not prove
that pairing is broken: the read alert therefore offers both retry and pairing,
without silently deleting a device, changing its permissions, or trusting the
account roster as a replacement for the machine's pin.

The pairing card offers a complete CLI command containing the short-lived offer.
Run it on the named machine, keep the requesting browser open, and compare the
fingerprints. The code is sensitive until it expires; do not publish it. On phones,
Safari and the Home Screen app have distinct keys: complete the flow in the same
browser container that failed to read.

A macOS shell exposing `clawdlinePairLocal` also offers a direct handover when the
requested machine matches the authenticated local daemon. A native confirmation
names that machine and explains access before the ordinary pairing offer route
runs. The bridge accepts only the Cloud main frame, never exports the local token,
never launches an assistant, and retains existing access by omitting a new scope.
The existing fingerprint comparison still completes in the browser. Older shells
continue to use the copied command; hosted deployment alone cannot add a native
message handler to an installed App.
