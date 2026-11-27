# Bot settings and source capabilities

A Bot has one draft editor shared by Details and Bot settings. Details contains
standing instructions, a primary Agent/model summary, the selected login profile
and an ordered fallback summary. Model choices and login management open on
request. Bot settings contains collapsed name/appearance, permission/notification,
application-extension, network, saved-rule and management controls. Its link
returns to the same conversation setup draft.

Save explicitly applies the whole draft with the version captured when the
editor opened. Cancel discards it. Switching information tabs or closing the
information panel keeps the draft. A stale version keeps the draft for review;
an unknown save outcome locks further saves and never generates an automatic
replay. Source capability checks and existing native-configuration validation
still apply. Login/profile management and network/rule operations keep their
own explicit commands; they are not staged by the Bot settings Save button.

Saved instructions and supported runtime settings apply to the next idle turn.
They do not rewrite the running turn's immutable settings snapshot. Project
binding remains fixed. Changing the primary Agent clears incompatible runtime,
selected-login, connector and fallback choices before saving.

## Conversation compaction is a separate capability

The settings layout was compared with Codync 2.47.0 at source revision
`9f67eb09084db61b3cca9c84a11bc03df9de29b7`. This comparison does not qualify an
installed Clawdline adapter to use Codync's compaction controls.

The supplied Codex ACP 2.6.0 implementation emits `usage_update` with the model's
reported context-window size and advertises `/compact`. Its command handler
calls the native `thread/compact/start` operation. A previously authorized
isolated native turn also recorded a nonzero model context window. That proves
native window reporting for that turn, not a universal window size or a
successful compaction operation.

Clawdline now decodes those events: the ACP client keeps `available_commands_update`
as `CommandsKnown`/`CompactAdvertised` and `usage_update` as `UsedTokens`/`WindowTokens`
(`client_context.go`). Compaction is not a Bot settings control. A person asks for it
through **Compact now** on the context meter, which admits one journaled, idle-only
turn whose only prompt is the agent's own `/compact` command, limited to Claude Code
and Codex, with typed refusals while busy, without a native session or for other
input (`compact_now.go`; described in `docs/bots.md`). The lower-level native RPC is
`Client.BeginCompaction`. Clawdline has no persisted Bot compaction policy. The native
Agent's own automatic compaction policy remains inherited; an absent Clawdline
control does not mean native compaction is off. Bot memory organization is a
different operation and does not satisfy conversation-compaction acceptance.

## Verification boundary

The owned browser fixture uses production Chat, SourceClient and settings
components against authenticated HTTP, the real Host and SQLite. Its native peer
is a local scripted ACP subprocess. It checks one instruction editor, draft
retention and cancellation, Save/reopen, stale and unknown outcomes, collapsed
secondary controls, desktop/mobile containment and settings in the next scripted
turn. It makes no external model request. This fixture does not establish
production Cloud delivery, physical mobile keyboard behavior or native-provider
compaction. Those destinations require their own acceptance evidence.
