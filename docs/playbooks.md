<!-- clawdline-doc: kind=guide audience=both -->

# Project Playbooks

A Playbook is a Project-scoped name and complete launch message. It may start
with a Skill invocation, but its launch message remains editable and a Playbook
made from an empty draft is equally valid. Imports also save a source reference
for attribution; that reference never replaces or rewrites the launch message.

The Playbook page in the sidebar lists one Project's saved messages. It reads
registered places, Skills, and Claude command files. The import list shows the
name and frontmatter description, then lets the person preview the full source
before creating an editable draft. A command import tells the assistant to read
the Project's named Claude command; the file remains required at run time. The
editor saves `title`, `body`, and an optional structured source reference. The
reference stores the source kind, file ID, original Project place ID, display
name, and version at import time. It lets the editor show the current source
through the existing authorized Project file read route. A moved or deleted
source retains its saved name and shows an unavailable state; it is never
reconnected by a matching name. Source changes are indicated by version, and
the saved launch message is not overwritten. Earlier Playbooks without a link
can be linked explicitly in the editor. The Project path
is assigned from the selected place, not typed into the form. There is no
description, scheduling rule, or implicit trigger.

The Session mark opens one sheet with Snippet and Playbook tabs. A Snippet
continues to insert text without sending. Selecting a Playbook shows its
entire saved message and Start sends it to this Session. A Playbook belongs to
the same Project key as the Session. The list is machine-specific, including
when the Console is reached through Cloud.
Cloud asks the selected machine to resolve a Session's Project, so a Session
in a subdirectory or worktree can see its canonical Project Playbook. If that
read is unavailable on an older machine, the Snippet tab still works and the
Playbook tab names the update requirement.

## Storage and execution

Playbooks use the reusable-text store's `playbook` scope, with the same title,
body, Project key, ordering, write receipts and capacity limits as snippets.
The Snippet tab filters that scope out. This shares the existing encrypted
Cloud read and command transport without giving Cloud the text in cleartext.

Starting creates a direct Session to-do containing the saved launch message
and a trailing Playbook identity marker, then sends that to-do. The to-do
route's own receipt prevents a repeated send for a single row. The marker
preserves the Playbook name at launch time even when its definition is later
edited or deleted. The Session to-do fold groups these rows as Playbook
progress: unsent, sent, read/in progress, and complete. The assistant's
`clawdline todo done` receipt or the person's checkbox completes the run.
Delivery and reading are not reported as completion. If creation succeeds but
send fails, the unsent row stays in the fold with a Send action; an uncertain
result directs the person to that fold before retrying.

The first version does not schedule Playbooks, chain Sessions, run commands
directly, or define a second step language. The launch message may tell the
assistant to use a Skill, but Clawdline does not interpret that instruction.
Global Playbook availability does not make a Project-specific source available
in another Project; the source reference is historical attribution only.

## Agent API

