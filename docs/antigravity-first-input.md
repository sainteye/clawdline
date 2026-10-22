# Antigravity first-input contract
Root accepted the process-bound input and durable slot design. The launch
Store and private app/terminal path are implemented. The private HTTP composition
now resolves the attached native tmux pane and rechecks process, foreground,
workspace, capability and an explicit local policy. Public capability enablement,
production policy composition and end-to-end managed CLI verification remain pending.
The private [native proof reader, Store and managed hook](antigravity-context-proof.md)
retain a pre-response fence and revalidate loaded evidence; role/Squad projection
and real CLI acceptance are still pending.
This document does not declare the complete integration ready. Format follows
docs/design-decisions.md.

## Verified subject
Pinned feature/antigravity-cli-20261022, HEAD 334e4e9ffb848f970b022c418d009cac575d467a. Before/after inspection, HEAD matched and porcelain status was empty in designated worktree. Citations use this HEAD. No source writes, vendor/API/account/browser/daemon operations or tests. Native claims use saved evidence.

Existing contracts:
- UUID guard/native-only send: internal/app/antigravity_actions.go:24-35,95-148. Request authorization/shared lane: internal/app/actions.go:40-55,106-136.
- Prepared Request/PrivateEnv/AttachTerminal and BindingPending: internal/app/start.go:74-95. Original context/skill paths restored before fresh Squad capability; immutable checks/all terminal plans: internal/app/squad_launch.go:97-146,163-194. Pending open must not reopen.
- Durable launch/terminal/full process/positive history, bounded refusal: internal/adapters/antigravitylaunch/store.go:24-79,328-405,491-584,690-740. Reopen needs fresh pin; uncertain publication blocks: store.go:235-266,522-524.
- Trusted commitment/no-UUID process verification: internal/adapters/antigravity/observe.go:63-69,355-382. Capability before pin, ancestry/CWD checks, binding before response: internal/transport/http/antigravity_managed_hooks.go:29-109. Resume UUID provisional.
- PREPARED is not loaded; immutable text/digest: internal/adapters/antigravitylaunch/response.go:12-28,52-68,99-122. System text excluded publicly: internal/adapters/transcript/antigravity.go:62-85.
- First input creates UUID; resume may lack SessionStart: docs/antigravity-cli.md:346-349. Preserve HTTP/UI gates until bootstrap/permission/proof/acceptance: internal/transport/http/start.go:594-598; web/console/src/session/antigravity-ui.ts:16-32,45-63.

## Decision and owners
Add one process-bound bootstrap action and one durable input slot per existing launch Record; retain ordinary guards. Capture transcript baseline synchronously inside authenticated PreInvocation, after native UUID discovery but before exposing injection response. Only later native system evidence establishes loaded.

Proposed private APIs:
    // app, authenticated request, existing lane
    SendAntigravityFirst(ctx, launchID, requestID, text) (Outcome, error)
    // app/ports, implemented by launch Store
    ReserveFirstInput(ctx, launchID, requestID, textDigest, process) (Receipt, *FirstInputAttempt, error)
    (*FirstInputAttempt).BeforePaste(ctx) error
    (*FirstInputAttempt).BeforeEnter(ctx) error
    FirstInputStatus(ctx, launchID, requestID) (Receipt, error)
    // private transcript adapter
    // Implemented privately, including managed hook composition:
    CaptureBaseline(ctx, verifiedBinding) (Fence, error)
    VerifyLoaded(ctx, preparedProofRequest) (Evidence, error)
    // launch Store: atomic fence/receipt preparation, then CAS evidence
    PrepareResponse(ctx, verifiedBinding, fence) ([]byte, error)
    RecordLoaded(ctx, evidence) error

