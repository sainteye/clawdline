# Persistent Bots: getting started

A Bot is a long-lived assistant bound to one Project. You chat with
it in the console; it remembers what you tell it, works in the Project with
its agent’s own tools, and can hand larger changes to an isolated worker
through Clawdline's Broker. The worker's result comes back to the same chat.

Bots are off by default.

## What you need

- macOS.
- For a Claude Code Bot: Claude Code 2.1.323 or newer, signed in as you
  normally use it. For a Codex Bot: Codex 0.166.0 or newer, signed in, with
  its memories feature off (the default; `codex features list`).
- For a Gemini Bot: Antigravity CLI (`agy`) on macOS or Linux, signed in to
  a personal Google account. The Bot uses an isolated native profile with
  AI Credits off and retains that profile for the Bot's current conversation
  and memory epoch. Choose **Gemini** in the Bot's agent picker.
- Node 22 or newer with npm for Claude Code and Codex.

Clawdline finds both programs itself, including installs from nvm, mise,
asdf, fnm, Volta or Homebrew. You do not place any files. On the first
message, Clawdline installs the ACP adapter it supplies
(`@agentclientprotocol/claude-agent-acp` or `@agentclientprotocol/codex-acp`) with your npm and checks it
against a fixed digest. This takes a few seconds, and later messages reuse
it.

## Turn Bots on

In the Clawdline app on this machine, open **Settings** and turn on
**Persistent Bots** (on the tab with the dispatch settings). The Bots
shortcut in the bottom status bar becomes available. A paired browser or
phone can read the setting but cannot change it, like this machine's other
settings.

Bots is not in public releases yet: the switch is shown only on a dev build
(`v<x.y.z>-dev.<commit>`), or where Bots is already on so it can be turned
off. The terminal command below still works on any build.

From a terminal, the same switch is:

```sh
clawdline project-assistants enable
```

`clawdline project-assistants status` shows the current state.
`clawdline project-assistants disable` turns Bots off and keeps saved Bots
and conversations. `clawdline project-assistants setup-claude` runs the
first-use setup ahead of time and prints which Claude Code version it found;
`setup-codex` does the same for Codex.

## Use a Bot

1. Click Bots in the bottom status bar, then **+**. The shortcut rings
   and counts new replies.
2. Give it a name, choose the Project and write its standing instructions.
   Creating a Bot does not call a model.
3. Send a message. Up to four pictures can go with it: press **+**, paste
   them or drop them on the message box. The Bot sees them as images. Pictures
   also work from a paired Clawdline Cloud browser when its source runs API 68
   or newer. A picture-only message is allowed. Cloud keeps the existing
   transport limits: a send that cannot fit is refused before sending, and a
   picture too large for one encrypted read shows an explicit error. Loading
   and failed reads remain visible; retrying a picture read does not resend
   the message.
   The Bot can send several separate messages during one turn, each delivered
   immediately with `send_message`, as in Codync. Each message carries one useful
   finding, result or question. Once it has sent a message, later final text stays
   in the process trace; it must send any further result explicitly too. If it sends
   no message, its last text segment becomes the fallback reply. Earlier narration
   stays in the trace. Longer reports can be published as an HTML artifact.
   **Show what it did** keeps separate native narration, thoughts when the agent
   supplies them, and individual tool calls with their status, text output and
   diffs. The supplied Codex adapter's terminal-output metadata is joined in
   order within its own tool, alongside ordinary ACP text output. Text chunks join within their own step; later steps do not overwrite
   earlier ones. An expanded step refreshes when new content arrives. Older
   category-only records retain their original representation; omitted thoughts
   and outputs from earlier turns cannot be recovered by this change.
   While it works, the chat shows what it is
   doing (the tool it is using, or that it is writing) and how long the turn
   has run. A message sent while it is still working waits in line and is
   shown as pending; **×** on a pending message takes it back before it is
   sent, leaving the current reply and the other pending messages alone, and
   **↳** sends it now, into the reply in progress, as in Codync. If that reply
   had just ended, the message keeps its place and goes next. Send now also
   works from a paired Clawdline Cloud browser on a source that supports it.
4. Ask it to dispatch work, for example "add a file and commit it". It
   starts an isolated worker in that Project's worktree, and a work card
   follows the work from accepted to running to done. The worker's summary
   arrives in the chat.
5. **Stop** ends the current reply and any pending ones. Your ordinary
   Sessions are not touched.

