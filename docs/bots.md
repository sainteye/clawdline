# Persistent Bots: getting started

A Bot is a long-lived assistant bound to one Project. You chat with
it in the console; it remembers what you tell it, works in the Project with
its agent’s own tools, and can hand larger changes to an isolated worker
through Clawdline's Broker. The worker's result comes back to the same chat.

Bots are on by default. Open **Bots** in the Session list and choose the
starred lead assistant. The local lead and its personal workspace are created
without a Cloud account or a model request. The first chat checks the default
Claude Code and Codex accounts on this computer and shows the installation or
sign-in step beside the composer. Drafts stay in the conversation.

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

## Change the Bots setting

**Settings** → **Persistent Bots** controls Bots on this computer. A saved
off setting stays off after an update. Local-first Bots keep their source
identity and history when the computer later registers with Cloud. Cloud
viewers cannot access those local-first Bots yet. Machines with an existing
Cloud identity keep their current Bot source and pairing rules.

From a terminal, the same switch is:

```sh
clawdline project-assistants enable
```

`clawdline project-assistants status` shows the current state.
`clawdline project-assistants disable` turns Bots off and keeps saved Bots
and conversations. `clawdline project-assistants setup-claude` runs the
first-use setup ahead of time and prints which Claude Code version it found;
`setup-codex` does the same for Codex.

## Bots settings: dispatch rules and memory for every Bot

The gear above the Bots list opens **Bots settings** as a modal (full screen at phone width) with a
side navigation: **Dispatch** and **Shared memory** (two tabs above the pane at phone width). Its
document is stored in `project-assistants/shared-settings.json` and served at
`/v1/project-assistants/shared-settings` (GET; PUT with `expected_version`, whole document). An empty
document keeps the earlier behavior.

- **Default agent.** Four tiles, one per agent (Claude Code, Codex, Gemini, Grok Build), each with
  the agent's own logo. An agent this computer cannot dispatch is shown as "not on this computer"
  and cannot be chosen. The choice, with an optional model and effort, is stored as `default`;
  absent means Claude Code, and Claude Code with nothing else set is stored as absent. A saved
  default whose agent later becomes unavailable falls back to Claude Code.
- **Kinds of work.** A table of rules; each row opens the kind's own page: the work kind (for example
  寫程式, 設計, 生圖), a description that helps the Bot recognize it, a Project scope, and all four
  agents as a list where the order they are picked is the order they are tried (up to four). Each
  picked agent carries a model and an effort. Claude Code and Codex are available when installed;
  Gemini and Grok when their owned session host runs, the same condition under which the Broker
  admits their dispatch. Effort lists only the values the agent's launcher accepts (Codex
  `high`/`xhigh`, Gemini `low`–`max`; Claude Code and Grok none). **Selection is ordered fallback**:
  the first listed agent available here runs the work. When none of a rule's agents is available,
  dispatch fails with `pa_unsupported` `dispatch_rule_agents` instead of silently running another.
- **Model drop-downs** read the machine's [model catalog](model-catalog.md) through
  `useModelCatalog`: one read answers every agent, no Bot is chosen, and the phone reads it over
  the Cloud word `model-catalog`. The first entry is "Agent default (<what it runs>)", meaning no
  model is named. The list shows its loading state (also while the daemon is still asking the
  agent), a failure with a retry, that the agent reported no other models, or that an older
  machine cannot list models; it opens upward when the pane has no room below. A saved model the
  agent no longer lists stays selected and is marked.
- **Precedence in `dispatch_work`**: explicit `agent`/`model`/`effort` > the rule for `kind` > the
  saved default > Claude Code. With no rule and no default, dispatch is exactly as before.
- **Child or Root.** `dispatch_work` opens an owned child by default (`mode: "child"`): bounded
  work that returns a branch for review. `mode: "root"` opens an independently owned Root Session
  for integration, release or a whole handed-off feature; it may merge, push, deploy and record
  landings under the ordinary Root rules, and its `clawdline session report` tells the Bot once.
  Only a turn the person started, or the holder of a request card, may open one
  (`docs/project-assistants-broker.md`, "Opening a Root").