Only the durable creator gets an unforgeable-by-construction volatile Go attempt with an unexported Store owner; existing/recovered receipts return a nil attempt. Closing that owner invalidates its attempts. The index now moves from known legacy versions to version 3 on the next atomic write; older readers refuse version 3 instead of dropping consumed slots or context proof. Unknown index fields are refused. Slot: request ID/text SHA256/full process/terminal/backend/context digest/phase. Proof: launch/process/UUID/path/binding sequence+digest/injection+response digests/fence. RecordLoaded CAS matches latest receipt. No private text/capabilities in public outcomes/logs.

Reuse PinProcess/LookupProcess/Commitment, Bridge, parser/helper/lane. Core imports ports; HTTP authenticates; adapters own IO/storage. Todo-ID reservation cannot guard arbitrary input (internal/app/work_v2_todos.go:470-523; internal/adapters/store/work_v2.go:2547-2564); launch slot grants effects. Seconds-only pending owner is insufficient (internal/app/actions.go:979-989).

## States and retry semantics
Input and binding/proof are independent; binding may precede Enter ack.

| State | Survives restart / retry |
|---|---|
| prepared | Immutable launch; fresh pin/attachment required. Retry only with absent slot |
| reserved-before-bytes | Fsynced before effects; status only, no replay |
| possibly-pasted | Persist BEFORE Type; partial/full text possible; no repaste/clear |
| Enter-attempted | Persist BEFORE key; uncertain submission; no second Enter |
| native-bound | Positive history; reverify live process; not input/loaded ack |
| context-loaded | Historical row/fence survives; current badge rechecks execution/bytes |

Slot stays consumed even on Unsent: avoids reset/lease races but loses convenient prebyte recovery. Only pre-reservation failure retries. Reserved/terminal ack is not USER_INPUT proof; context proof is separate.

Same key/digest returns status; conflicts refuse. Cancel/lost ack never releases slot. Unknown fsync/rename blocks until reopen; recovered RESERVED grants nothing. No expiry/retry/background continuation. Earlier conservative phase survives later write failure.

## Checks before every byte/key
Use only actual attached terminal. Reverify standalone executable/fingerprint, PID/parent, full RFC3339Nano birth, exec generation, Linux boot ID, normalized TTY/exact terminal ID, kernel canonical CWD and foreground against commitment. Tmux bounds candidates/rejects ambiguity/rechecks (internal/adapters/terminal/antigravity_identity.go:114-165); unsupported iTerm proof refuses.

Hold existing backend/terminal lane throughout. After acquisition and before EVERY read/effect check ctx, request-local authorization, provider send/permission policy, live launch/capability and intact context. Empty UUID is allowed only for this exact fresh prepared launch with no positive native history/binding; unreadable/ambiguous is not absence. Resumes cannot use this shortcut.

Bracket fresh screen reads with full checks. Require positively empty ready composer, fullyIdle=false; reject drafts/menus/slash/bang/unsafe controls with existing validator. Reserve, recheck, persist possibly-pasted, recheck immediately before literal Type. Chunked IO repeats last-mile checks before each chunk; no embedded submit/control injection.

After paste, bounded awaitShown plus fresh capture/full checks must show exact unchanged draft/HoldsTyped. Persist Enter-attempted; repeat full identity/foreground/auth AND draft check after disk write; issue ONE Enter, no other key. Reuse helper (internal/adapters/terminal/submit.go:27-66). Changed/unreadable composer yields send_unsubmitted/unknown, no cleanup/replay.

Binding arriving mid-send must be independently admitted for same launch/process; latch UUID/sequence and reverify. Switch stops next effect. Hooks never acquire terminal lane: key IO may await hook. Lane excludes Clawdline writers, not humans/OS writers; kernel check and IO are not atomic. Test/document residual TOCTOU; refuse untargetable transports.

## UUID-free cursor and loaded proof
Before first input, no UUID cursor; never fabricate UUID or select newest history. First input triggers trusted admission/Observe; synchronous PreInvocation supplies verified UUID/path. Capture stable snapshot BEFORE injection preparation/response exposure, AFTER launch preparation; SessionStart is unnecessary.