Bot avatars keep their outline shapes. Each saved color has a matching
three-color gradient and a soft neon halo, including while idle and in the
shape and color picker. Color choices preview the selected shape. Working
Bots glance, blink and slowly turn the gradient light; Bots waiting for an
answer keep their gaze centered and send a faint contour outward. These
motions follow Codync's activity cadence, adapted to line art. Reduced
motion keeps the gradient and halo without movement. Existing saved shapes
and colors keep the same storage and transport format.

The chat header follows the current turn too. The status-bar shortcut
summarizes visible, authorized Bots: a Bot needing your answer takes
precedence over working Bots, then the entry rests when all are idle.
Unread counts alone do not start a working animation. Stale or disconnected
activity stops moving while unread counts remain available. Local sources
that support the roster stream update the closed entry from that stream;
Cloud and older sources keep the existing snapshot refresh interval.

The chat's working indicator uses the same thinking orb as the roster. It
expands and fades in for new work, then collapses and fades out when work
ends. A quick restart reverses the exit without removing the new activity.
An already-running turn in the initial snapshot appears immediately, and
reduced motion skips these transitions.

A Bot keeps its own memory, as in Codync: when you tell it something lasting
or ask it to remember, change or forget something, it does so with its
memory tools, one short fact per entry. **Memory** in the chat lists every
entry, yours and the Bot's, and forgets the ones you choose. Adding or
changing an entry keeps the Bot's conversation going, and its next reply
starts from the whole memory as it is now; forgetting starts a fresh
conversation that carries only what is still remembered. A Bot keeps at most
256 entries.

**Permissions**, in the Bot's settings and when you create it, decides how
much the agent does on its own before it asks, as in Codync. It lists the
agent's modes, its Auto mode first, then **Approve automatically**:

| Setting | What happens |
| --- | --- |
| **Auto** (Claude Code), **Auto review** (Codex) | The agent decides what's safe and does it, and asks you about the rest. New Bots start here. |
| The agent's other modes: **Manual**, **Accept edits**, **Plan** (Claude Code); **Read-only**, **Workspace access**, **Full access** (Codex) | The agent works in that mode; anything it asks about appears as a permission card. |
| **Approve automatically** | Each request is allowed for you with the agent's allow-once option. |

With any setting your agent's own settings still apply. On a model without
Claude Code's Auto mode, Claude Code works in Accept edits instead. A mode
the agent does not offer stops the turn with a reason rather than run it in
another. When the agent asks, a permission card appears in the chat: **Allow
once**, **Always allow**, **Reject** or **Never**, as in Codync. "Always" is
as the agent defines it. Claude Code keeps the rule in the Project's
`.claude/settings.local.json`; **Standing rules** in the Bot's settings lists
those rules and revokes one, after which Claude Code asks again. (Standing
rules are shown in this machine's own console; Codex keeps its own and
Clawdline does not list them yet.) If a worker's terminal shows a question, such as whether to trust a
folder, the work card tells you to answer it in that Session.

### Edit a queued message

Open **Edit queued message** on a message that has not started. The source
pauses it at its queue position. Save replaces the text and resumes the queue;
**Keep original and resume** releases the hold with the previous text. Files,
pictures and the quoted message stay attached. A paused message remains
visible to paired devices and survives a daemon restart. A paired device can
reopen the editor or withdraw the message. Once the Bot has reserved the
message for execution, the source refuses an edit.

### See the model, context usage and group work

Above the message box, a running Bot shows the model selection reported by its
native session. A Bot with no active turn shows its saved setting or the Agent
default. Choose the model label to open the same model list as Bot settings.
Saving applies the selection when the next turn starts, including when a reply
is already in progress. Model discovery sends no prompt.

A Bot chat shows context tokens when its provider reports a valid reading.
After a successful turn, the last reported reading stays visible for its
current binding and memory revision until another turn starts. In group
chats, the member's name shows who is queued, working or waiting for
permission. Existing Broker tasks from that room turn show their current
states under the same member.

## Files and interactive documents from a Bot

A Bot explicitly shares an output with `send_file` and publishes self-contained
HTML with `publish_artifact`. Both tools require an `idempotency_key`: reuse the
same key and arguments after an uncertain reply. A success means the snapshot
and its chat card are already saved. Project files are never published by scanning
the directory. Paths must name regular files inside the Bot's current Project;
absolute paths inside that directory also work. Symlinks, directory traversal,
directories and special files are refused.