- **Permission.** A Bot's dispatch carries no permission of its own: it is admitted exactly as a
  Session's child is, so every agent, Claude Code included, runs with the person's dispatch
  permission setting, and Grok follows the child-dispatch mapping.
- **Memory for every Bot.** Rows of text, each with a Project scope: *All Projects (including future
  ones)* by default, or chosen Projects. A row whose Projects were all removed applies nowhere. The
  rows that apply enter a Bot's memory opening as `<shared_memory>` before its own memory; its
  instructions and own memory take precedence. Each Bot's settings page has **Use memory shared by
  every Bot** (on by default), stored in the same document (`memory_opt_out`).
- **Limits** (`internal/domain/capacity`): 20 shared rows, 1 KiB each, 4 KiB total; 12 rules,
  4 agents per rule; 64 Projects per scope.
- **Project overrides** are reserved in the stored shape (`project_overrides`) and already win in
  rule resolution, but the console edits only the machine-wide lists in this version.
- **Known limits of this version.** A Bot whose native session is resumed reads changed shared
  memory at its next fresh session, compaction or own memory change, not immediately. Cloud and the
  phone do not carry this route yet; the modal reports that settings cannot be read there.

## Team charter: how the Bots of one Project work together

Each Project can have one **team charter** (團隊章程): plain Markdown you write, in any shape you
like, saying who does what, how work flows and what must be asked of you. Every Bot in that
Project follows it, including the hidden workers that answer requests from other Bots. Where the
charter and a Bot's own instructions or memory disagree, **the charter wins**.

- **Versions.** Every save is a new version that never changes afterwards; the current charter is
  the newest version. Each version records who made it (`person`, `lead_proposal`, `restore`,
  `template`), an optional reason and when. Restoring an old version saves a copy of it as the
  newest version, so nothing is lost. A save names the version it was based on: if someone else
  saved first, it is refused (`pa_stale_version`, field `charter_version`) and you reload.
- **Asking the lead.** You can also ask the lead Bot (主助理) in words, from the charter page or
  in its chat. The lead answers with a proposal: the whole new text and one line saying which
  sentences it removed and added. **Nothing changes until you press Apply.** A proposal written
  against an older version is marked stale and Apply refuses it (`pa_stale_version`, field
  `charter_proposal_base`); dismiss it or ask again. The lead's `propose_charter_edit` tool is
  offered only in its turns that come directly from you.
- **Removing it.** 移除章程 on the charter page (API 123, `POST …/remove` with the
  `expected_version` you read) takes the charter away. It saves a `removed` version with empty
  text: the Project then reads as having no charter (version 0), its pending proposals go, a Bot
  already in a conversation is told once that there is no charter
  (`<team_charter_update version="0">`), and a fresh conversation carries none. The removal keeps
  its number, so a charter made later is numbered after it and every Bot hears that one too; a
  removed version cannot be restored. Personal workspaces belong to one Bot and are not offered
  a charter in Bots settings.
- **When a Bot hears it.** A Bot starting a fresh conversation reads the current charter first,
  as `<team_charter version="N">`, ahead of shared memory and its own memory. A Bot already in a
  conversation reads a changed charter once, at the start of its next prompt
  (`<team_charter_update version="N">`); a charter change never restarts its conversation. The
  charter page shows, for each Bot, the version its latest prompt carried and whether it is
  working now, so you can tell who has not seen a change yet.
- **Limits** (`internal/domain/capacity`): 32 KiB of text per charter; 200 versions kept per
  Project (saving past that removes the oldest, never the current one); 20 pending proposals per
  Project (a new one is refused until you apply or dismiss one); 4 KiB for a request to the lead.
- **Routes** (API 122, paired devices with this machine's Bots authority; every write carries
  `Idempotency-Key`): `GET /v1/project-assistant-charters?machine_id=` lists Projects with a
  charter and those with Bots but none; `GET …/charter?machine_id=&project_id=&version=` reads the
  current charter with its history (`version` 0, or a retained version to read its text);
  `PUT …/charter` saves; `POST …/restore`; `POST …/remove` (API 123); `GET …/readers`; `GET …/proposals`;
  `POST …/proposal-apply`; `POST …/proposal-dismiss`; `POST …/change-request` sends your words to
  the lead. The contract is `api/v1/project-assistant-charters.schema.json`. The same operations
  cross Cloud as the `project-assistants.charter*` words, so a phone can read and edit.
