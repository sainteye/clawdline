# Installed agents: native qualification

The common Bot flow has real installed-agent evidence for Claude Code and
Codex. A first Codex follow-up exposed a context-loss defect: every execution
opened a fresh native session, whose provenance-filtered handoff contained
approved memory and a history bookmark, not the prior conversation text. The
second native prompt therefore could not produce its requested conversation-only
token. This was an assertion failure after a completed turn, not a quota,
authentication or transport failure. The original receipt and observations are
retained privately; no unknown turn was acknowledged or resent.

## Verified inputs and results

The initial run used source `284034701e4fe8eb7f22e8bf2a5b50341b46b892`.
It sent two prompts to each agent. Claude passed both; Codex passed its first
turn and failed the follow-up assertion. The correction was then exercised
with two new Codex prompts on the candidate, through the production Host,
SQLite, owned HTTP MCP and installed ACP provider. No existing Bot, native
Session or user conversation was used.

| Boundary | Claude Code | Codex |
| --- | --- | --- |
| Installed CLI observed on 2026-11-16 | 2.1.325 | 0.166.0 |
| Supplied, digest-sealed adapter | 0.106.0 | 2.6.0 |
| Common-flow native prompts | 2, both passed | 4 including the original failed follow-up assertion; corrected pair both passed |
| Empty then approved Bot memory | Two actual `read_memory` calls | Two actual reads in each pair |
| Fixed visible MCP replies | Two, independently reopened from SQLite | Two in the corrected pair; one in the original pair |
| Cross-process native session identity | Same ID; fresh then resume | Same ID in the corrected pair; fresh then resume |
| Conversation-only prior token | Recovered on follow-up | Recovered on corrected follow-up, without adding it to Bot memory |
| Native memory | Auto memory/dreaming disabled by launch settings | Existing CLI reports `memories=false`; otherwise admission refuses |
| Native configuration/login | Existing account and settings retained | Existing account and settings retained |
| Native permission offers/four option kinds | Not exercised by this read/send-only batch | Not exercised by this batch |
| Routines, fallback switching and team flows | Not qualified by this batch | Not qualified by this batch |

Across the two batches: six native opens, six prompt writes, six memory reads
and five successful MCP `send_message` calls. These are Clawdline boundary
counts. Provider-internal model requests, retries and billing were not measured;
no zero-cost claim is made. The person explicitly authorized both installed
native CLIs before execution. No direct paid API or additional purchase was made.

The first native batch establishes Claude's result on the named base. The Codex
correction used an isolated Go compile overlay with its resume capability on;
normal source retained `Resume=false` until that actual round trip passed.
The promoted Codex provider file is byte-identical to the compiled probe, with
SHA256 `a0a718e46dbbe119abe7828e395f775bcfd5bb5193411c517e025d1d1ae22de9`.
The two unchanged Claude prompts were not rerun merely to inflate coverage.

## Resume follows the adapter and source ownership

Pinned Codync `9f67eb09084db61b3cca9c84a11bc03df9de29b7`,
`host/src/agent/bot/launch.rs`, chooses `session/load` when initialize advertises
`loadSession`. The supplied Codex adapter implements that route using native
thread resume. Clawdline's client already checks the advertised capability,
loads the requested native ID and rejects an answer naming a different ID.
It does not turn loaded history notifications into a new turn's messages.

The installed Codex provider now uses feature-owned native-ID records before
loading and records a newly opened ID only after successful initialization.
Records bind the agent, full Bot/Project identity, conversation, forgotten
revision, instructions and routine slot. A new execution generation or added
memory keeps the same owner; forgetting or changed instructions gets a new
epoch. Existing Claude identity bytes and record policy are retained.

A new scripted red/green test exposed the old shared agent identity before the
correction. Negative controls cover copied cross-agent records, foreign Project,
conversation, forgotten revision, instructions and routine slots. Unrecorded,
forgotten, changed-instruction, copied-agent and revoked Codex loads are refused
before adapter setup or native execution. No new queue, history limit, wire
protocol or conversation replay was introduced. A genuinely missing native
session still uses the existing typed `resume_unsupported` recovery; retained
transcripts never bypass memory provenance or forgetting.

## Rerunnable common native route

The test is opt-in and skipped by ordinary CI/checks. Obtain native-use
authorization and choose a nonexistent private state root plus owned temporary
and cache directories:

```sh
CLAWDLINE_NATIVE_QUALIFICATION_07DDAF71=four-prompts-once \
CLAWDLINE_NATIVE_QUALIFICATION_ROOT="$TASK_WORK/native-state" \
TMPDIR="$TASK_WORK/tmp" GOCACHE="$TASK_WORK/go-cache" \
go test ./internal/adapters/projectassistanthost \
  -run '^TestInstalledAgentsNativeQualification$' -count=1 -timeout=45m -v
```

Each run caps native opens and prompt writes at four independently, even when
broader native testing is authorized. Failure, unknown outcome, cancellation or
a real permission wait stops that batch; no automatic native retry occurs.
`-run '^TestInstalledAgentsNativeQualification$/^codex$'` selects the corrected
Codex pair while retaining both installed-tool preflights.

Each Bot starts in Auto/default mode. The first prompt reads empty memory and
sends a nonce while retaining a conversation-only token. A literal approved
memory entry is saved through Service. The second prompt must read it and send
the nonce plus both tokens exactly once. Separate query-only SQLite connections
verify each accepted user input, receipt, source scope, admission fence,
creation event and actual execution fence. Open observations require the same
native ID across two processes and the second request to be a resume.

Private artifacts retain capability versions, Open/Send requests, tool reads,
reopened aggregates, original failures, source manifests and the exact overlay.
The scripted durable-readback control remains a control, not native evidence.
These results do not establish production Cloud, physical-phone, cross-platform,
permission-offer, routine, team or fallback acceptance. Those remain separately
tracked; no unsupported behavior is relabeled as verified.