`send_file {idempotency_key, path, name?}` accepts binary, extensionless and empty
files up to 100 MiB. `publish_artifact {idempotency_key, path, title?, artifact?}`
accepts UTF-8 HTML up to 5 MiB. A new artifact needs a title; titles are trimmed,
up to 200 UTF-8 bytes, with no control characters. For an update, pass the previous
`artifact_id` as `artifact` and publish the complete replacement HTML. Omitting
the title on an update keeps its current title. `list_artifacts {}` returns the
Bot's latest artifact descriptors, newest first, including their stable
`artifact_id`, display `name`, byte count, SHA-256 and the current HTML snapshot's
`path`. That path is for reading; edit a copy inside the Project and publish the
copy, rather than modifying stored snapshots.

Click a file card to download its verified snapshot. An artifact card opens the
newest published HTML, including when opened from an earlier card. When a Bot
follows an artifact with text in the same turn, its last reply includes a button
to open that artifact. **Library** in the conversation menu lists Bot outputs,
files sent with messages and picture
attachments, grouped by the viewer's local calendar date, newest first. Multiple
revisions of an artifact occupy one library row. Shared-content changes in the
chat refresh the snapshot; already loaded earlier rows remain available within
the same source epoch. Earlier messages can be loaded there on demand without
automatically draining the entire history. Input filenames are preserved; picture
metadata has no original filename, so the library displays a picture label and a
short content ID, and downloads use the complete ID plus the media extension.
The original source may be
edited or deleted, and a daemon restart does not change the saved bytes.

Every authenticated output read names the Bot and conversation. A file also
names its exact message and output ID; an artifact names its stable artifact ID.
The source checks snapshot size and SHA-256 on each read and returns at most
384 KiB. The client verifies the complete size and SHA-256 before displaying HTML
or offering a download. Closing the viewer cancels subsequent reads and discards
late responses; changed authorization invalidates the transfer.

HTML runs in two sandboxed frames with opaque origins. It has no app credentials,
parent-document access, app storage, network access, popup permission or top-level
navigation permission. The trusted outer frame's policy also blocks navigation
of the inner frame. Inline CSS and JavaScript can provide local interaction.
Links inside the document do not navigate the frame; **Open a link** offers its
HTTP, HTTPS and mail links as explicit app-owned clicks. **Save as HTML** exports
the original verified bytes. **Open in browser** opens a trusted local Blob
document containing the same two sandboxed opaque-origin frames, with no opener;
it does not open the raw HTML with application-origin privileges. The new tab is
an offline copy of bytes already read, not a live authenticated view. This adds
no public links or automatic publishing. All library transfers expose progress,
cancel and a failure code with explicit retry. Picture reads use their existing
single-response protocol and report completion after verification; they do not
claim byte-by-byte streaming progress. Read-only devices can read and download.
On API 78 or newer, **Rename artifact** in the viewer edits the display title.
The human command uses the captured source, full conversation fence, expected
publication ID and title revision. Names are trimmed and use the existing
200-byte artifact-title rule. Read-only devices have no rename control.
A separate persisted overlay updates library rows, old chat cards, output reads
and the native `list_artifacts` result without changing immutable messages,
HTML snapshots, digests or snapshot IDs. Publishing a new revision clears the
overlay; omitting its title retains the effective current title. A replay with
the same key and canonical payload returns the original receipt; a changed
payload conflicts. An uncertain command remains stored under its account,
source, conversation and artifact identity. Only an explicit retry sends that
same command; it never generates a fresh key while unresolved. The feature
schema is 5 so older stores refuse to rewrite title metadata they cannot retain.
Archives preserve original publications and the display overlay separately.

Copying runs outside the Bot actor and SQL transactions. Current execution
authority is checked during copying and again at publication; Stop, replaced
turns and reconfiguration cannot publish a late card. Each copy has its own
snapshot, so refusing one cannot remove bytes used by another card. An uncertain
SQL acknowledgement preserves a snapshot when its ownership cannot safely be
disproved. Bot deletion removes the stored output directory with the Bot's other
attachments.

Input pictures retain their separate attachment protocol. General input files use
the separate upload protocol described below. The local regression fixture uses an owned
ACP subprocess and real MCP, SQLite and HTTP, with zero provider/model calls.
For its optional real-browser run, supply `PLAYWRIGHT_MODULE`,
`PLAYWRIGHT_BROWSERS_PATH` and a task-owned `CLAWDLINE_OUTPUT_E2E_DIR`, then run
`go test ./internal/transport/http -run '^TestProjectAssistantOutputOwnedMCPHTTP$' -count=1`.

