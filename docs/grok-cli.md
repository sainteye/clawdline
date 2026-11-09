# Grok Build coding Sessions

This integration uses the official **Grok Build** coding agent, through
`grok agent stdio` (ACP over JSON-RPC pipes). It does not implement a second
coding harness around the xAI chat API. Grok owns file edits, shell tools,
repository instructions and its native tool permission offers.

This page describes the implementation candidate. Real account authentication,
coding acceptance, independent end-to-end review and deployment are still
pending; passing protocol peers does not establish those results.

## Installation and account

Install from [xAI's official source](https://x.ai/build). The official
[CLI reference](https://docs.x.ai/build/cli/reference) documents `grok login`,
`grok models` and `grok agent stdio`. Finish the CLI authorization, including
its browser approval and return to the waiting CLI. Signing in to the website
alone does not prove that the CLI received credentials. Clawdline never asks
the person to put a password or token in a Session message.

The inspected macOS ARM64 binary reports `grok 1.0.77 (50ca579fa996)` and its
xAI Developer ID signature passed `codesign --verify --strict`. Version text
is not the compatibility gate: discovery requires the coding CLI's version
and help fingerprint. Before opening a conversation the native process must
also advertise ACP v1, `grokShell`, an agent version and the capabilities used.
An unrelated executable named `grok` is not accepted.

Clawdline uses the native account directory (`GROK_HOME`, otherwise `.grok`
under the user's home) without copying credentials into its own state. It
removes inherited API keys and other providers' identities from the child's
environment. Only an advertised `cached_token` method is attempted. Successful
RPC transport is insufficient: returned auth metadata must confirm `Oidc`
and that another backend is not billing the request. API-key and unknown
account modes are refused before session creation or model input. An advertised
account gate is also a refusal. This is not proof that subscription usage is
unmetered: native usage and extra-credit policy still belong to the account.

## Supported surface

- Session creation, text input, streamed assistant and tool output, follow-up,
  saved transcript and exact native-conversation resume use an owned backend.
  They do not type into a TUI or fall back to Gemini.
- Model selection accepts current native offers only. Omit a model to retain
  the native default. `grok-build` is a product label, not an invented model ID.
- Native tool permissions use the existing live permission endpoint and exact
  option IDs. The current Session, operation fence and authorization are
  checked again before writing a reply; expired offers are not replayed.
- Dispatch uses `assistant: "grok"`, the existing task/worktree lifecycle,
  private native address and separate actor capability. The worker is bound
  before its first briefing. Duplicate dispatch keeps the existing task.
- The ordinary Session starts with explicit supervised permissions. A dispatch
  `ask` does the same; explicit `full` uses Grok's `--always-approve`.
  `--no-leader` keeps each process owned by its daemon lifecycle.
- An `edits` ceiling is refused. Grok's Auto mode is not assumed equivalent to
  allowing edits alone. Explicit reasoning effort is also refused until its
  session-local mapping has been verified.

The first real input carries the frozen Project context: role and handbook,
shared memory, Board language, answer-Note guidance and chosen workflow.
Resume preserves the original snapshot. Private capability values are not
prompt content. Grok receives neither Gemini step-explanation preferences nor
Gemini turn-end instructions. The public transparency inventory has a separate
Grok entry.

Bare slash and shell controls without a verified route stay in the draft.
`/model <offered-id>` and Session model controls use native model selection;
file paths containing slashes or extensions remain ordinary input. This first
surface does not claim the complete native slash catalog, images, arbitrary
native interaction extensions, scheduling or a terminal fallback. Those need
separate capability verification, rather than assuming Gemini support applies.

## Completion, interruption and recovery

Only the native prompt result completes an ordinary turn. Text, usage messages,
Gemini markers, background notifications and quiet time cannot manufacture
success. Inactivity leaves an uncertain outcome. Native failures remain typed
and do not expose raw provider errors or credentials.

Grok cancellation includes `_meta.cancelTrigger: "ctrl_c"`. The daemon can
cancel and tear down its exact owned process. That alone does not establish
that a detached native job, child process or external side effect stopped;
real tool cancellation must be verified separately. An uncertain write is
never automatically repeated. After a daemon restart, the original transcript
can be resumed, but queued or interrupted Grok prompts are not automatically
resent. The person controls the next input.

History is stored in a Grok-specific state directory, and persisted records,
lookup and restore require the actual provider identity. Existing Gemini
records retain their previous format and are not migrated into Grok history.
The shared implementation's historical package names do not select a provider.

## Source investigation and validation

The comparison was pinned before implementation:

| Source | Inspected behavior |
|---|---|
| [T3 Code, `521ac7c`](https://github.com/pingdotgg/t3code/blob/521ac7c218b3faa74281fb72fc36ce74a0951fb3/packages/provider-grok/src/server/acpSupport.ts) | ACP arguments, separate permission semantics, cached login, cancellation metadata and native model offers |
| [Orca, `fcc072c`](https://github.com/stablyai/orca/blob/fcc072c37d70da1b1d6bf91b3ba3a2c2eff616c8/src/main/acp/acp-dialects/grok-dialect.ts) | Grok-specific completion, error and background notification dialects; old fixtures are not current local evidence |
| [Grok Build, `2bdd1d6`](https://github.com/xai-org/grok-build/blob/2bdd1d6a6369de0e8c68132ea4539e9abd9e14a8/crates/codegen/xai-grok-shell/src/agent/mvp_agent/acp_agent.rs) | Advertised auth methods, cached-token fallthrough, initialization and native session operations |

The account-free native initialize probe advertised protocol v1, embedded
context and load/resume/list/close support. It advertised only `grok.com`
before CLI authorization. No model prompt was sent by that probe.

Local protocol-peer tests cover provider isolation, missing/expired/unverified
auth, first-turn context, exact native address, offered models and permissions,
restart replay refusal, false-success prevention, repeated dispatch and
unsupported permission refusal. Failure-injection runs demonstrated the old
restart/marker/inactivity defects, incorrect provider address, accepting an
unsupported edits ceiling and unverified account mode before their corrections.
These are local tests, not real-provider end-to-end or deployed acceptance.
