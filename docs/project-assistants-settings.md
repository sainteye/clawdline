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

Clawdline currently has neither a source-qualified compaction capability nor a
persisted compaction policy or fenced compaction command. Its ACP notification
decoder does not retain usage sizes or available commands. Ordinary Bot input
may contain a source-owned handoff, quoted message or image before the current
text; sending the string `/compact` through normal chat therefore is not a
qualified substitute for a native command. No new compaction command was sent
for this settings increment. Claude command/window behavior has not been
qualified here. The native Agent's own automatic compaction policy remains
inherited; an absent Clawdline control does not mean native compaction is off.

The smallest useful follow-up needs a source capability reporting a known
window and supported trigger, typed decoding of native usage/command events,
and an explicit idle-only command bound to the Bot, conversation and execution
generation. Acceptance must observe actual native start/completion, retained
conversation continuity and refusal while busy or stale. Unknown outcomes must
not replay compaction. Only after those boundaries work should a policy control
be shown. Bot memory organization is a different operation and does not satisfy
conversation-compaction acceptance.

## Verification boundary

The owned browser fixture uses production Chat, SourceClient and settings
components against authenticated HTTP, the real Host and SQLite. Its native peer
is a local scripted ACP subprocess. It checks one instruction editor, draft
retention and cancellation, Save/reopen, stale and unknown outcomes, collapsed
secondary controls, desktop/mobile containment and settings in the next scripted
turn. It makes no external model request. This fixture does not establish
production Cloud delivery, physical mobile keyboard behavior or native-provider
compaction. Those destinations require their own acceptance evidence.