Library coverage has a separate stored-snapshot fixture, without an ACP process:
`go test ./internal/transport/http -run '^TestProjectAssistantLibraryReadOnlySnapshots$' -count=1`
checks snapshot/history paging and reads of outputs, input files and pictures
with a read-only credential. To exercise the real library and captured local/
Cloud source clients in headless Chromium, set task-owned
`CLAWDLINE_LIBRARY_E2E_DIR` and the existing `PLAYWRIGHT_BROWSERS_PATH` (or `CHROME`),
then run `go test ./internal/transport/http -run '^TestProjectAssistantLibraryBrowser$' -count=1`.
The browser fixture installs nothing and closes its own tabs/process/profile.
It is synthetic transport acceptance, not production Cloud pairing or a native
Agent publication receipt.

## Roster activity

The roster follows Codync's four presentation states: `idle`, `working`,
`needsInput` and `error`. Working rows show the current tool title, writing or
thinking activity. A permission request uses its actual action title. Idle rows
return to the latest message; errors have a separate red caption. Unread counts
and the machine connection remain independent of these states. A needs-input
badge takes precedence over the avatar's unread dot without hiding the count.

The avatar retains the approved BR-06 SVG outline, original eye strokes and
identity palette. Motion timing follows Codync commit
`9f67eb09084db61b3cca9c84a11bc03df9de29b7`: working eyes glance and blink,
and a waiting contour pulses at its ripple cadence. Idle and reduced-motion
avatars use the original static lines. Animation pauses off screen and while
the page is hidden. The small thinking orb retains its upstream Canvas geometry.
License notices ship under `public/licenses/` in the console.

This first projection is exercised through the real local Store and HTTP
boundary with a synthetic event producer, without a provider/model call.
Set task-owned `CLAWDLINE_ROSTER_E2E_DIR`, `PLAYWRIGHT_MODULE` and
`PLAYWRIGHT_BROWSERS_PATH`, then run
`go test ./internal/transport/http -run '^TestProjectAssistantRosterStatusHTTP$' -count=1`.
Its current browser evidence explicitly records `eventUpdates: false`:
the console still consumes snapshots. Automatic console updates and encrypted
Cloud subscriptions remain pending; the local feed below does not establish
either destination's acceptance.

### Local roster event feed

API 71 adds local `GET /v1/project-assistants/roster-events?machine_id=…`.
Optional `project_id` narrows the Project; `cursor` or `Last-Event-ID` resumes
the last fully applied frame. The normal human read gate, current source,
reader, grant and registered Project are required. Read-only viewers can
subscribe. Ordinary Cloud request recorders explicitly refuse this route.

The application subscribes to the production writer Store's content-free
post-commit wake-ups before reading SQLite. Its ViewStore borrows the existing
query-only pool. No polling, provider startup, conversation Export or second
runtime authority is involved. Query-local SQLite JSONB parses a large stored
aggregate once; JSON projections return only current summary facts and reuse
the roster's existing status logic. A files-only message retains the existing
filename preview; text still takes precedence when both are present. Read transactions end before network IO.

Frames are `hello`, `snapshot`, `snapshot-complete`, `changes`, `resync` and
`closure`. Apply an entire data frame before saving its signed cursor. Initial
pages retain their first machine journal frontier while enumerating by Bot key;
changes during enumeration replay afterward, even when the changed key sorts
before the next page. Only `snapshot-complete` establishes a complete roster.
Upserts and hide/delete removals carry a durable roster revision. Unread is an
absolute reader-specific count, so duplicate replay cannot add unread messages.
Read positions do not advance the Bot execution CAS version. They retain their
own durable ReadAdvanced event and roster revision, so viewing a conversation
cannot invalidate a prepared native turn; actual Bot edits still fence stale
execution mutations.
The minimal roster journal survives rich event/aggregate deletion. It is
retained without automatic pruning; future retention must explicitly expire
old cursors. Recreated Bots use the latest journal revision.

Cursors bind machine, optional Project, principal, reader, grant/source access
and a process-local signing epoch. Restart, tampering, changed scope or grants
refuse with `pa_cursor_expired`; epochs never grant authority. Each serialized
publication rechecks authority, Project and conversation/generation identity.
An unavailable Project conservatively refuses the batch rather than exposing
any of its rows. The feed does not grant Project access that the resolver lacks.

