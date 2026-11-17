# Installed-agent common-flow harness

Construction baseline: `e2da5e1a0da84128ff85d83c1cbb4c8f1d29c5d6`.
Upstream protocol reference remains Codync `9f67eb09`; this harness changes no
provider, catalog, protocol, capability admission, or product capacity limit.

**Actual installed-agent execution: NOT EXECUTED.** This is a construction
delivery. Native execution, Cloud, dev installation and visual acceptance belong
to the later acceptance card after all construction steps. No existing successful
native run or historical evidence is replaced by these controls. See also
[the earlier native qualification](project-assistants-native-acceptance.md) and
[the advanced verification matrix](project-assistants-advanced-verification.md).

## Entry points and isolation

`native_common_flow_test.go` adds `TestInstalledAgentsNativeCommonFlow`.
Without its exact task-specific token it skips before constructing an installed
provider. A nonempty wrong token fails. Its absolute state root must not exist,
including as a dangling symlink; an atomic `Mkdir(0700)` refuses races and reuse.
The parent directory must already exist. A new root is mandatory for each run.

The ordinary test contract must leave all native opt-ins unset. Do not put a
native flag in `tools/check.sh`, CI, a shell profile, or another test. The token
below is an explicit batch selector, not a credential and not approval to run it.
Obtain separate approval for installed agents and any potentially paid model
requests before using these commands. A prompt-write cap does not establish a
currency cost cap or count vendor-internal requests.

The test reuses the existing `newHostFixture` composition: actual Host, Service,
Engine, schema installers, SQLite, native tool ownership, output snapshot store,
room driver and collaboration transactions. It moves the fresh repository into
the batch root and opens a new database there. A fixture broker is present only
to compose the Host; dispatch and work-status tools are refused by the wrapper.
No existing Bot, Session, Project, database or native history is adopted.
`CLAWDLINE_NEXT_DIR` is scoped to the new root. The production installed provider
uses the person's existing CLI/login and its normal native memory suppression
and owner records; this is not a claim that account access is isolated.

Capability admission may install the bundled, pinned npm adapter into the new
state root through the existing `InstalledProvider` setup path. It also probes
local CLI versions and native memory policy. It is not a model access check.
No such setup or native capability call ran during construction.

## Fixed batch budget and stop rule

Every invocation has a shared, immutable maximum of **24 provider Opens,
24 prompt writes, and 96 owned Clawdline tool calls**, across both agents and all
routine, peer and group turns. These numbers are test inputs, not product bounds.
No automatic retry, resend, cap increase, fresh command after unknown, permission
answer or alternate provider is authorized. Repeated execution identity opens
are refused. Reservations and journals precede the external boundary under one
lock; concurrent peer/routine turns use the same counters. Memory edits,
dispatch and work-status operations are forbidden. Browser and team tools are
not forwarded by this batch's wrapper. Native vendor-internal tools and billing
requests are not counted as owned MCP tool effects; only the final safe read
probe requests a native tool, and any permission offer stops the batch.

The wrapper stops on provider Open/Send/Wait errors, any owned tool error, an
event error, cap exhaustion, or evidence-write failure. The stage runner stops
on failed/unknown/cancelled turns, deadline, assertion failure or permission
wait. Both layers seal the entire batch, including subsequent stages and the
other agent. A native permission offer is saved without choosing any of its
options. The wrapper refuses `Answer` even if a caller tries it. Ordinary
shutdown still cancels and closes owned sessions; cleanup never means consent.

The functional path currently requires ten prompt writes per agent before the
permission probe, including the isolated routine and three peer turns. The
permission probe can consume an eleventh. Fixed headroom is not permission to
retry. Actual counts are taken from the journal, not inferred from these totals.

## Stage evidence

| Boundary | Required evidence and honest limitation |
| --- | --- |
| Capability admission | Actual installed version/capability record; resume supported and native memory demonstrably disabled. Unsupported admission stops before the first prompt. |
| Main chat | First `read_memory` is empty; one explicit nonce message, one accepted input/event and its real SQLite receipt. |
| Continuation and approved memory | Service saves one scoped approved fact. Second native MCP read observes it; response combines the fact with a token only the earlier chat held. Two native Opens must be fresh then resume with the same native ID. |
| File and HTML publication/list | Agent calls the real `send_file`, `publish_artifact`, and `list_artifacts` tools on a task-created HTML input. Exactly two durable output cards, one artifact, immutable snapshot bytes, and actual tool-call observations are checked. The input file is a fixture; published receipts are never seeded. |
| Isolated routine | Agent saves a paused definition and requests one test run; Engine ticks it. A distinct native session slot and native ID open without main resume, one successful routine run and exactly one matching durable output appear. The paused interval cannot generate background repeats. |
| Peer roundtrip | Agent lists Bots and calls `message_bot`; the second fresh owned Bot performs the review, the source receives a correlated reply turn and emits its summary. Exchange endpoint keys, full Project scope, exact reply, reply-turn ID and delivered state are checked. No installed evidence is inferred from scripted peers. |
| Group roundtrip | Service creates a room with the two owned Bots and sends an explicit mention. Group history must contain the addressed member's exact reply. The group/room runtime decides who may speak; model text supplies no authority. |
| Forget and late events | Service forgets the approved source after functional turns finish, an event from the completed old fence is refused, then a fresh native owner with a different native ID actually reads empty memory and publishes one exact message. Durable memory tombstones remain marked forgotten. Forget changes privacy provenance; it does not by itself terminate an already running main turn. |
| Source revocation | The owned source callback becomes revoked; a Service send must return `Revoked`, with no additional durable turn or message. Source state is restored only for the separately described permission probe. |
| Native permission request/denial | Final probe asks the native agent to request a read of task-owned `a.go`, forbidding shell, writes and networks. A real offer records the actual options and ends the entire batch waiting for the person; no answer is sent. A model finishing without an offer stops with an explicit no-offer diagnostic. This cannot pass as a native denial test: installed adapters expose no deterministic safe offer/denial injection port. Four-option facts and trusted denial are tested separately by deterministic controls. |
| Team mutation | Explicit `unsupported`, with cause: `create_bot`/`update_bot` require a real person's approval card. No card is approved and the wrapper does not forward team management tools. This is a batch safety restriction, not a claim the product lacks team tools. |
| Fallback refusal, unknown and safe switch | Explicit `unsupported`: installed adapters do not emit the authoritative `PreSendRefusal` guarantee until separately qualified (`provider_failure.go`). The harness never signs out, exhausts an account, or invents a no-send guarantee. Deterministic tests inspect safe/unsafe attempt facts; that is not an installed switch. |

