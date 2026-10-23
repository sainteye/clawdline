# Antigravity CLI integration design

Status: proposed technical design, 2026-10-22. The person authorized planning and implementation,
then explicitly changed the target from Gemini CLI to **Antigravity CLI**. No further general
design approval is needed. Root owns installation, account sign-in, the existing Board item,
implementation dispatch, independent verification and
landing. This design child changes only this document; its declared filename is retained until
root renames it. No implementation, sign-in or model request was performed by this child.

## Problem and scope

Clawdline must support a third interactive assistant throughout its existing product: installation
and auth availability; new/resume; managed and unmanaged live discovery; exact process/terminal/
conversation identity with simultaneous same-repository sessions; conversation text, tools,
model, tokens and activity; safe state, menus and send receipts; local and encrypted Cloud UI;
children and immutable Squad launch snapshots; project rules, memory and unify; quotas; history,
retention and daemon restart. Preserve Claude/Codex behavior and existing platform boundaries.
Neither a quota footer nor a print-only adapter satisfies this scope.

Gemini CLI personal access is not the solution. Google's [May 19 transition announcement](https://developers.googleblog.com/an-important-update-transitioning-gemini-cli-to-antigravity-cli/)
and [June 18 cessation announcement](https://github.com/google-gemini/gemini-cli/discussions/28017)
say individual free, Pro and Ultra requests stopped on June 18, 2026; enterprise licenses and
API-key authentication remain supported. Root reported an OAuth success followed by a service
rejection for the individual account. That is a root observation, not this child's live test.
The person subsequently authorized Antigravity; Gemini enterprise/API-key support remains a
separate possible future feature. No automatic API-key, ADC, paid-credit, model or provider fallback.

## Evidence and current boundaries

Repository base: `cafc4a2ac7d26af5179fb3586c857574d602daf8`, initially clean, in the isolated
task branch. Its exact branch name belongs in the private delivery receipt. Citations below refer
to this base.

| Existing boundary | Checked source and consequence |
| --- | --- |
| Provider vocabulary | `internal/domain/session/session.go:17` and `api/v1/common.schema.json:21` contain only Claude/Codex. Add a distinct `antigravity`; never alias it to `gemini` or infer it from a model slug. |
| Launch admission | `internal/adapters/projects/launch.go:32`, `:284`, `:295`, `:307`, `:353`: two-provider admission, Codex-only effort/language, Claude prompt-file fallback and Codex resume fallback. Each needs an explicit third branch. |
| Installed/auth/start/history | `internal/transport/http/projects.go:116` lists two executables. `internal/transport/http/start.go:433`, `:467`, `:529` contain provider-specific auth/launch/history branches. A found executable is not proof of a usable account. |
| Process and terminal | `internal/adapters/process/ps_unix.go:267` classifies the executable's first word; `internal/adapters/terminal/tmux.go:200` only names two pane commands. Neither arbitrary `node` nor a matching string in a shell command proves an assistant. |
| Exact record identity | `internal/adapters/transcript/host.go:91` and `:182` select identity and record path. Codex already avoids cwd-only matching (`:119`). Extend this owner rather than finding a different record in each reader. |
| State and menus | `internal/domain/session/screen.go:37` has two prompt readers; `internal/domain/session/menu.go:148` falls through from non-Codex to Claude logic. `internal/app/orchestrator/composer.go:94` tries Claude's frame for every non-Codex provider. |
| Transcript and spend | `internal/transport/http/transcript.go:167`, `:238` and `internal/adapters/transcript/info.go:100` default non-Codex to Claude. `internal/adapters/transcript/ledger.go:452` infers Claude unless a payload suggests Codex. A third provider must never reach these defaults. |
| Squad ownership | `internal/app/orchestrator/squad_launch.go:73` publishes an immutable snapshot; `:47` checks capability against both terminal and conversation. Reuse it. Native provider subagents do not become broker children. |
| Dispatch permissions | `internal/app/orchestrator/dispatch.go:900` maps only Claude/Codex modes; gate permissions (`:926`) are Codex-specific. Unknown modes must be refused, not silently dropped. |
| Project context | `internal/adapters/projectfiles/unify.go:52` owns AGENTS/CLAUDE and their skills links. `internal/app/orchestrator/language.go:55` selects two language paths. Extend preview and conflict detection before writes. |
| Quota | `docs/provider-quotas.md`, Sources and ownership, explicitly makes Gemini quota-only; it uses Gemini OAuth metadata. Do not relabel this account reading as Antigravity's. |
| Platform | `docs/cross-platform.md:111`–`:121` describes Unix tmux and Windows' unavailable terminal/process readers. Native Windows external-terminal control is not supplied by adding a provider enum. |

### Verified standalone CLI, not the similarly named IDE

This child executed the root-installed standalone binary's `--version` and `--help`: **1.4.5**.
Its help admits `--conversation`, `--project`, `--agent`, `--prompt-interactive`, `--model`,
`--effort low|medium|high|xhigh|max`, `--mode accept-edits|auto|plan`, `--sandbox`, `--log-file`,
and print/stream input/output flags. It has no `--resume` or `--session-id` flag.
The pre-existing PATH-resolved `agy` instead answered **1.107.0**, an editor help page containing
`--new-window`, `--install-extension` and `serve-web`. Root was notified of this collision.
Implement executable resolution using a validated absolute standalone path and a cached help/
version fingerprint, not `LookPath("agy")` alone. Do not alter the person's PATH or IDE symlink.
The paths are intentionally omitted from this public document.

The [official resume contract](https://antigravity.google/docs/cli/commands/resume/)
uses `agy --conversation <UUID>` for exact resume. `--continue` uses a workspace-keyed latest
cache and cannot safely select among concurrent sessions. Never use latest-cache resolution
for a Clawdline resume. `/clear`, `/fork` and `/resume` can change the active conversation;
launch argv therefore does not establish permanent current identity.

The [hook contract](https://antigravity.google/docs/hooks) uses camelCase `conversationId`,
`workspacePaths`, `transcriptPath`, `artifactDirectoryPath`, `modelName`; CLI logs live below
`~/.gemini/antigravity-cli/brain/<UUID>/.system_generated/logs/transcript.jsonl`. Named hook
blocks can live in `.agents/hooks.json` or global configuration. Events are `SessionStart`,
`PreInvocation`, `PostInvocation`, `PreToolUse`, `PostToolUse`, `Stop`; hook timeouts are seconds.
`PostInvocation` is a model-call boundary, not a completed user turn. `Stop` includes `fullyIdle`
and `terminationReason`. `SessionStart` supports `injectSteps`. Tool pre-hooks require a decision;
`ask` respects prior grants. Do not copy Gemini's snake_case payload or millisecond timeout.

The [status-line contract](https://antigravity.google/docs/cli/statusline/) separately exposes
`conversation_id`, optional `transcript_path`, model, workspace, context totals/window,
`agent_state`, `tool_confirmation_pending`, pending-input and task counts, and optional numeric
quota buckets/reset times. It also includes email: whitelist useful fields locally and discard
email/identity before persistence or publication. It is an opt-in supplementary observation,
not authorization. Preserve an existing status-line command through a reviewed composition;
never silently replace it. Absent optional fields remain unknown, not zero.

The [headless protocol](https://antigravity.google/docs/cli/headless/) supports persistent
`--input-format stream-json --output-format stream-json`, but rejects control-request/response
messages and CLI slash commands; approval-requiring tools may be soft-denied even with exit 0.
It therefore cannot substitute for an interactive permission-capable Session.

## Reference implementations read at pinned commits

These are source inspections, not successful local runs. Upstream test files are evidence of
intent, not a test receipt for Clawdline. No upstream code was copied. Reuse mechanisms with
Clawdline's authorization, capacity, privacy and receipt contracts; preserve applicable notices
if implementation later copies substantial code.

### Herdr — `herdrdev/herdr`, Apache-2.0

Commit: `5bd900d88c80d95f5b145397e45bbeea398b8165` ([license](https://github.com/herdrdev/herdr/blob/5bd900d88c80d95f5b145397e45bbeea398b8165/LICENSE)).

| Concern | Source, observed behavior and reuse limit |
| --- | --- |
| Identity/resume | [`herdr-agent-state.sh:20`](https://github.com/herdrdev/herdr/blob/5bd900d88c80d95f5b145397e45bbeea398b8165/src/integration/assets/antigravity_cli/herdr-agent-state.sh#L20) requires managed pane/socket environment, sends `conversationId` and optional `transcriptPath` to that pane. [`agent_resume.rs:281`](https://github.com/herdrdev/herdr/blob/5bd900d88c80d95f5b145397e45bbeea398b8165/src/agent_resume.rs#L281) emits `agy --conversation`. Useful positive binding; unmanaged PID discovery still needs our own proof. |
| State/permissions | [`detect/mod.rs:342`](https://github.com/herdrdev/herdr/blob/5bd900d88c80d95f5b145397e45bbeea398b8165/src/detect/mod.rs#L342) classifies this integration as identity-only. [`antigravity.toml`](https://github.com/herdrdev/herdr/blob/5bd900d88c80d95f5b145397e45bbeea398b8165/src/detect/manifests/antigravity.toml) separately matches permission, question and trust footers before working indicators. Gemini has its own manifest. Reuse ordering, capture our own version's screens. |
| Installation/auth | [`targets.rs:1585`](https://github.com/herdrdev/herdr/blob/5bd900d88c80d95f5b145397e45bbeea398b8165/src/integration/targets.rs#L1585) installs only its named hook block, preserving neighbors. It is not a Google login implementation. |
| Dispatch | [`agents.rs:139`](https://github.com/herdrdev/herdr/blob/5bd900d88c80d95f5b145397e45bbeea398b8165/src/app/api/agents.rs#L139) validates readiness before prompt submission. This is a useful send gate, not proof of Clawdline Squad snapshot loading. |

### T3 Code — `pingdotgg/t3code`, MIT

Commit: `3b95e7831b11b17627dff16843bc087bfca397ec` ([license](https://github.com/pingdotgg/t3code/blob/3b95e7831b11b17627dff16843bc087bfca397ec/LICENSE)).

| Concern | Source, observed behavior and reuse limit |
| --- | --- |
| Identity/history/state | [`AcpSessionRuntime.ts:2170`](https://github.com/pingdotgg/t3code/blob/3b95e7831b11b17627dff16843bc087bfca397ec/packages/provider-acp/src/server/AcpSessionRuntime.ts#L2170) adopts IDs from `session/new`, negotiates load/resume. [`:1706`](https://github.com/pingdotgg/t3code/blob/3b95e7831b11b17627dff16843bc087bfca397ec/packages/provider-acp/src/server/AcpSessionRuntime.ts#L1706) isolates replay by session. Structured owned sessions; not a discovery implementation for someone else's TUI. |
| Permissions | [`adapter.ts:5990`](https://github.com/pingdotgg/t3code/blob/3b95e7831b11b17627dff16843bc087bfca397ec/packages/provider-acp/src/server/adapter.ts#L5990) handles ACP permission requests and returns offered option IDs. Reuse request identity and replay fencing; do not import its auto-approval policy. |
| Auth/skills | [`antigravityAuthSupport.ts:63`](https://github.com/pingdotgg/t3code/blob/3b95e7831b11b17627dff16843bc087bfca397ec/apps/server/src/provider/antigravityAuthSupport.ts#L63) isolates profiles and ambient auth environment. [`AntigravitySkills.ts:31`](https://github.com/pingdotgg/t3code/blob/3b95e7831b11b17627dff16843bc087bfca397ec/apps/server/src/provider/Drivers/AntigravitySkills.ts#L31) treats user and workspace skill roots differently. Do not adopt its credential ownership implicitly. |
| Roles/subagents | [`AntigravityAdapterV2.ts:84`](https://github.com/pingdotgg/t3code/blob/3b95e7831b11b17627dff16843bc087bfca397ec/apps/server/src/orchestration-v2/Adapters/AntigravityAdapterV2.ts#L84) projects native subagent tool calls without inventing child conversation IDs. [`:161`](https://github.com/pingdotgg/t3code/blob/3b95e7831b11b17627dff16843bc087bfca397ec/apps/server/src/orchestration-v2/Adapters/AntigravityAdapterV2.ts#L161) prefers resume to avoid history replay. Native subagents remain distinct from broker work. |

### Orca — `stablyai/orca`, MIT

Commit: `66e50e22ccb66d8eba12e06da2d6ae4967c32870` ([license](https://github.com/stablyai/orca/blob/66e50e22ccb66d8eba12e06da2d6ae4967c32870/LICENSE)).

| Concern | Source, observed behavior and reuse limit |
| --- | --- |
| Identity/restart | [`server-row-ownership.ts:69`](https://github.com/stablyai/orca/blob/66e50e22ccb66d8eba12e06da2d6ae4967c32870/src/main/agent-hooks/server/server-row-ownership.ts#L69) fences terminal ownership with retained launch-token hashes. [`agent-launch-prompt-delivery.ts:31`](https://github.com/stablyai/orca/blob/66e50e22ccb66d8eba12e06da2d6ae4967c32870/src/main/agent-launch/agent-launch-prompt-delivery.ts#L31) distinguishes handed-to-terminal, journaled and unconfirmed. Reuse these distinctions. |
| Transcript | [`session-scanner-antigravity-parser.ts:63`](https://github.com/stablyai/orca/blob/66e50e22ccb66d8eba12e06da2d6ae4967c32870/src/main/ai-vault/session-scanner-antigravity-parser.ts#L63) says logs lack cwd/model; [`:99`](https://github.com/stablyai/orca/blob/66e50e22ccb66d8eba12e06da2d6ae4967c32870/src/main/ai-vault/session-scanner-antigravity-parser.ts#L99) recognizes `created_at`, `USER_EXPLICIT/USER` input and `MODEL/PLANNER_RESPONSE`. This preview reader does not establish full tools/tokens coverage. |
| Permissions/state | [`hook-events.ts:3`](https://github.com/stablyai/orca/blob/66e50e22ccb66d8eba12e06da2d6ae4967c32870/src/main/antigravity/hook-events.ts#L3) uses distinct Antigravity events and `ask` for tool pre-hooks. [`antigravity-dialect.ts:9`](https://github.com/stablyai/orca/blob/66e50e22ccb66d8eba12e06da2d6ae4967c32870/src/main/acp/acp-dialects/antigravity-dialect.ts#L9) distinguishes ACP interaction questions from approvals. Screen composer parsing also has provider-specific code. |
| Login/runtime | [`antigravity-chat-sign-in.ts:98`](https://github.com/stablyai/orca/blob/66e50e22ccb66d8eba12e06da2d6ae4967c32870/src/main/antigravity/antigravity-chat-sign-in.ts#L98) owns a separate ACP sign-in flow. [`antigravity-acp-runtime.ts:9`](https://github.com/stablyai/orca/blob/66e50e22ccb66d8eba12e06da2d6ae4967c32870/src/main/antigravity/antigravity-acp-runtime.ts#L9) pins a separate binary runtime. Neither proves standalone `agy` login is reusable by ACP. |

Orca's Gemini adapter is separate (`src/main/gemini/hook-service.ts:32`); its comment about no
permission hook is not authoritative for Gemini 0.65.0, whose installed code had ToolPermission
notifications. Likewise its Gemini JSONL preview parser is not Antigravity's log contract.

## Decisions and alternatives

These local decision IDs supplement the existing `docs/design-decisions.md` register; they do
not supersede its work, receipt, claims or authority policies.

| Decision | Options and trade-off | Chosen direction; reversibility |
| --- | --- | --- |
| A01: product target | Keep old Gemini personal OAuth: cannot serve this account. Gemini API-key/enterprise: valid distinct audience, potentially paid. Antigravity: satisfies the newly authorized target. | Antigravity standalone; **irreversible public provider name** `antigravity`, executable `agy`. Keep Gemini quota identity distinct. |
| A02: interactive integration | TUI + opt-in identity hooks + optional status line: fits existing terminal product and unmanaged sessions, requires conservative screen handling. ACP: stronger structured owned interactions, separate runtime/auth/distribution. Print stream: simpler, insufficient approval protocol. Do nothing: does not deliver the feature. | TUI integration first, full requested scope retained. The accept-edits plus force_ask review candidate was falsified by an owned native file-write control; evaluate the separate ACP approval lane while retaining standalone discovery/history and all accepted scope. Adapter internals **reversible**; no headless-only completion claim. |
| A03: exact identity | Cwd/latest chat: cheap but wrong with two sessions. Open files: positive only while a descriptor exists. Hook/launch commitment: stable identity with local lineage checks, installation required. | Hook/launch commitment primary; kernel-open-file fallback only when positively proven. **Irreversible durable binding format**, version it. Missing proof means ambiguous/unreadable/no_record, never latest. |
| A04: state | Trust invocation/end hooks alone: misses prompts, background work and lost events. Screen only: brittle. Combine typed observations with source/freshness: more integration, honest unknowns. | Combine current screen/status with fenced hooks. **Reversible** rules; never equate PostInvocation with idle or waiting with permission grant. |
| A05: context injection | Rewrite global rules: affects other sessions. Replace base system prompt: changes provider behavior. Immutable launch pointer in supported SessionStart context/initial interactive prompt: preserves existing files, requires a loading receipt. | Immutable Squad snapshot + memory/rules pointer, scoped to launch. **Irreversible receipt meaning**; no native `--agent` substitution for Squad role identity. |
| A06: account/quota | Read keyring/token stores or automatically activate billing: broad authority. Consume CLI-produced numeric observations: no new account owner, may be absent. | CLI owns login; use passive status/quota evidence. Missing data stays unknown. **Reversible** source selection; paid fallback remains prohibited. |
| A07: durable native policy (implementation in progress) | Browser mode names or saved receipts cannot establish native behavior. A separate policy service would duplicate launch ownership. | One server-owned policy, bound inside the existing private launch transaction, shared by HTTP and broker; durable revocation preserves history and consumed input. Runtime composition binds the actual Project manager, launch store and bridge; stop/replacement withdraws matching inputs. A closed review strategy replaces the unspecified effective callback; distinct edits/full semantics and mandatory native hook evidence remain open. Native behavior rows remain inactive until measured. See [native policy contract](antigravity-native-policy.md) for the implemented storage stage and remaining activation gates. No scope or final acceptance reduction. |

ACP is a real alternative, not an undocumented `agy --acp` flag. The [official ACP registry entry](https://github.com/agentclientprotocol/registry/blob/main/antigravity-acp/agent.json)
currently lists a separate proprietary 1.4.1 runtime, `agy_acp_server.par`/`.exe`, for macOS,
Linux and Windows. The standalone binary tested here is 1.4.5. Root has now
pinned the official darwin-arm64 ACP archive, verified both Google signatures
and obtained one ACP v1 initialize response without model input. The following
session/new attempt timed out without a Session answer. These facts establish
neither personal authentication nor free/Credits settings nor standalone
resume compatibility. Before activation, validate separate authentication and resume behavior,
and preserve TUI discovery; do not treat a T3/Orca version pin as our compatibility receipt.

## Proposed boundaries and exact contracts

Core session rules stay in `internal/domain/session`; they import no process, HTTP, disk or UI
adapter. Add provider-specific parsing to adapters and pass typed observations through ports.
The app joins terminal/process facts, record identity and observations. HTTP projects these
facts through existing generated contracts and encrypted Cloud transport. No new relay service.

**Public additions, proposed names:** add `AssistantAntigravity = "antigravity"` to domain and
schema-generated Go/TS `Assistant`. Existing start/resume routes take `antigravity` in the
assistant segment. Browser requests still select a listed place/conversation, never a command,
raw path, auth token or arbitrary hook. Model/effort names require provider-specific validation;
do not reuse Codex `xhigh` admission without validating Antigravity's five-value help vocabulary.

Extend `StartAssistant` with optional `installed: boolean`, `version: string`,
`authState: "ready"|"required"|"unknown"|"unsupported_account"`,
`integrationState: "active"|"disabled"|"missing"|"incompatible"`, and
`capabilities: {start:boolean,resume:boolean,transcript:boolean,send:boolean,menus:boolean}`.
Keep `availability` as quota vocabulary; installation, auth and quota are separate facts.
Publish these through `/v1/places` and its existing Cloud operation. Use next API-level capability
admission rather than offering unsupported Gemini/Antigravity actions to older daemons.

Reuse `sessionId`, `binding`, `state`, `evidence`, freshness, activity and existing menu/receipt
fields. Add binding spelling `launch_hook` (a versioned Clawdline launch commitment), retaining
existing `open_file`, `no_record`, `unreadable`, `ambiguous`. Do not call Clawdline-owned state a
provider-native registry. No new enum value is required for ordinary idle/working/waiting/unknown.

**Local bridge only:** proposed CLI `clawdline provider observe antigravity --event <closed-name>`
reads bounded JSON stdin. Its private record is
`{version:1,launchId?,terminalId,pid,processStart,tty,conversationId,transcriptPath,
workspacePaths,event,observedAt,sequence,model?,agentState?,toolConfirmationPending?,quota?}`.
The helper obtains process ancestry itself; JSON-supplied PID is not authority. Managed launches
carry a file capability for their launch, separate from Squad actor capabilities. Persist only
whitelisted observations under the daemon's owned directory. Do not log raw hook stdin.
No remote/browser write route can manufacture these records.

For unmanaged sessions, validate the helper-to-shell-to-standalone process chain, process birth,
TTY/pane, supported executable identity and transcript confinement. Then join a conversation ID
to the exact matching terminal. Validate UUID, canonical path within the CLI brain root and
conversation directory agreement; reject symlinks/escapes and multiple candidate processes.
Open-file discovery is only an optimization after an actual descriptor is observed: this design
has **not** demonstrated that `agy` keeps the log open. A process table read failure is unknown,
not disappearance. A closed/reused PID invalidates liveness but does not erase history.

**Opt-in installation:** preview a named `clawdline` hook block and optional status-line wrapper,
then merge only owned keys with a before-digest check and atomic replacement. Do not install on
existing accounts merely because the CLI is present. Preserve unrelated hooks, settings and
status-line output. Uninstall only the matching owned block. Hooks disabled/missing must be
visible as such; screen/process discovery can continue, but unproved transcript identity and
automatic dispatch remain unavailable until positive evidence returns. A rejected installation
must not become an empty successful configuration.

**Permission ceiling:** 1.4.5 help lists `accept-edits`, `auto` and `plan`; this child has not
proved that any equals Clawdline's existing `ask`, `edits` or gate semantics. Capture the actual
default policy and tool/file behavior before mapping them. An unmappable request must fail with
`provider_permission_unsupported`, not inherit a wider ambient mode. Never substitute
`--dangerously-skip-permissions`, turn a verification gate into an ordinary writable child, or
assume that a pre-hook notification is a request awaiting confirmation. Keep optional ACP
permission mapping separate from TUI settings.

**State/send:** status confirmation or a recognized active menu beats working/idle observations.
Invocation events indicate work in progress; a model response does not finish the execution loop.
An explicit finish plus a current ready composer can establish idle; background task count remains
separate. Lost events, stale state, unknown screens or a pending queued input block automatic
briefing/reply. Preserve user drafts. Bind a menu answer to its exact terminal/conversation and
observed menu revision; reread before keys, refuse changed menus. Never convert a stale option
index into a current choice. Permission observations cannot synthesize approval.

Reuse the existing per-terminal send lane and idempotency receipts. Distinguish admitted,
written-to-terminal, observed-in-transcript and task-accepted states. The write syscall is not
model execution or child acceptance. On a timeout after bytes may have been sent, record an
uncertain receipt and reconcile against the same conversation; do not resend automatically.

**Transcripts/history:** implement an Antigravity log reader, not a renamed Gemini parser.
Capture real 1.4.5 records first, including tool input/result/error, repeated/revised records,
rewind/fork, cancellation and subagent output. The Orca source provides candidate input/response
spellings only. Keep stable provider record IDs where present; otherwise identity is a validated
file generation plus byte offset. Retain the existing bounded pages and byte cursor only after
proving append semantics. For a rewrite/rewind return `transcript_cursor_stale`; if updates target
earlier rows, add a versioned logical projection with optional `cursor: string`, `nextCursor:
string` and `revision: string` to `TranscriptPage`, rather than misusing numeric byte offsets.
Exactly one provider path resolver serves pages, facts, title, activity and history.

Tokens/context/model come from validated CLI status data or verified structured records, never
from Gemini's token layout or a guessed model price. Missing cost remains absent; never show a
subscription's token estimate as money actually billed. For cumulative counters replace the last
sample per conversation/generation, do not sum repeated status samples. Count tool IDs once.
Use provider event timestamps for activity, not filesystem touches. Existing page read and reply
budgets apply; new index/cache/spool bounds must be registered before introduction.

Provisional capacity entries for the implementation are `antigravity.hook_payload_bytes` (1 MiB
per input), `antigravity.event_queue` (256 observations with explicit overflow),
`antigravity.live_bindings` (512 entries, LRU only for inactive identities), and
`antigravity.observation_spool` (4 MiB total, 24-hour retention). A current-state observation is
eligible for an automatic send for at most 15 seconds and must still pass a fresh composer read;
expiry only removes authority, never proves idle. Register that age budget too. These are proposed
limits, not measurements. Reuse existing transcript/page budgets; if complete-log indexing is
needed, register its own disk/entry bound after measuring the captured format. Measure warmed
idle inventory with 0, 1 and 20 synthetic sessions: unchanged records must not be reparsed on
every poll, work must scale with changed records, and no provider API is called by inventory.

History scans only the known CLI transcript chain with bounded directory enumeration; do not
walk artifacts or account stores. Do not delete provider transcripts, borrow their retention
policy, or modify vendor SQLite/protobuf files. Cache loss triggers rebuild from provider facts.
After daemon restart, recover commitments as unverified and reread live process/screen evidence
before send/end/cleanup. `/clear`, conversation switch and fork invalidate current bindings and
pending sends until the replacement ID is proven.

**Squad, memory and rules:** keep existing snapshot publication, content hashes, launch IDs,
actor capability checks and skill-event receipts. Load the snapshot pointer and bounded Project
memory before the first model turn using supported context injection; initial interactive prompt
is the fallback if measured equivalent. A changed catalog after launch must not mutate the prompt
or role badge. Resume retains its original snapshot unless an explicit new launch requests a
change. Fail launch with `squad_context_unavailable` rather than falsely displaying a loaded role.

Official [rules](https://antigravity.google/docs/rules/) recognize directory `AGENTS.md` and
`GEMINI.md`; [skills](https://antigravity.google/docs/skills/) recognizes workspace `.agents/skills`
and surface-specific global/plugin roots. Reuse repository AGENTS and the Project memory port;
do not copy the person's global files. Unify must preview provider reads, collisions and limits,
preserve its current layout, and never overwrite an existing GEMINI file to manufacture parity.
Do not assume rule include syntax is identical to Claude's. Provider-native subagent activity may
be shown from verified records, but never fabricates a broker task, conversation ID or landing.

**Quota and platform:** add an independent `antigravity` numeric reading from CLI observations,
preserving unknown, stale, real zero and reset boundaries; connect it to the existing footer and
dispatch quota snapshot without inheriting Gemini's OAuth fetcher. Quota exhaustion pauses/refuses
new work with a typed reason and an exact reset if known; it does not select a paid backend.
Linux/macOS use existing managed tmux/control and platform process readers. Windows receives
history/config/schema support and explicit terminal capability refusals where the baseline has no
backend. Shipping native Windows start/control requires its own backend work and acceptance;
do not claim it from successful cross-compilation or borrow WSL evidence as native evidence.

## Implementation graph and file-level claims

Root should dispatch only coherent boundaries, in isolated worktrees, with installed Squad roles
and matching immutable skills. This architect used the installed decision-record skill; implementation
uses the installed implementation role/skills; the final verifier must not help build the feature.
Choose actual installed definition IDs from the live catalog, never invent role/skill IDs.
All candidate claims below have fewer than 32 literal paths. `NEW` paths are proposals, not current
files. Paired tests belong to the same owner. Root expands a claim explicitly if discovery needs it.

| Stage/owner | Literal file claims | Acceptance and dependency |
| --- | --- | --- |
| P0 root + architect | `docs/antigravity-cli.md` | Pin standalone version/help, sanitized screen/hook/status/transcript samples and auth outcome. Resolve the open schema questions below before hardening parser contracts. Independent design review can read immediately, without waiting for tests. |
| P1 provider contract owner | `internal/domain/session/session.go`; `api/v1/common.schema.json`; `api/v1/projects.schema.json`; `api/v1/sessions.schema.json`; `api/v1/routes.json`; `internal/contract/zz_generated.go`; `internal/transport/http/routes.go`; `web/console/src/cloud/carry-generated.ts`; `web/contract/src/generated.ts`; `internal/app/cloudops/zz_decode_generated.go`; `skills/clawdline/routes.md`; `skills/clawdline/routes.zh-TW.md`; `skills/clawdline/capacity.md`; `skills/clawdline/capacity.zh-TW.md`; `skills/clawdline/refusals.md`; `skills/clawdline/refusals.zh-TW.md`; `internal/adapters/projects/launch.go`; `internal/adapters/projects/launch_test.go`; `internal/adapters/projects/launch_antigravity_test.go` (NEW); `internal/transport/http/projects.go`; `internal/transport/http/start.go`; `internal/transport/http/assistants_test.go` | P0. Generate Go/TS from schema together; full generated output set is checked in `tools/contract-gen/main.go:140`. Exact resume uses `--conversation`; wrong IDE refused; unsupported fields refused; two original providers' argv unchanged. No hook permission escalation. |
| P2 identity/bridge owner | `internal/adapters/transcript/host.go`; `internal/adapters/transcript/antigravity_identity.go` (NEW); `internal/adapters/transcript/antigravity_identity_test.go` (NEW); `internal/adapters/process/ps_unix.go`; `internal/adapters/process/ps_windows.go`; `internal/adapters/process/antigravity_test.go` (NEW); `internal/adapters/terminal/tmux.go`; `internal/adapters/terminal/farewell.go`; `internal/adapters/terminal/antigravity_test.go` (NEW); `internal/app/ports/antigravity.go` (NEW); `internal/adapters/antigravity/observe.go` (NEW); `internal/adapters/antigravity/install.go` (NEW); `internal/adapters/antigravity/observe_test.go` (NEW); `internal/adapters/antigravity/install_test.go` (NEW) | P1. Two concurrent sessions in same cwd bind distinct IDs. Replay/reused PID/foreign path rejected. Disabled hooks survive unchanged. CLI entrypoint/wiring is integrated by root in P5, so this worker delivers bounded adapter APIs, not an unreachable feature. |
| P3 records owner | `internal/adapters/transcript/antigravity.go` (NEW); `internal/adapters/transcript/antigravity_test.go` (NEW); `internal/adapters/transcript/info.go`; `internal/adapters/transcript/movement.go`; `internal/adapters/transcript/models.go`; `internal/adapters/transcript/ledger.go`; `internal/adapters/transcript/ledger_antigravity.go` (NEW); `internal/adapters/transcript/ledger_antigravity_test.go` (NEW); `internal/transport/http/transcript.go`; `internal/transport/http/transcript_antigravity_test.go` (NEW); `internal/adapters/projects/antigravity_history.go` (NEW); `internal/adapters/projects/antigravity_history_test.go` (NEW) | P1; P2 identity interface fixed. Exact captured records render tools/results once, cumulative usage does not inflate, older pages retain ordering, rewinds invalidate cursors, incomplete records are typed. P1 owner lands any resulting schema changes before generation. |
| P4 interaction/Squad owner | `internal/domain/session/screen.go`; `internal/domain/session/menu.go`; `internal/domain/session/antigravity_test.go` (NEW); `internal/app/orchestrator/composer.go`; `internal/app/orchestrator/dispatch.go`; `internal/app/orchestrator/language.go`; `internal/app/orchestrator/brief.go`; `internal/app/orchestrator/squad_launch.go`; `internal/app/orchestrator/antigravity_test.go` (NEW); `internal/adapters/projects/launch_memory_test.go`; `internal/adapters/projects/launch_persona_test.go`; `internal/adapters/projectfiles/unify.go`; `internal/adapters/projectfiles/unify_test.go` | P1/P2. Current permission/question/login/trust/picker screens never receive a normal prompt. Child task accept/finish and immutable role/skill receipts survive restart/resume. Provider-native tasks remain separate. P1 owns launch.go; integrate context request changes serially. |
| P5 root integration/capacity | `cmd/clawdline/main.go`; `internal/transport/http/server.go`; `internal/transport/http/orchestrator_wiring.go`; `internal/transport/http/provider_quotas.go`; `internal/adapters/providerquota/service.go`; `internal/adapters/providerquota/service_test.go`; `internal/domain/capacity/register_antigravity.go` (NEW); `internal/domain/capacity/testdata/register.golden.json`; `internal/transport/http/capacity_measures.go`; `internal/transport/http/capacity_measures_antigravity.go` (NEW); `internal/transport/http/testdata/capacity_measures.golden.json`; `docs/provider-quotas.md`; `docs/cross-platform.md`; `docs/architecture.md` | P2/P3/P4 adapters. Register hook payload bytes, event concurrency, spool bytes/age, binding/cache entries and expiry, scan files/bytes/time and any logical index budget. Guard must go red before registration, then both register and HTTP measurement snapshots pass. Keep existing shared quota budgets where sufficient. Coordinate regenerated capacity guides with P1. |
| P6 UI owner | `web/console/src/legacy/start-bridge.ts`; `web/console/src/session/started-row.ts`; `web/console/src/session/started-row.test.ts`; `web/console/src/session/history.ts`; `web/console/src/session/history.test.ts`; `web/console/src/session/sender.ts`; `web/console/src/session/sender.test.ts`; `web/console/src/pages/projects/project-unify.ts`; `web/console/src/pages/projects/project-unify.test.ts`; `web/console/src/provider-quota/model.ts`; `web/console/src/provider-quota/model.test.ts`; `web/console/public/catalogs/en.json`; `web/console/public/catalogs/zh-Hant.json`; `web/console/src/session/antigravity.e2e.ts` (NEW) | P1 schema; may build fixtures while P2–P5 proceed. Local and Cloud list/start/resume, conversation/tools, auth/unknown/menu, role badge, memory/unify and footer verified. Audit legacy renderers/icon/settings/default-model consumers and expand claims before editing; this list is not permission to miss those surfaces. |
| P7 independent verifier | Read complete integrated feature; only task-owned report artifacts | One consolidated design/security/concurrency/privacy finding set before correction. Original owners fix it. Root validates exact integrated tree and public Cloud bundle, performs dev-install acceptance, records landing and cleans only proved residue. |

P2 and P3 may run in parallel after their identity/record contract is fixed; P4 can prepare screen
fixtures then. P5/P6 integrate after their dependencies, not by sharing dirty trees. Claims that
overlap remain serialized. No worker edits generated contracts independently of P1. Before final
dispatch, root resolves generated outputs and actual wiring/catalog paths against its current
base. Generated outputs above were read from the generator, not inferred from their names.

## Acceptance and failure injection

All synthetic fixtures use invented IDs/content and an empty daemon configuration. Live rows below
require a separate real-run receipt with binary version, OS/backend and exact observed result.
No live model request is authorized by this design. Potentially priced calls require specific
approval and a request/cost cap before root runs them; use local captures and protocol fixtures
first. A login receipt, CLI help and a successful model turn establish different things.

| Test/failure | Required falsifiable outcome |
| --- | --- |
| Executable collision, missing CLI, unsupported help | IDE-shaped `agy` refused; standalone 1.4.5 accepted; missing/install/auth/quota states distinct. Probe never opens a GUI or logs in. |
| Two new/resumed sessions, same repository | Distinct PID/birth/TTY/conversation/path; message/tool from A never appears in B. Repeat with managed + unmanaged and two hooks disabled. Unknown remains explicitly unbound. |
| Clear, switch, fork, reused PID, stale hook | Old binding cannot authorize send, menu reply, close or role attribution; new ID needs proof. Duplicate/reordered observations cannot regress the current generation. |
| Daemon offline/restart, full spool, slow consumer | Hook exits within its registered budget; no model-loop blockage; typed dropped/incomplete state, bounded retry, no automatic prompt resend. Recovery does not promote stale state to current. |
| Trust/login/tool permission/question/model menu/draft | Normal text withheld. Exact current offered selection only; changed menu rejected. No default option is submitted. Existing draft survives. |
| PostInvocation, Stop with background tasks, aborted turn | No premature idle or child completion. Background tasks distinct; error/unknown preserved. Same input screen tested against near-miss prompt/menu layouts. |
| Partial/malformed/huge log; rewrite/rewind; EN/CJK | Bounded reader returns typed incompleteness, no panic, no false empty success; no duplicate messages/tool calls/tokens; stale cursor rejected; localization does not alter parser correctness. |
| Missing tokens/model/cost/context; repeated status | Omitted metrics stay absent, true zero stays zero; cost not fabricated; cumulative totals replace rather than add. Activity uses real event time. |
| Immutable role/skills; catalog edited after launch | Snapshot hash/version remain fixed. Correct read/applied receipts, terminal/conversation actor capability and original resume role; forged/foreign launch pointer rejected. |
| Child dispatch and lifecycle | Task accepted by secret, correct worktree/claims, result finish and root landing remain existing broker facts. Quota exhaustion and unknown auth never select paid alternatives. |
| Opt-in config edit, conflicts, uninstall | Existing third-party hooks/status command byte-preserved, digest conflict refuses, malformed config not replaced; disabled integration is not silently re-enabled. |
| History/retention/restart | Resume selects listed UUID, never latest; log removal not mistaken for idle; cache rebuild bounded; provider-owned files never deleted; restart marks old liveness unverified. |
| Local + Cloud + platform | Actual encrypted Cloud wire preserves assistant/identity, state/freshness/menus/roles and selected machine. API-level mismatch disables actions. Linux native and Windows refusal/capability rows tested independently from macOS. |
| Claude/Codex regressions and capacity | Existing focused suites and local gate green; no new third-provider fallback. `TestEveryBoundIsRegistered`, measured capacity and both golden snapshots cover every new bound; shrink limits to force rejection. |

## Measured native contracts after the design review

The root inspected the private native evidence and independently reran its local integrity
checks, including three negative controls. These observations apply to the installed standalone
1.4.5 binary, SHA-256 `b89677b741d8d9d38cf0a6911ab468fac629158194039904cbfd645fdb897554`.
They supersede the provisional native path and event assumptions above.

- The free personal account completed login. Two later probe processes displayed Starter quota
  and their own configuration panels showed AI Credits off before model input. API-key,
  Vertex/ADC and paid fallback environments were excluded. No invoice was queried; the NTD 0
  boundary is based on that free-only gate, not account billing telemetry.
- The observed transcript is `brain/UUID/.system_generated/logs/transcript_full.jsonl` beneath
  the CLI's state directory. Native events carry `conversationId` and `transcriptPath`.
  Initial input emitted SessionStart; exact UUID resume emitted PreInvocation without
  SessionStart. An idle launch alone did not supply a native identity event.
- Under an existing conversation-local printf grant, `ask` allowed the exact command without
  another question. `force_ask` displayed the permission menu again. The probe selected the
  conversation-local scope and allow-once scope explicitly; it never persisted a global grant.
- A deliberate PreToolUse hook exit 23 produced a native ERROR result. No successful tool output
  or PostToolUse was recorded for that attempt. Grant persistence across restart was not proved,
  so this does not establish failure behavior with a known-live cached grant in that process.
- A nonempty-Target edit showed its exact diff, was explicitly approved, produced native DONE,
  and changed the fixture's bytes. The earlier INVALID edit remains a failure rather than
  evidence of successful editing.
- An unchanged synthetic context was attributed to PreInvocation in eight native SYSTEM_MESSAGE
  records and was reflected in initial and resumed responses. Separate workspace instructions
  had a distinct marker. This proves the native nested injection shape; it does not prove the
  product's immutable Squad snapshot, project memory journal or loaded badge.

The latest bounded probe accounted for four input attempts, three delivered USER_INPUT records,
eight PreInvocation events and eight planner responses. One counted send found its private
server already stopped by the 45-second supervisor; it was not retried or refunded. Both provider
processes and the private server were confirmed stopped. These counts do not describe hidden
vendor transport retries. Integrity checks rejected fabricated hook success, unchanged file bytes,
and missing resumed context; 43 sanitized artifact hashes matched.

## Remaining evidence root must gather

1. **Product composition:** wire native process/Unix-peer identity, local-only hook ingress,
   durable sequencing, launch capability, workspace/history binding and safe startup before the
   first native identity event. A requested UUID remains provisional until verified observation.
2. **Permission coverage:** `force_ask` is required for the measured review ceiling; `ask` is
   insufficient. Reuse measured configured-hook failures rather than treating them as proof
   for removed/disabled hooks. Verify mandatory callback persistence, competing hook/rule
   precedence, all actual tool contracts, local service outage and the complete tool coverage
   before admitting generic broker permission modes. The bounded 2026-10-23 probe stopped on
   an actual `write_file` name/argument difference; it did not reach removal/disable cases.
   See the [enforcement amendment and native result](antigravity-native-policy.md#a07-enforcement-amendment-and-bounded-native-result). Retain `provider_permission_unsupported` until those gates pass.
3. **Product interaction and context:** exercise the actual paste/menu lane and receipts, edit
   permission and other native pickers, cancellation, immutable role/memory publication and exact
   resume. Distinguish context prepared, delivered and observed before showing a loaded badge.
4. **Transcript and status:** carry native generation signatures through local and encrypted
   Cloud history, prove replacement/truncation behavior, large-file bounds and exact usage
   attribution. Numeric quota/context data remains unknown unless the CLI produces it.
5. **Independent acceptance:** after assembly, review the actual product and verify desktop,
   phone, light/dark themes, English/Traditional Chinese, Cloud and brand consistency. Name
   platform capabilities separately; cross-compilation is not Linux/Windows runtime acceptance.

Implementation and bounded free CLI verification can proceed under the person's existing authorization.
These are technical evidence dependencies, not a request for another blanket design approval.
Any actual conflict with the person's scope, account choice or billing permission belongs to the
person and must be presented explicitly rather than resolved by reducing the feature.

## Not building in this change

This document contains no runtime implementation. The plan does not add Antigravity's own remote
control, replace Clawdline encryption, import account vaults, migrate Gemini settings or credentials,
enable paid teamwork/credits, delete provider histories, or port an IDE. These are unnecessary to
the authorized integration. ACP runtime distribution is an alternative requiring its own exact
compatibility evidence, not a hidden prerequisite or excuse to omit interactive TUI support.


## Provider-specific launch defaults

The feature branch's `nextconfig.DefaultEffort` selects the saved preference by
explicit provider. Antigravity reads `antigravity_default_effort` through its own
`low|medium|high|xhigh|max` validator; Codex keeps its existing validator and key.
Absent, malformed and unknown-provider values remain empty rather than falling
back to another provider. Both console and broker composition read the current
configuration for each launch, so saving a preference needs no daemon restart.

`Starter`, child spawning and handoff opening pass the provider into this shared
selection. A child with an explicit effort keeps that value. A resumed Session
does not read a new machine default; its original prepared context remains the
launch preparer's responsibility. These settings do not authorize execution:
Antigravity's managed context, verified executable, permissions and native
identity gates must still admit the launch. Passing preference-selection tests
is not acceptance of the complete native start, resume or handoff experience.


### Captured ACP message and native read-tool events

A bounded native ACP 1.4.1 run completed personal Google authentication,
created a session, read a task-owned synthetic file and returned its expected
marker with `end_turn` in 29.727 seconds. It sent one model prompt and four client
RPC requests in total. Starter Quota and `useG1Credits: false` were checked after
session creation, before that prompt. There was no client retry, paid API or
fallback. All owned process groups exited and cleanup reported no errors. This
configuration evidence does not substitute for a provider invoice; internal
provider HTTP requests and model-invocation counts were not measured.

The native stream contained three `view_file` starts and three status-only
completion updates, four context snapshots, and one message chunk. The agent
attempted three paths, including two that differed from the fixture's absolute
path; one prompt must not be described as one filesystem attempt. All three
completion events followed the visible text chunk. A renderer must keep matching
tools by `toolCallId`, merge only fields present in the delta, and continue
processing events after text appears. The capture supplies no tool-output content;
`completed` alone is not proof that each attempted path returned file contents.

The small decoder fixture `internal/adapters/antigravityacp/testdata/native-read-1.4.1.jsonl`
retains only explicitly checked native fields, fixed synthetic paths, a fixed
synthetic reply and replacement session/tool IDs. Command-list metadata and
unrelated fields are omitted. The local comparison checked every retained field
before writing that fixture; no raw account wire record is stored in the repository.
`TestSessionUpdateNativeRead141` checks the actual order and partial completion
shape. `usage_update.used/size` replace context occupancy and do not accumulate as
billed input/output tokens. These are native script and decoder results; the
product's ACP process owner, message flow and admin mobile entry still need
integration and end-to-end acceptance.

For the initial mobile pilot, the existing admin/operations-access UI decision
controls entry visibility. The pilot does not introduce a new backend admin
entitlement service or change ordinary session authorization. Other accounts do
not receive the pilot entry. Paid API use continues to require separate approval.


The subsequent whole-tree gate exposed an older hook-service refusal race, not
an ACP decoder failure: peer verification could reject before reading the
request, then close both socket halves immediately after writing the refusal.
A real Unix-socket regression reproduced `antigravity_hook_disconnected` before
the fix. Refusals now use the capacity branch's existing half-close and bounded
drain, under the same request deadline and frame limit. The new regression and
the observation/tool-policy ordering tests passed 25 repetitions after the fix.
No authority, grant, timeout or capacity value changed. The earlier full gate
remains a failed run; the corrected exact candidate needs a fresh green gate.


### Gemini preview display and readiness

The Console calls this assistant **Gemini** and bundles the official four-color
Gemini favicon from `https://gemini.google.com/`. The assistant picker, Session
list badge, transcript and information views use the same local asset.
The transport identifier remains `antigravity`; changing a product label does
not migrate conversation identities or change the underlying official CLI.

An admitted new Session has no introductory sign-in notice, and its composer
does not display a routine availability-check sentence. A specific sign-in
refusal still has its own recovery guidance. Current observations of the same
execution no longer cancel an outstanding readiness read: the request belongs
to the selected machine, execution and transport. New observations refresh
expired facts without starting concurrent reads; switching executions, losing
connectivity or hiding the page still invalidates the old authority.

The changing-observation browser regression uses a delayed capability response
and continuing Session snapshots. The old bundle remains unable to edit; the
fixed bundle becomes editable and sends once to the selected Session. Cloud
Project reads report a content-free elapsed time as `cloud.places.read`, so
chooser delays can be measured separately from provider startup.


### Native bindings across Cloud presentation

Cloud rows carry two distinct identities: the relay machine/Session routing pair
and the native conversation binding. Keep the native binding as `sessionBinding`
when constructing the routing pair and when joining an authenticated pinned
presentation to its exact execution. A `protocol` binding from that execution's
Antigravity presentation identifies the owned backend even on machines whose
older status projection omits it. A title or a Session ID prefix grants nothing.

An owned native conversation uses its idle/busy state for input. It does not
fetch the assistant inventory or require a terminal observation before composing.
Text goes through the existing send route; the server still checks the exact
execution, owned process, account and free mode immediately before writing.
Terminal-backed integrations retain their separate screen and menu observations.


An owned native user echo confirms delivery but does not complete the model
reply. Retain the existing bounded follow window after that echo and reuse a
native structured page only for the existing short read-coalescing window.
Native pages do not have a terminal line whose movement invalidates the longer
terminal transcript cache. This follows reads only; it never resends the prompt.