- **In the console.** A Project with a charter shows a 團隊章程 row first in its roster section;
  it opens the charter where a chat would be, one card per `## ` section with a pencil to edit
  that section (its title and body as separate fields, so clearing the body never drops the
  heading; saved as the whole document with `expected_version`), the lead's pending
  proposal as a red/green line diff with Apply, Dismiss and Revise, a 版本紀錄 side panel with
  each version's diff and a restore, and a composer that sends a change request.
  The page rereads every ten seconds while visible, at once when the tab becomes visible or the
  charter is opened again, and every two seconds for two minutes after a change request, so the
  lead's proposal appears without a reload even in a background tab. Bots settings
  has a 團隊章程 tab listing each Project and creating a first version from a five-heading
  template. Every call goes through `web/console/src/project-assistants/charter-api.ts`; the
  fixture-backed browser check is `charter.e2e.mjs` (`PLAYWRIGHT_MODULE`, `CHARTER_ARTIFACTS`).
- **Storage.** The charter tables live in the Bots database next to the tower floors, outside any
  Bot's own record, keyed by machine and Project. They were added without a schema version bump,
  like the tower floors: an older daemon ignores them. An older daemon that rewrites a Bot drops
  the recorded charter version, which only means the next prompt carries the charter once more.

## Use a Bot

To trigger a Bot routine from GitHub or another service, see [Bot Webhooks](user/bot-webhooks.md) · [繁體中文](user/bot-webhooks.zh-TW.md).

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
   stays in the trace. A reply to another Bot's request, including one forwarded
   after the person answers its question, carries all the text after the turn's
   last tool call instead: an agent can stream one answer under several message
   ids, and the last segment alone once delivered only its closing status lines. Longer reports can be published as an HTML artifact.
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
default. Choose the model label to open the model list. For Claude Code, Codex
and Grok it is the machine's [model catalog](model-catalog.md), the same list as
Bots settings, with an effort beside it when the agent has no effort option of
its own; the choice is saved as `model` or `model/effort`. The daemon maps it to
the agent's ACP value when it validates and applies it
(`modelcatalog.ToConfiguration`), and a value saved before the catalog keeps
working and shows as its catalog entry (`gpt-5.5/high` as gpt-5.5 with high,
Claude's `default` as the agent default). A choice the agent does not offer is
refused, not guessed. Gemini keeps the Bot's own ACP list: the catalog's
`agy models` slugs are not proven to be the ACP adapter's model ids. Other
switches (fast and the like) still come from the Bot's own configuration answer.
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

### Dispatched Sessions (分身)

A Bot that dispatches broker Sessions shows them in three places, all governed
by one switch, **Show dispatched Sessions** in the roster's options menu (on by
default; stored per browser). With it off, rows, avatars, the tower and the
chat footer look exactly as they did before.

