# Message reactions

This construction unit adds source-owned reactions to actual main-conversation
human messages and final Bot messages. API 85 advertises the optional controls;
API 84 and earlier retain the Bots entry and existing chat operations. The
source/console Cloud decoder adds `project-assistants.react`; it does not repin
the private Cloud operation registry or change the opaque Bot network wire.

`POST /v1/project-assistants/{bot}/react` takes an immutable operation ID, message
scope, emoji, explicit `present` state and the authenticated reader's own
`expected_revision`. Reader identity is derived at the existing local/paired
authentication boundary. Neither a browser actor ID nor a model actor ID is
accepted. Each reader retains up to eight distinct emoji, oldest first. Emoji
validation follows pinned CodyNC 9f67eb09 `host/src/hub/reactions.rs`: trim,
nonempty, at most 32 UTF-8 bytes, no alphanumeric or whitespace characters.

Schema 11 adds `project_assistant_reactions` without decoding or rewriting the
historic aggregate, event or receipt bytes. Empty actor rows retain revisions.
The source locks against aggregate changes, validates the current Project and
conversation, then commits reaction metadata, its event and its durable command
receipt in one SQLite transaction. Replaying the exact operation returns that
receipt; changed intent conflicts. CAS belongs to each actor's metadata, so
another actor's concurrent addition survives. No message content, order, unread
cursor, last-message sequence or Bot execution version changes.

Selected bounded snapshot/history pages project metadata onto message copies;
events carry `reactions_changed`. Canonical archives use an optional additive
`Reactions` relation and retain original message facts. Old producer archives
remain hash-compatible. Restores validate association and actor metadata before
the same transaction inserts anything. Bot deletion removes its reaction rows.

Production Chat renders persisted chips, an accessible six-emoji palette and
own-reaction removal. Pending writes do not invent success. The exact request is
saved before transport; an uncertain result survives navigation/reload and can
be explicitly checked with the same operation. Source/grant changes discard late
component results. Read-only and unsupported sources display persisted facts
but do not offer writable controls. The English and Taiwan Traditional Chinese
catalogs are updated together.

The optional owned MCP `react` tool derives its actor and target from the
running turn. It accepts an emoji, not an actor, account, target message, URL or
execution generation. The source checks the captured lifetime, source policy,
current binding, actual running turn, configuration and target at effect time.
It journals the native invocation before the write. One Bot's next reaction
replaces its previous emoji on that target. Retained successful reaction evidence
allows a reaction-only result without fabricated assistant text or an empty-reply
notice. Peer/network work, room turns and other unsupported origins are refused.

## Human reactions in group rooms

Group history includes reactions on published room lines and identifies the
current reader's own reactions. API 91 accepts a group reaction by room
conversation ID and line sequence. The write checks the group version and the
reader's own reaction revision, and commits the reaction with the existing
group command receipt. Its replay still checks the current authority and room
identity. Taking back an emoji retains that reader's revision. Reactions leave
the room sequence, member turn, quote and original line intact. The table is
additive on installed databases. Group history and the opaque Cloud transport
keep their existing byte limits. Older daemons and read-only grants still open
group history and expose the actions they support.

The room reuses the main-chat message menu, palette and same-key recovery.
Desktop displays the action button beside the bubble on hover or keyboard
focus, and the context menu opens the same actions. Touch opens that menu
after a stationary hold. A scrolling gesture cancels the hold. Reply
continues to quote the original line from the room store. Group Bot tool
reactions, including target selection from shown messages and spacing,
remain outside the human controls.

The main-chat coverage includes source, Project, turn and generation fences,
Stop, independent authenticated HTTP readers, late revoked replies and narrow
touch actions.

The owned task artifacts contain rerunnable commands, real HTTP/SQLite tests,
production bilingual browser evidence, an owned synthetic ACP/MCP test and the
decoder set comparison. No installed provider, model, account or paid API is
used by those fixtures. Root owns integration, final review, deployment and the
parent Board checkbox.
