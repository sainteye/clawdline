# Installed agents: bounded qualification

This record is **incomplete native acceptance**, not a claim that either vendor
completed the Bot flow. The only requested native qualification command was
rejected by automatic approval review before it executed. No workaround or
native retry was attempted. Integration, Board acceptance and deployment remain
with the owning root.

## Fixed inputs and authorization boundary

The inspected Clawdline source is
`fc00d608991cf75ac19c41b92031b3008dca53a5`. The read-only Codync reference is
`9f67eb09084db61b3cca9c84a11bc03df9de29b7`, independently checked with
`git rev-parse HEAD`. Relevant upstream boundaries are `host/src/chat/context.rs`
(context and memory revision), `host/src/agent/bot/launch.rs` (mode, settings and
session lifecycle), and `host/src/agent/own_memory.rs` (native memory). No upstream
build or program was run.

The task authorizes at most four native prompts total: an initial and a follow-up
for each installed agent, through the production Host, SQLite and
`projectassistantacp.InstalledProvider`. It prohibits separate usage-priced API
calls, automatic native retries, CLI upgrades, global settings changes and use
of an existing Session. The supplied adapters are the repository's locked
dependencies, rather than globally installed replacements.

Automatic approval review rejected the opt-in command because it considered the
installed CLI model usage insufficiently covered by a provider-specific monetary
cap. The briefing explicitly authorizes native CLI qualification without separate
quota approval, but the reviewer did not accept that basis. The next owner must
resolve that approval boundary once before running the native command. This
record does not infer pricing, account access or available quota from a local
version probe.

## Compatibility matrix

Local version probes ran on 2026-11-15 UTC (2026-11-16 Taiwan time), on
Darwin/arm64 with Go 1.27.1 and Node 24.1.0. A version response establishes local
installation only.

| Boundary | Claude Code | Codex |
| --- | --- | --- |
| Installed CLI response | 2.1.325 | 0.166.0; a sandbox PATH-alias warning was also printed |
| Supplied ACP adapter | 0.106.0 in bundled lockfile; private installation not executed | 2.6.0 in bundled lockfile; private installation not executed |
| Production factory | `InstalledProvider` routes to `installedClaude` | `InstalledProvider` routes to `installedCodex` |
| Auto/default mode, source inspection | Native `auto`; existing adapter fallback to `acceptEdits` on an unsupported model is retained | Public Auto maps to native `agent` |
| Native memory, source inspection | Auto memory and dreaming disabled per launch; existing native settings/login remain authoritative | Existing native home/login retained; `codex features list` must report `memories=false`, otherwise typed `memory_isolation_unverified` |
| MCP descriptor/runtime tool use | Not observed natively | Not observed natively |
| Empty memory and durable nonce | Not executed | Not executed |
| Approved memory and context follow-up | Not executed | Not executed |
| Native resume | Production capability is qualified; this run did not exercise it | Intentionally `Resume=false`; follow-up must use fresh-session filtered handoff |
| Routines | Not exercised in this run | Main-session routines are unsupported while native resume is unqualified |
| Native permission metadata/four kinds | No native permission request observed; no fabricated offer or answer | No native permission request observed; no fabricated offer or answer |
| Native opens / prompt writes in this task | 0 / 0, blocked before process execution | 0 / 0, blocked before process execution |

These are counts at Clawdline's native session/prompt boundary, not a claim of
zero internal model calls during a successful qualification. Provider-internal
requests, retries and billing were not observed. No direct paid API call was
made, and no measured provider billing amount is available.

The production code supports tool descriptors for chat, work and memory;
`sessionParameters` carries the owned HTTP MCP descriptor and agent-specific
system prompt. This is source evidence only. Existing
[permission evidence](project-assistants-native-permissions.md) records inspected
adapter envelopes and scripted cancellation/fencing controls. Domain option
kinds are `allow_once`, `reject_once`, `allow_always`, and `reject_always`; their
existence in source is not a native observation or approval to create a standing
rule. Actual permission action metadata must come from an actual native request.