The new registered bounds are 64 subscriptions per writer Store, one pending
content-free wake-up per subscription, a 3,600-second connection lifetime and
250 ms authority rechecks. Snapshots reuse 100 rows, replay 256 journal entries,
cursors 4 KiB, frames the 8 MiB plaintext budget, and writes the existing
2,000 ms Bot write deadline. A full wake-up slot latches lag and requires
resync. A blocked writer is interrupted on cancellation/revocation without
holding storage or actor locks. When possible, the response ends with a typed
SSE control or `X-Clawdline-Roster-Error: pa_cursor_expired` trailer. A broken
socket can lose both: EOF never advances a cursor. Reconnect from the last
applied cursor; start a fresh snapshot if refused.

Focused acceptance uses real SQLite, Service and `httptest.NewServer` SSE bytes:
`go test ./internal/adapters/projectassistantstore ./internal/transport/http
-run 'TestRoster' -count=1`. Set task-owned `CLAWDLINE_ROSTER_SSE_EVIDENCE` to
an existing directory to retain synthetic SSE transcripts. Coverage includes
subscribe/snapshot interleaving, concurrent paginated snapshots, offline
permission/idle/error/read/hide/removal replay, cursor and generation fences,
large histories that cannot aggregate-decode, read-only viewing, overflow,
blocked-write revocation, independent reads and production Store composition.
These checks make zero provider/model requests. They verify the local feed;
browser UI consumption, recipient-isolated Cloud delivery and public release
enablement remain separate work owned by integration.

## Files attached to a message

From API level 70, explicitly picked, dropped or pasted regular files can be sent
with text or in a files-only message. The Bot composer keeps removable draft chips.
Each general file may be empty, binary or extensionless, up to 100 MiB; filenames
are plain names of at most 128 UTF-8 bytes. A message carries at most four general
files, independently of the existing four picture blocks. Unsupported image formats
are not converted into general files. Session file drops still insert paths.

Uploads use UUID IDs and at most 384 KiB per authenticated request. The source
requires the current Bot, registered Project, conversation and draft execution
fence. Chunks must arrive in order; exact repeated bytes are idempotent, conflicting
bytes are refused. Size and SHA-256 must agree before a send can reference an ID.
A single durable message transaction binds each upload; replay uses the same send
receipt and cannot reuse the file in another message. No private path appears in
public metadata. Only the owned native prompt gets an Attached files list of
verified absolute private paths for native tools to read. The local fixture is a
synthetic ACP subprocess, not a real model or a provider readiness claim.

Other authorized paired devices can download sent inputs from the same Bot and
conversation in bounded chunks. The client checks size and SHA-256 before offering
a download. Queue takeback deletes those private files and reclaims their upload slots after
the never-started cancellation is durable. Cleanup runs outside execution locks;
a failed filesystem cleanup can be retried with the same takeback command.
Stop-dropped messages keep their Not sent evidence. Sent metadata survives restart. Source revocation, changed
authority and deletion are checked again before publishing each read response.
General files, native picture blocks and explicit output snapshots remain separate.
Send now remains unavailable for queued messages carrying attachments.

Files never enter an offline relay mailbox. A failed send retains the complete
selection for explicit retry. Reloading or leaving a draft loses its File objects;
a durable marker refuses a partial text-only send until the missing selection is
explicitly discarded or selected again. Cancel stops further upload chunks and
ignores late replies. Transfers do not resume in the background. To bound abandoned
partial data without evicting durable evidence, a Bot retains at most 128 upload
slots (finished or unfinished, at most 100 MiB each); new uploads then fail with a
capacity refusal. Taking back a queued message reclaims its inputs; deleting that
Bot reclaims the entire private input namespace.

Run the real HTTP/SQLite/owned-ACP input fixture with
`go test ./internal/transport/http -run '^TestProjectAssistantInputOwnedHTTP$' -count=1`.
Set the existing `PLAYWRIGHT_MODULE`, `PLAYWRIGHT_BROWSERS_PATH` and a task-owned
`CLAWDLINE_INPUT_E2E_DIR` for actual English desktop and Taiwan Traditional Chinese
mobile-dimension Chromium acceptance. This is local browser evidence, not production
Cloud acceptance or a real-phone test. No provider/model requests are made.

## The Bots tower

The building button in the Bots toolbar switches the roster between the list
and the tower: a pixel-art building where each Bot stands according to its
roster state. A wide screen opens on the tower until the person chooses; a phone
opens on the list. The choice, the layout, the last floor, the Bot look and the
name-tag switch are this browser's (`localStorage`, read and written inside
`try`/`catch`); the code is in `web/console/src/project-assistants/tower/`.