- **Take over dispatched work.** On by default in Bot settings (「接手派出的工作」).
  When a dispatched Session finishes, fails, times out, is cancelled, cannot
  start or first waits for you, the Bot gets one review turn, marked as a
  report rather than your instruction, and tells you what happened. Off: the
  result is only posted in the chat. The Bot can still ask for the review of
  one task explicitly. See
  [Waiting for dispatched work](project-assistants-broker.md#waiting-for-dispatched-work).
- **Roster count.** A two-outline mark and a number under the row's time:
  dispatched Sessions accepted, executing or waiting for the person. Delivered
  (awaiting integration), failed, landed and background threads are not
  counted. With nothing running the mark takes no space.
- **`waitingOnWork` state.** A Bot whose own chat is idle while its dispatched
  Sessions run shows "Waiting for N Sessions" / 「等 N 個分身完成」. Precedence is
  `needsInput` (own permission, a background thread or a dispatched Session
  waiting for the person) > `error` > `working` (own turn or background
  threads) > `waitingOnWork` > `idle`. The avatar draws a thinner second
  outline that drifts up-right and back every 4 seconds (translation only);
  with reduced motion it is a still, dashed outline. The tower places the Bot
  at its desk in the `oversee` mood. In unread-first order these Bots join the
  working section after Bots that are working themselves, and that section now
  reads the same state the row shows (a Bot working through background threads
  used to be shown as working but not lifted).
- **Chat strip.** Above the composer, one grey line "N running · M need you ·
  K awaiting integration" shares the footer line with the background capsule.
  Opening it lists at most `bots.dispatched_strip_rows` (5) Sessions with their
  task title, state, elapsed time and destination Project (tagged when it is
  not the Bot's own); a row opens the Session by its terminal address. Each
  row says in plain words what it waits for, read only from the task record
  and its landing state: waiting for you to answer the worker, running,
  starting, delivered and waiting for the Bot to merge it (landing
  `pending`), delivered with no landing recorded (an older source, or a task
  the broker opened no landing for), or failed with changes nobody has dealt
  with (failed and landing `pending`). The broker's landing note is the row's
  tooltip. A finished Session stays listed, with no time limit, until its
  landing becomes `landed`, `incorporated`, `abandoned` or
  `nothing_to_land`, and leaves the moment it does; past
  `bots.dispatched_strip_rows` the rest are counted as "N more". The former
  one-day window `bots.dispatched_delivered_seconds` is retired: it hid
  unfinished work. Its constant survives only as the completion wake retry
  deadline, registered as `bots.completion_wake_retry_seconds`. A delivery
  that another task cherry-picked closes when that carrier declares it in
  `carries` and lands (`docs/broker.md`).

The facts come only from broker task records projected into the Bot's work
links (`AssistantWorkSnapshotOf`), never from what the model says. The roster
summary carries them as the optional `dispatched_work {running, needs_you,
delivered}`, where `delivered` counts every finished link whose landing is
still owed; `status` keeps its four values because the console rejects a
whole roster frame with an unknown status. Work links carry `title`,
`project`, `terminal`, `started_at`, `finished_at`, `landing` and
`landing_note`. Older sources omit
these fields and the console shows what it showed before. The chat strip reads
the work links in the loaded timeline, so a link older than the loaded pages
is counted on the roster but not listed until earlier messages load.

This first projection is exercised through the real local Store and HTTP
boundary with a synthetic event producer, without a provider/model call.
Set task-owned `CLAWDLINE_ROSTER_E2E_DIR`, `PLAYWRIGHT_MODULE` and
`PLAYWRIGHT_BROWSERS_PATH`, then run
`go test ./internal/transport/http -run '^TestProjectAssistantRosterStatusHTTP$' -count=1`.
Its current browser evidence explicitly records `eventUpdates: false`:
the console still consumes snapshots. Automatic console updates and encrypted
Cloud subscriptions remain pending; the local feed below does not establish
either destination's acceptance.


#### When the agent compacts its own context

Claude Code and Codex summarize a full context by themselves. Clawdline records it from what the agent
actually reports: a `compaction_update` with status `completed`, or, only for an agent that never sends
one, a usage reading below half of a previous reading of at least 20K tokens (the rule Codync uses). One
compaction counts once even when both signals arrive. Nothing is guessed from time or message counts.

- The turn in which it happened gets one persistent chat notice, `compacted-<turn id>`: "Earlier context
  was summarized to make room / 已摘要較早的內容，騰出空間：345K → 41K" (the numbers appear only when the
  agent reported readings on both sides). Only a successful turn writes it.
- The next turn on the same native binding repeats the Bot's memory once, through the same
  `<memory_update>` renderer and filtered snapshot a resumed session uses when memory changed, so there
  is one source. Instructions are not repeated: Claude Code and Codex keep them in the system prompt,
  which survives compaction, and Gemini hears them on every open. The guard is the persisted notice
  itself: only the most recently started earlier turn of the resumed session counts (each turn mints
  a new binding ID, so it is the binding being replaced), a later turn disarms it, and it survives
  restarts.

#### Compact now and New session from the context meter

Clicking the context meter under a Bot chat opens a small menu (a bottom sheet within the Safe Area on
a phone): **Compact now**, **New session** and **Cancel**. New session is the existing
roster action and confirmation (`fresh_context`): it stops any active turn, invalidates the earlier native
session without carrying a handoff, clears the previous `Last CTX` reading, and confirms above the composer
that the next message starts with a fresh context.

Compact now is `POST /v1/project-assistants/{bot}/messages` with `compact: true`, empty text and an
idempotency key; Cloud carries it as the same `project-assistants.send` word. The source admits one
ordinary, journaled turn with origin `compact` and the chat notice "Compact now / 立即壓縮". Its only
prompt is the agent's own command: Claude Code gets `/compact` followed by a keep-list, Codex a bare
`/compact`. Nothing else is added (no instructions, memory, quote or files), the session's memory
revision is not advanced, and the agent's words about compacting are not posted as a reply.

- **Path.** The compaction is a turn, not the native maintenance port. `BeginCompaction` reserves a
  whole ACP client and no production binding supplies its journal; a turn already has a receipt,
  idempotency, the queue, Stop and restart recovery, and its client closes when it ends, so the next
  message opens a new one.
- **Only while idle.** Refused with a typed error and no write while a turn runs or anything is queued
  or unknown (`pa_capacity_full` / `compact_busy`), before the Bot has a native session
  (`pa_unsupported` / `compact_no_session`) and for other agents (`compact_agent`). The menu disables
  the item and says why. A fresh session is never opened to compact: a queued compaction that would
  need one ends failed before anything is sent.
- **Result.** The agent's `compaction_update` produces the same `compacted-<turn id>` notice as an
  automatic compaction, with before → after usage (the before reading falls back to the last turn's
  stored reading), and the next message repeats memory once. An answered `/compact` that reported no
  compaction ends the turn failed with `compaction_not_reported`; an agent that listed its commands
  without `compact` is refused before the write (`native_compaction`). Neither leaves a turn that
  blocks the next message. A repeated idempotency key returns the first receipt and sends nothing.

Compared with Codync (`9dae3e2b`): Codync has no "Compact now" button; a person types the agent's own
`/compact`, which it passes through when the agent advertised it
([turn.rs#L255-L261](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/turn.rs#L255-L261)).
Its host-timed compaction is a hidden turn that sends `/compact <keep-list>` to Claude and a bare
`/compact` to Codex
([compaction.rs#L114-L121](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/compaction.rs#L114-L121),
keep-list [memory compaction.rs#L176-L178](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/crates/memory/src/compaction.rs#L176-L178));
Codex hears the keep-list as `compact_prompt` in its launch config
([session.rs#L196-L197](https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/host/src/agent/bot/session.rs#L196-L197)).
Deliberate differences: Clawdline offers an explicit menu item instead of a slash command, because its
composer does not list agent commands; the request is a visible notice rather than a hidden turn, so
it has the receipt and failure of any message; Codex's launch configuration is not changed to carry
`compact_prompt`; and an unadvertised command list does not block the send when the agent has not
yet reported one, because Claude Code and Codex both implement `/compact` and an answered prompt that
reports no compaction is a definite failure. See `docs/research/codync-context-recovery-2026-11-23.md`.

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
first, then the most recently active. The Name tags switch pins a name for
every Bot drawn in the scene, including in the building overview. Busy Bots on a
floor also show an activity line. With the switch off, hovering or focusing a
Bot still reveals its name.

**Unread replies.** A Bot with unread replies carries a New reply badge on its
floor and in the building overview. The badge stays visible with name tags
off. A crowded zone's “N more” button and list also show when a Bot inside has
an unread reply. Reading the chat clears the badge when the roster refreshes.

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

**Scene controls.** The layout, look, name tags and elevator start collapsed
behind the floor button on desktop and phone. Choosing a layout or floor closes
the menu.

**Phone.** The picture pans inside its own frame and centres on a busy Bot. The
HUD keeps one row and panels open below the picture. A chat opened from the tower rises as a
bottom sheet with the floor still visible above, and another Bot can be tapped
from there.

**Browser check.** `tower/tower.e2e.mjs` serves the synthetic Bots fixture and
the floor pictures, refuses any `/v1` request, and checks Chromium and WebKit at
390 and 1440 wide in Taiwan Traditional Chinese and English: the building is
offered, the controls start collapsed and clear the wide HUD, name tags toggle
on resting Bots and in the building overview, the floor picture loads, the doors
end open, opening "N more" does not move the floor, and a phone chat is a bottom
sheet over the tower. It installs nothing and calls no model:

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