Four current permission kinds are `allow_once`, `allow_always`, `reject_once`,
and `reject_always`. Actual native option IDs, labels, kind, allow/read-only
facts, fence, turn and native request ID are recorded as offered. Never synthesize
missing native options from this list. The control sends only `reject_once`
through the real trusted Service entry point against a scripted offer and
reads its resolved selection back from SQLite.

`evidence.json` records installed versus deterministic evidence kind, fixed caps,
reserved operations, requests/results/errors, native IDs, permission/trace
events, UTC stage starts/ends and the first halt. Its `remaining` array explicitly
marks unattempted agent/boundary pairs `not-executed`. `stages` uses `running`,
`pass`, `failed`, `waiting-for-user` and `unsupported`. Unsupported never means
pass; remaining never means a capability was exercised. Temporary-write/rename
checkpoints precede effects; deferred and test cleanup retain final journals,
per-agent capability records, source/peer exports and runtime usage after stage
failure. Evidence persistence failure itself refuses further effects.
The full SQLite database, repository, outputs and installed state remain under
the root for inspection. Evidence contains test prompts and local paths;
keep runtime evidence private, out of the public repository.

## Future authorized commands

Create the parent artifact directory first. Replace the example roots with new
absolute task-owned paths that do not exist. Do not precreate the roots.

```sh
# After separate native/model approval only. The final permission probe normally
# stops the full batch during Claude, leaving Codex explicitly not executed.
CLAWDLINE_NATIVE_COMMON_FLOW_E675BF5B=e675bf5b-common-flow-once-24-prompts \
CLAWDLINE_NATIVE_COMMON_FLOW_ROOT=/absolute/task/artifacts/common-flow-both-01 \
go test -json ./internal/adapters/projectassistanthost \
  -run '^TestInstalledAgentsNativeCommonFlow$' -count=1 -timeout=45m

# Collect Codex independently only after reviewing the first stopped batch and
# approving a NEW batch with the same hard 24/24/96 limits.
# Go's subtest filter skips Claude without executing its body.
CLAWDLINE_NATIVE_COMMON_FLOW_E675BF5B=e675bf5b-common-flow-once-24-prompts \
CLAWDLINE_NATIVE_COMMON_FLOW_ROOT=/absolute/task/artifacts/common-flow-codex-01 \
go test -json ./internal/adapters/projectassistanthost \
  -run '^TestInstalledAgentsNativeCommonFlow$/^codex$' -count=1 -timeout=45m
```

Each command is a distinct batch with a new authorization decision and root.
Do not automatically run the second command after failure. A real permission
wait leaves denial acceptance pending; inspect the saved offer and decide the
next separately authorized human acceptance procedure. A Go test failure at
that boundary is stop evidence, not successful native qualification.

## Deterministic construction controls

```sh
# No native opt-ins. Uses only local scripted Go providers and fresh SQLite.
go test ./internal/adapters/projectassistanthost \
  -run '^TestCommonFlowControls' -count=1 -v

# Ordinary gate, with ALL native flags absent from the environment.
tools/check.sh
```

Controls cover wrong opt-in/root/reuse, exact cap exhaustion before effects,
global stop across stage and agent, persistence on stage failure, evidence-write
failure, unsupported status, actual SQLite receipts/events and scope mutations,
four-option facts/denial, real-offer global sealing, completed-turn late tools,
source revocation, and safe/unsafe fallback attempt facts. A separate control
exercises the actual Engine/SQLite fallback switch after an explicit
scripted no-send refusal, and proves that ordinary quota metadata stays unknown
without opening an alternate. Exact accepted-command replay reads its existing
receipt using its original fence; it is not a fresh unknown acknowledgement.
The complete functional control invokes the same stage function for both agents with scripted responses,
including real output snapshots, isolated routine, peer exchange and group room.
It omits the final native permission probe and labels its evidence
`deterministic-control`; success cannot establish installed compatibility.

Root integration is restricted to the two new test files and this document.
No generated contract or capacity row is needed. Root may add a link from an
existing document in its own integration change; no out-of-claim file is edited
here. Root must rerun the ordinary gate on the integrated tree and preserve the
native NOT EXECUTED state until the acceptance card authorizes real execution.