Agents can create, edit, delete and reorder Playbook definitions through the
machine's existing `/v1/snippets` routes or `clawdline playbook` commands. A Playbook
is a record with `scope: "playbook"`.
The CLI supports list/create/edit/delete; reorder uses the API. Saving a definition
does not start a Session or send its message. See [the agent connection guide](../skills/clawdline/guide.md#2-reaching-the-daemon)
for credential handling and `clawdline guide routes` for the installed build's
route inventory.

Use `clawdline guide project` for the installed build's agent instructions. The CLI
reads credentials inside its own process and prints JSON. Flags go before the id:

```sh
clawdline playbook list --session <terminal-id>
clawdline playbook create --project <returned-project-key> --body-file create.json
clawdline playbook create --global --body-file create.json
clawdline playbook edit --body-file edit.json <returned-id>
clawdline playbook delete <returned-id>
```

The create file contains `title`, `body`, and optional `source`; the edit file contains
only the fields to change. A body-only edit preserves title, Project and source.
Create requires exactly one of `--project` or `--global`; use the key returned by
`list --session`, including when your checkout is a worktree. List without a Session
returns all machine Playbooks. Edit/delete refuse an ID that is not a Playbook.
All actions accept `--port`; writes accept `--key`, and `--body-file -` reads stdin.
The CLI prints the write key before sending; preserve it with the same operation,
id and JSON content after an uncertain result. Saving never starts a Playbook.
Older installed CLIs can use the existing API below with their version's guide.

| Method and path | Request / result |
|---|---|
| `GET /v1/snippets` | Returns `{ "snippets": [...] }`, including other reusable-text scopes. Filter records by `scope == "playbook"`. |
| `GET /v1/snippets?session=<URL-encoded terminal ID>` | Returns the resolved `project: {key, label}` and visible `snippets`, including global Playbooks. Use the returned Project key rather than a worktree path. |
| `POST /v1/snippets` | Create with `title`, `body`, `scope: "playbook"`, optional `project` and `source`. Returns the saved record. |
| `PATCH /v1/snippets/<id>` | Change only the supplied writable fields; omitted fields retain their values. Returns the saved record. |
| `DELETE /v1/snippets/<id>` | Returns `{ "ok": true, "deleted": "<id>" }`. |
| `POST /v1/snippets/order` | Send `{ "scope": "playbook", "project": "<key>", "order": ["<id>", ...] }` with every member of that group exactly once. Omit `project` for the global group. Returns `ok`, `scope`, optional `project`, and the ordered `snippets`. |

Reads require a valid machine or paired-device credential. Writes require the
machine's `X-Clawdline-Orchestrator` token or a paired device allowed to send.
Do not send the orchestrator token as `Authorization: Bearer`. Every write,
including DELETE and reorder, requires an `Idempotency-Key` of at most 200
characters. Send JSON with `Content-Type: application/json`.

For example, in Bash or Zsh on the target machine, read the credential inside
the shell and pass it through a header file descriptor so it does not appear
in process arguments. Do not enable shell tracing or print the token:

```sh
PLAYBOOK_STATE="${CLAWDLINE_NEXT_DIR:-${XDG_CONFIG_HOME:-$HOME/.config}/clawdline-next}"
PLAYBOOK_URL="http://127.0.0.1:${CLAWDLINE_NEXT_PORT:-7727}"
playbook_auth() { printf 'X-Clawdline-Orchestrator: %s\n' "$(cat "$PLAYBOOK_STATE/orchestrator-token")"; }

# Discover this Session's canonical Project key; encode terminal IDs such as %47.
curl --fail-with-body -sS -H @<(playbook_auth) --get \
  --data-urlencode 'session=<terminal-id>' "$PLAYBOOK_URL/v1/snippets"

# Save create.json using the Project key returned above.
cat > create.json <<'JSON'
{"title":"Review changes","body":"Review the current changes and report findings.","scope":"playbook","project":"<resolved-project-key>"}
JSON
PLAYBOOK_CREATE_KEY="$(uuidgen)"
curl --fail-with-body -sS -H @<(playbook_auth) \
  -H 'Content-Type: application/json' -H "Idempotency-Key: $PLAYBOOK_CREATE_KEY" \
  --data-binary @create.json "$PLAYBOOK_URL/v1/snippets"

# Use the returned id. Each new logical operation gets its own key.
PLAYBOOK_EDIT_KEY="$(uuidgen)"
curl --fail-with-body -sS -X PATCH -H @<(playbook_auth) \
  -H 'Content-Type: application/json' -H "Idempotency-Key: $PLAYBOOK_EDIT_KEY" \
  --data-binary '{"body":"Review the changes, prioritizing correctness and regressions."}' \
  "$PLAYBOOK_URL/v1/snippets/<returned-id>"

PLAYBOOK_DELETE_KEY="$(uuidgen)"
curl --fail-with-body -sS -X DELETE -H @<(playbook_auth) \
  -H "Idempotency-Key: $PLAYBOOK_DELETE_KEY" \
  "$PLAYBOOK_URL/v1/snippets/<returned-id>"
```

Replace placeholders before running. To create a global Playbook, omit the
`project` key entirely; an empty string or null is invalid. A title-only or
body-only PATCH preserves the existing Project and source link. Writable keys
are exactly `title`, `body`, `scope`, `project`, and `source`; do not send an
entire returned record back (it includes read-only fields such as `id`,
`position`, `created_at`, `updated_at`, and sometimes `builtin`). Title and body
must be nonblank. Limits are UTF-8 bytes: title 200, body 4,000, Project path
1,024; the HTTP write body is at most 64 KiB.

`source` is optional attribution, not executable content. If supplied, it has
exactly six string fields: `kind` (`skill` or `command`), `file_id` (32 lowercase
hex characters), `place_id` (1–200 bytes), `scope` (`project` or `global`),
`name` (1–200 bytes), and `version` (1–200 bytes). Use values from the authorized
source listing rather than inventing IDs. `source: null` clears the link;
omitting it on PATCH keeps the link. Only Playbook records accept a source.

After an uncertain write, reuse the same key, method, path and exact body bytes.
A stored response is replayed with `Idempotent-Replayed: true`, including across
daemon restarts. Reusing a key for a changed request returns 409
`idempotency_key_reused`. Do not generate a new create key merely because the
connection dropped. Read the saved list to verify the resulting definition.

Validation refusals use `{ "error": "<code>", "detail": "...", "counts": {...} }`
(`counts` is optional); authentication and receipt refusals may use the nested
`error.code` envelope described in the connection guide. Branch on codes:

- 400: `idempotency_key_required`, `malformed_snippet`, `snippet_too_long`,
  `snippet_scope_mismatch`.
- 401/403: missing or invalid credentials / insufficient send permission.
- 404: `snippet_not_found` (or `not_found` for an unresolved Session).
- 409: `snippet_limit_reached`, `idempotency_key_reused`, `receipt_expired`.
- 413: `body_too_large`.
- 429: `rate_limited`, `busy`, `receipts_full`; do not retry in a tight loop.
- 5xx: store or write failure; reconcile the saved state and receipt before
  deciding whether to retry. Route-level 429 and 5xx answers are not retained
  as completed snippet-write receipts.

Implementation references: `internal/transport/http/snippets.go`,
`internal/domain/snippet/snippet.go`, and `internal/transport/http/receipts.go`.

## Upgrading saved definitions

The store upgrades existing definitions and source attribution to the `playbook` scope
and `playbook_sources` table in one transaction. It preserves record IDs, Project keys,
ordering, authored messages and timestamps. Existing Session to-do metadata is converted
to `[Clawdline-Playbook …]` only when it is a valid trailing run marker; ordinary message
content is left intact. Send, read, completion and version receipts are unchanged. A second
open is a no-op. Current writes accept only `playbook`; the retired scope is not an alias.
Legacy definition-write receipts become expired tombstones: retry keys and digests
remain, so an old response cannot revive the retired scope or create a duplicate.
The console page is `#page=playbook`, and the running refusal is `playbook_running`.

## Acceptance

- A Project Playbook can be created blank or from a Skill or Claude command,
  edited after import, read after a reload, and deleted without changing the source file.
- A Session in another Project sees no Playbook from this Project.
- Snippets still insert text and never send; a Playbook start leaves a durable
  to-do and one send receipt.
- The fold distinguishes sent/read from complete, and an unsent run remains
  recoverable after a failed send.
- The phone E2E path imports a Skill, saves the Playbook, starts it from the
  Session mark, sees its progress in the fold, and marks it complete.
