# Portable Bot settings templates

The chat header's **More actions → Create template** reads the currently authorized
Bot, displays a selectable JSON preview, and offers **Copy template**. The existing
**Export** action remains a complete journal export with its existing format.
Template copying creates no Bot, Session, import, download or model request.

## Format and coverage

The renderer owns the reference format `clawdline.bot-template.v1`. Its root contains
`format` and `settings`; settings contains these explicit fields:

| Field | Coverage |
| --- | --- |
| `name` | Configured Bot name |
| `appearance` | Only `shape` and `color` |
| `agent` | Primary Agent |
| `instructions` | Configured instruction text, without revision or role reference |
| `permission_policy` | Saved approval policy |
| `mode` | Saved mode when present, including an empty inherited value |
| `notify` | Saved notification selection when present, including `off` |
| `runtime_settings` | Model and the portable options listed below, when present |

The runtime option whitelist is `reasoning_effort`, `reasoning-effort`, `effort`,
`thought_level`, `fast`, and `fast-mode`. Values remain strings as in the current
runtime settings contract: `"false"` is retained, as are empty model/option values.
Unrecognized native option IDs are omitted, even if offered under a known category.
They need deliberate setup on the receiving source. Null and undefined optional
fields are omitted. Object keys are sorted recursively using ordinal comparison;
JSON uses two spaces, no trailing newline, and ordinary JSON escaping. Instructions
are data and are rendered only as text in a read-only textarea. They may contain
private prose supplied by the person, so both languages ask them to review before
sharing. No automatic content redaction changes those configured instructions.

The whitelist excludes Bot/conversation/binding/turn/reader/request identities,
machine/account/Project identity and paths, history/journal/memory, receipts,
events/traces/work links, login/profile/key/secret handoff data, fallback identities,
team-management and network grants, and extension selections, fingerprints and
materialized paths. Source-specific configuration is never traversed or serialized.

This flow follows the pinned CodyNC create/preview/copy behavior reviewed at
`9f67eb09084db61b3cca9c84a11bc03df9de29b7`, using an explicit Clawdline whitelist
instead of copying an entire runtime object. It does **not** claim CodyNC wire
interoperability or support importing/creating a Bot from the copied JSON.

## Authority and interaction

Opening the preview reads the existing SourceClient `get` operation. A list summary
alone cannot produce a template. Contract, source, Bot, Project and conversation
identity are validated, including current readable, online and fresh status.
Read-only readers with current content access can preview and copy.

Copying performs another authorized read and requires its encoded settings to match
the exact displayed preview. A changed grant, unavailable source, different identity
or changed settings stops copying, clears the preview and asks for a fresh open.
Unmounting aborts outstanding reads. The clipboard is written only after these checks;
its success state follows the real write result. A clipboard refusal or missing API
shows a manual-copy explanation and retains the selectable preview. The product never
reads the clipboard, evaluates content, fetches referenced URLs, opens a frame or
stores the template automatically.

The native modal keeps keyboard focus inside the preview. Escape, the close button,
and browser Back return to the chat and its menu focus. The chat and composer stay
mounted, preserving the conversation and draft. Enter in the preview has no copy or
send shortcut, and composing Enter cannot activate a button. There are no animations,
including with reduced motion enabled.

## Verification

`bot-template.test.ts` exercises exact output, nested key ordering, false/empty/null
handling, explicit inclusion/exclusion, future extra fields and invalid read scopes.

The opt-in `TestProjectAssistantTemplateBrowser` fixture uses a task-owned real
HTTP server, Host and SQLite with synthetic data and a local configuration-only
provider. Run it with `CLAWDLINE_TEMPLATE_BROWSER_DIR` and `CHROME` pointing to an
existing isolated test browser. It installs no browser or dependency and never uses
a person's profile. Its harness renders the production Chat and SourceClient,
compares native browser clipboard content with exact expected JSON, verifies both
languages, clipboard refusal, source/conversation fencing, focus and draft behavior,
and checks that requests are read-only and durable state stays identical. This is
construction evidence, not production Cloud, device or public Release acceptance.