## What was verified without models

The existing
`TestOwnedMCPReachesRealStoreBrokerAndDurableChatWithoutRegistration` passed:
an owned Go ACP peer followed the real HTTP MCP descriptor into the production
Host, real SQLite, Broker and isolated fixture worktree, with one durable chat
result. This is a scripted transport/control receipt, not vendor qualification.

Its first restricted-sandbox execution failed with an unknown execution outcome.
A minimal independent census probe then showed `/bin/ps` was denied. The same
read-only census succeeded with the required permission, and the same scripted
Host test passed with that permission. Neither attempt launched a vendor CLI or
consumed the native prompt allowance. Unknown execution fencing was not weakened.

`TestInstalledAgentsNativeQualification` compiles and skips without its exact
task-specific flag. The ordinary `tools/check.sh` was run once with both native
qualification variables explicitly unset and reported every selected check
passed. The later focused scripted control checks the harness's independent
query-only SQLite readback and distinguishes the accepted user-input fence from
the new native execution fence. It passed together with
`TestHostDefaultOffAndMetadataNeverInitializeProvider`; the native test skipped
in that same focused run. The final two files also passed the scoped privacy
check. The ordinary gate preceded that final assertion/document refinement, so
the integrating root still owns the final-tree commit gate.
Complete CI and native acceptance are still pending.

## Rerunnable native route

Only after the approval boundary is resolved, use a new, nonexistent task-owned
state directory and a task-owned temporary directory/cache:

```sh
CLAWDLINE_NATIVE_QUALIFICATION_07DDAF71=four-prompts-once \
CLAWDLINE_NATIVE_QUALIFICATION_ROOT="$TASK_WORK/native-state" \
TMPDIR="$TASK_WORK/tmp" GOCACHE="$TASK_WORK/go-cache" \
go test ./internal/adapters/projectassistanthost \
  -run '^TestInstalledAgentsNativeQualification$' -count=1 -timeout=45m -v
```

`TASK_WORK` must name the authorized task's private working directory. Do not run
this command from CI or put its flag in a persistent environment. The test
refuses an existing state root. SQLite and synthetic repositories are fresh Go
test directories under `TMPDIR`, outside the adapter state root. The normal Host
limits come from the production capacity register: a ten-minute prompt and
five-second cleanup, rather than the fault fixture's short budgets.

For each agent the test creates a stored Bot in Auto/default mode with ask-mode
Host permission handling. The initial prompt reads empty Bot memory and sends
one fixed nonce. A literal approved memory entry is saved through the human
Service endpoint. The follow-up must read that entry and combine its token with
a token present only in the prior conversation. Claude must follow its supported
resume path; Codex must open a different fresh native session. No standing rule
or permission answer is created.

The observer delegates to the actual installed provider and Host-bound tools.
It records session opens, prompt writes, tool reads and sends, refuses a repeated
open for the same binding (including automatic resume fallback), and caps opens
and prompt writes independently at four. Polling observes durable state only; it
never resends. A failed, unknown, cancelled or permission-waiting outcome stops
the whole run before another agent is attempted.

After each successful turn a separate query-only SQLite connection reopens the
source data. Assertions require exactly one accepted user send, one visible
nonce, its durable receipt, full Project/Bot scope, the accepted input fence and
its creation event, and the assistant's actual execution fence. The first
admission initializes a binding before execution advances its generation;
later inputs must preserve the submitted fence. The test also checks empty then approved
memory, native session IDs and the factory's unchanged resume capability.

Private evidence includes capabilities, opened requests (without tool authority
or credentials), prompt/context snapshots, MCP observations and reopened durable
aggregates. Copy these JSON files out of temporary work before task cleanup.
Preserve typed failures and the original log; do not acknowledge/resend an
unknown turn or relabel setup/source inspection as native acceptance.

No full fallback, teams, Cloud, physical phone, production UI or public Release
acceptance is established here. Original line avatars and public Release-hidden
behavior were outside the write scope and were not changed.