**Where a Bot stands.** Working Bots sit at desks, waiting ones queue, failing
ones go to the repair corner, recently active ones and idle ones stroll the
lounge, and those quiet for a day sleep. Placement follows the roster status
(`working.ts`), never the raw activity text. Each zone draws at most three Bots
(`bots.tower_zone_shown`); the rest are counted as "N more", which opens a list
whose names open that Bot's chat. Waiting and failing Bots keep a drawn place
first, then the most recently active. Name tags stay pinned for working,
waiting and failing Bots; the Name tags switch turns them off, after which a tag
shows on hover or keyboard focus while the canvas still marks who waits.

**Floors.** Four floors (4F, 3F, 2F, B1) each have a picture and coordinates in
`tower-scene.ts`. The building layout is offered from seven Bots across two
Projects. Each machine keeps a versioned document of floor placements in its
Bots database, served at `/v1/project-assistant-floors` (API 97) and carried
over Cloud as `project-assistants.tower-floors` and
`project-assistants.tower-floor-save`. A write names the version it read:
another device's change first is refused as stale and the panel reads the
floors again; a retried write whose reply was lost finds its own effect; an
unknown outcome reads again rather than resending. Projects nobody placed are
spread over the floors by a sort of their identities, so every device agrees.
A removed Project keeps its row, so registering it again does not move its Bots.
Open devices read placements when the tower mounts and when the panel opens;
they are not pushed live.

**The lead.** Each machine's lead Bot (the one marked with a star) wears a cape
and a gold star, always stands on 4F whatever floor its Project has, and
patrols a short beat before each floor's big screen. Waiting, failing, asleep or
with reduced motion it stands at the beat's middle. In the roster each lead has
its own section above Pinned.

**Helpers.** When a Bot hands work elsewhere (Board work links still accepted,
waiting for the person or executing, plus background tasks still waiting or
active; `active_work` on the Bot summary), translucent helpers in its shape
stand on the floor's portal. The HUD's Helpers chip and the portal itself open a
list of whose they are.

**Waiting and allowing in place.** The HUD's waiting chip lists every waiting
Bot across the building. A row can show the full request and, when the offer
has an allow-once option and the source may be written, allow it once: the
offer is read again and its fence, turn and deadline are checked before sending,
and an answer already sent from here is never sent again.

**Pictures.** The floor pictures are WebP, loaded only when the tower first
opens. A page opened before a hosted deployment may ask for the previous
deployment's removed files, which Pages answers with its HTML; the building
therefore fetches every floor's picture when it shows, requests a failed one
once more with a fresh query, and only then shows a Reload note. The elevator
doors rest open, because under reduced motion every animation is removed and
doors that only opened by animating stayed shut over the floor.

**Phone.** The picture pans inside its own frame and centres on a busy Bot; the
HUD keeps one row and the layout, look and floor controls fold behind one floor
button; panels open below the picture. A chat opened from the tower rises as a
bottom sheet with the floor still visible above, and another Bot can be tapped
from there.

**Browser check.** `tower/tower.e2e.mjs` serves the synthetic Bots fixture and
the floor pictures, refuses any `/v1` request, and checks Chromium and WebKit at
390 and 1440 wide in Taiwan Traditional Chinese and English: the building is
offered, no phone control covers the wide HUD, the floor picture loads, the
doors end open, opening "N more" does not move the floor, and a phone chat is a
bottom sheet over the tower. It installs nothing and calls no model:

```sh
PLAYWRIGHT_MODULE=<existing-playwright-index.mjs> \
BOTS_ARTIFACTS=<task-evidence-directory> \
node web/console/src/project-assistants/tower/tower.e2e.mjs
```

This is local synthetic evidence, not acceptance on a real phone or on the
hosted console.

## If something is missing

- *This Bot needs Claude Code 2.1.323 or newer and Node 22 or newer*:
  install or update them, sign in to Claude Code, then send again.
- A reply whose result is unknown is never sent again on its own. Your next
  message marks it as failed and continues.

Technical details are in [project-assistants-native-entry.md](project-assistants-native-entry.md)
and [project-assistants-server.md](project-assistants-server.md).

An existing, disabled Bot installation receives repeatable additive schema
migrations during Host composition before its history read service is exposed.
This seeds the roster journal from existing Bot identities without enabling
execution or constructing a provider. Fresh default-off installs keep no Bot
schema until opt-in; the borrowed read pool remains query-only.