Reuse confined open, ReadBudget, complete JSONL parsing, generation/full-prefix revision (internal/adapters/transcript/antigravity.go:295-373,376-467,488-533). Fence: UUID/path, device/inode generation, complete-line length N, SHA256 of ALL [0,N), last step index. Bracket capture with fresh process/CWD/binding checks. Atomically persist fence+PREPARED before response exposure. No terminal IO under store lock. Allow one outstanding first-response fence per launch; refuse newer preparation while unresolved. Receipt replay is not delivery; never retrofit old unfenced receipts.

Private raw-row VerifyLoaded requires same live complete process/launch, positive UUID/confined path, same generation, unchanged entire N-byte prefix, complete row beginning >=N and increasing step index. Require source=SYSTEM_SDK, type=SYSTEM_MESSAGE, status=DONE, no error, native wrapper identifying PreInvocation injection. Decode once; extracted text equals ExpectedInjection.Text byte-for-byte and SHA256 equals InjectionDigest. Reject trimming/substrings, MODEL/USER echoes, generic SYSTEM, arbitrary hook JSON. Persist row start/end plus prefix digest through end. Current badge revalidates bytes/process/latest receipt; public pages never expose system text.

Missing/partial/oversized file, unknown generation or wrapper grammar: antigravity_context_proof_unavailable. Changed prefix/generation: antigravity_context_proof_stale. Missing file at PreInvocation REFUSES fenced response preparation: absence is not generation+offset zero. Preserve input uncertainty; Root must prove boundary availability or design native creation fence/private supported transport, not poller.

Evidence: docs/antigravity-launch-context.md:160-168; persisted docs/antigravity-cli.md:359-362. The sanitized native fixture now pins the exact wrapper grammar. Native file timing/first-response ordering still needs evidence. Without native request correlation, proof establishes context in this execution, not particular invocation. One outstanding first response excludes another from this launch; same-UID forgery is outside native-log trust.

## Options, compatibility and bounds
| Option | Gain | Cost/decision | Reversibility |
|---|---|---|---|
| Keep input disabled | No new effects | Retains deadlock; keep pending gates | Reversible gate |
| Process slot + hook baseline | Reuses owners; no guessing | Chosen; consumes launch after prebyte failure, needs native evidence | Code reversible; storage compatibility irreversible |
| --prompt-interactive | Avoid composer | Reject private argv/history exposure and pre-attachment effects; persisted help only (docs/antigravity-cli.md:51-54) | File/stdin needs evidence |
| Pre-input inventory/fabricated UUID | Apparent early cursor | Broad reads/races or false authority; reject | No safe advantage |

Bound slot/fence by existing launch retention/MaxLaunches/MaxReceipts/MaxIndexBytes; full refuses. Register ownership in internal/domain/capacity; measure maximum synthetic record/index size and read/confirmation time; retain red-then-green guard evidence. No new registry/service/poller.

Irreversible: stored phases/fence/loaded receipt/index migration. Version migration/downgrade refusal must prevent old readers dropping slots/resending. Public request-id/status/refusal fields are wire commitments; prefer private/reused outcomes. Errors need EN/zh-Hant-TW. Preserve UUID meaning/grants/original paths.

## Order and failure injection
1. antigravitylaunch/store.go, capacity/focused tests: slot CAS/reopen/crash boundaries; concurrent calls across restart yield <=1 execution grant.
2. app/ports, app/antigravity_actions.go, terminal tests: same lane/helper, zero generic Send; revoke/switch around every effect.
3. transcript/antigravity.go private proof, response.go, managed hooks: saved native fixture, fence-before-response, loaded CAS.
4. HTTP/contract/UI only after bootstrap/permission/native proof gates; pending outcomes/todo receipts, EN/zh-TW desktop/phone, independent code/UI acceptance. Separate synthetic from native evidence.

| Inject | Required result |
|---|---|
| Cancel before/after reserve | Before retry possible; after status only |
| Crash before paste/key; lost ack | No second paste/Enter |
| Full/uncertain store write | No next effect; typed refusal/reopen |
| PID/exec/boot/TTY/CWD/pane switch; foreground ambiguity | Stop next chunk/key |
| Revoke auth/human draft change | No Enter; preserve possible paste |
| Hook races Enter/binding appears | No deadlock; same-process latch |
| Missing/partial/oversized file, unknown generation, old receipt | No loaded badge |
| Correct text in MODEL/USER/SYSTEM, wrong UUID/old offset/error | Reject |
| Truncate/rewrite/replace after loaded | Invalidate current badge; retain history |
| Exact fenced SYSTEM_SDK/DONE | Record once; verify idempotently |

## Private native first-input composition

`managedAntigravityFirstInputs` resolves only the prepared launch's attached tmux
pane. It brackets discovery with identical pane ID/TTY/root metadata and uses
the existing pinned executable, kernel ancestry and foreground verifier. Neither
caller-selected session IDs nor inventory listings supply a native UUID.
Duplicate tmux hosts and unsupported terminal backends refuse.

The launch Store exposes integrity-checked private preparation metadata and
reservation status without restoring execution authority. The bridge separately
distinguishes no observation from an unreadable, recovered or invalid binding.
The target checks canonical native workspace and the explicit trusted local
policy before pinning the process and committing the launch. Every subsequent
read/effect repeats those checks. Owner replacement or shutdown refuses.

A positive UUID arriving before reservation or before the durable Enter-attempt
phase refuses. After that phase, only the exact same process and existing
consumed slot can latch its native UUID and increasing binding sequence. The
final check again resolves foreground after any durable latch. This admits
same-execution identity without granting another paste or Enter. A current
binding projection is not a loaded-context receipt.

Local tests use actual isolated tmux panes, kernel process identity and a Unix
hook helper with a synthetic executable; they do not invoke the vendor. They
cover no invented UUID, one consumed slot, same-key status only, conflicting
requests, early binding, missing policy, wrong workspace/pane/backend, duplicate
hosts, tampered context/capability, policy revocation, stopped runtime and pane
process replacement. The tests advance durable phases explicitly without
pasting text or submitting Enter; app/terminal transport checks remain separate.
The first targeted run failed on missing test imports and an incorrect assertion
that tampered context should be reported as known zero usage. Those tests were
corrected; the unchanged integrity check correctly reports that usage as unknown.
The three affected targeted packages then passed (0.170, 0.308 and 3.626 seconds).
These are local test durations, not application latency measurements.

Outside this private implementation: production permission/usage policy,
permission/login/vendor probes, role/Squad loaded-proof projection, deployment
and full-feature/UI acceptance. Public start/resume/send routes stay guarded.
A later single standalone 1.4.5 probe confirmed a complete file at the first
PreInvocation and the exact injected system row by PostInvocation and Stop;
see [native boundary evidence](antigravity-context-proof.md). Product managed
transport, resume timing and complete acceptance remain pending.

The first full local gate passed Go vet/build/tests, Linux and Windows builds,
repository/contract checks and console checks/build/tests, but refused a fixed
synthetic pane ID in the wrong-pane fixture under the privacy guard. The fixture
now allocates a second pane inside the owned private server and asserts it differs
from the provider pane. No privacy rule was changed. The affected wrong-pane
test passed in 0.562 seconds and the same file privacy check passed; a new exact
candidate is required for the full commit gate.

Empty immutable context needs no native injection proof. Its first-input slot
still obeys the same one-shot process/foreground/authorization checks; only
subsequent context preparation/completion avoids waiting for a nonexistent
system row. Unfenced empty preparation is neither a loaded receipt nor authority
to bypass proof for any nonempty context.
