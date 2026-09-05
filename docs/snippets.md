# Snippets

**This shipped.** It was written as a design first — one description for the sessions that built
it, so the decisions already taken were not retaken — and it is kept as one, because the reasons
are the part worth having written down and a reader who has only the code re-derives them wrong.
Every "does", "answers" and "rides" below is now something you can check against a running app.

**Where this page and the code disagree, the code is right**, and the seven places they did are
marked ⚠ in the text below rather than quietly corrected: a design that is silently edited to
match what was built stops being evidence of anything. Two *other* sentences were not
divergences but plain mistakes — arithmetic that did not hold and a claim about a request that is
made — and those are replaced rather than marked, because there is nothing about the design to
preserve in a wrong number. The wire is
[`api.md`](api.md#the-snippets-a-session-can-press), which was written from
[`Snippets.swift`](../Sources/Snippets.swift) and the route cases rather than from here.

- [What a snippet is](#what-a-snippet-is)
- [Where you press one](#where-you-press-one)
- [What a press does, and what it deliberately does not do](#what-a-press-does-and-what-it-deliberately-does-not-do)
- [Which snippets a session shows](#which-snippets-a-session-shows)
- [Where they live](#where-they-live)
- [The routes](#the-routes)
- [The list rides the snapshot](#the-list-rides-the-snapshot)
- [What the web app gains](#what-the-web-app-gains)
- [Words](#words)
- [Tests](#tests)
- [Not in v1, and what each would cost](#not-in-v1-and-what-each-would-cost)
- [Build order](#build-order)

## What a snippet is

A piece of text you wrote once and press instead of typing again. Two real ones, which are the
request this design came from:

- *"commit（逐檔指名，不要 git add -A）、push、deploy。"*
- *"回報你剛剛做了什麼、什麼還沒做、接下來要做什麼。"*

Both get typed several times a day, into a phone, one thumb at a time.

**A snippet is not a macro, a command, or an instruction Clawdline interprets.** Clawdline does not
read it, does not expand it, and does not act on it. It puts the words in the box; the person
presses send, and the assistant is the one that decides what they mean. This is the same line
dictation already holds — `input/voice.js` says it out loud: *the phone records, the Mac
transcribes, the words land in the box — and stop there.*

## Where you press one

Three entrances, one list behind them.

| | |
|---|---|
| **The project mark in the session header** | The shortcut. It is already the most identifiable thing on the phone's header, it already means *this project*, and pressing it costs nothing to reach. |
| **`⋯` → Snippets** | The discoverable one. A shortcut nobody can find is a shortcut nobody uses, and the overflow menu is where this app has put every other session-scoped action. |
| **The sheet's own `＋` and `編輯`** | Making and changing them, in the place where you are already looking at them. |

**The mark has to become a button of its own, and that is a real change to a header that works
today.** Right now `#detail-mark` is a `<canvas>` *inside* `#detail-info`, and the whole identity
block — mark, name and path — is one button that opens Session info
(`Resources/web/index.html`, `view/transcript.js:renderDetailHead`). The split is:

- `<button id="detail-snippets">` wrapping the canvas (`aria-haspopup="menu"`, labelled with the
  project's name), and
- `#detail-info` keeping the name and the path, which is what a reader points at when they mean
  "tell me about this session".

Two things fall out of that split and both must be handled, not discovered later:

- **The mark is small and a thumb is not.** Seven cells at 5px is about 35×20 CSS pixels. The new
  button carries its own padding to a 44×44 target and a `margin` that takes the space back, the
  way `.detail-session` already does.
- **A project with no mark draws nothing at all.** `core/pixels.js:drawIcon` sets the canvas to
  `0×0` when the session has no icon, deliberately, so a CSS placeholder can win — and
  `detail.css` has no placeholder for this canvas. So the button needs its own minimum box and a
  neutral glyph, or sessions in projects that were never registered in
  `~/.claude/project-icons.json` get an invisible shortcut. The `⋯` row still works there, which
  is exactly why the menu entrance is not optional.

  ⚠ **What shipped is better than a neutral glyph, and the paragraph above still describes the
  plan it replaced.** `Resources/web/app/js/view/project-mark.js` draws a *generated* mark: a
  pure function of the project path — FNV-1a over its code units, `Math.imul`, no randomness and
  no state — giving four rows of seven cells mirrored about the middle column, an ink and a
  ground from one hue, and an `accent` the session name is tinted with. So two sessions in two
  unregistered projects look like two projects rather than like the same "no icon" glyph twice.
  A registered icon always wins: `markForSession` returns `session.icon` whenever it has cells,
  and only reaches the generator when it does not. Its density is clamped to between 5 and 12 of
  the sixteen decided cells, because an empty mark is an invisible button and a full one is a
  rectangle — both identify nothing, which is the failure the neutral glyph would also have had.
  Measured over 20,000 paths, the worst ink-against-ground contrast is 3.71:1, past the 3:1
  non-text threshold. **The `⋯` row is still not optional** — a transport with no snippets route
  has no sheet to open from either entrance — but the sentence "the entrance that still works in
  a project nobody ever gave a mark", which `CHANGELOG.md` and `docs/interface.md` also carried,
  described a mark entrance that fails and there is no longer one.

  What the generator is hashed from is the project key **the Mac resolved**, once the sheet has
  read for that session — not the session's own `cwd`. Two sessions of one project, one of them
  in a worktree cut from its checkout, are then one mark and one name.

## What a press does, and what it deliberately does not do

Pressing a snippet row **puts the text in the composer and closes the sheet**. It does not send.

`input/composer.js:appendMsg` is already exactly this function — it joins to whatever is in the box
with one space, keeps the undo stack, re-renders the composer, tells the skill picker the box
changed, and scrolls to the end. Dictation uses it. Snippets use the same one; no second insertion
path.

Sending is the second press, on the button that already says 送出. That is one more tap than "one
tap sends it", and it buys:

- a mis-tap on a phone in a pocket cannot run `commit, push, deploy` in the wrong session;
- the ordinary case of *snippet plus one sentence* — "…, 但先跑測試" — needs no separate feature;
- the write gates stay where they are. The composer's send is the only thing that types into a
  session, and it is already the thing the remote write switch, the device capability and the
  `Idempotency-Key` are wrapped around.

An opt-in `send_on_tap` per snippet is listed under [Not in v1](#not-in-v1-and-what-each-would-cost)
with what it would cost.

## Which snippets a session shows

Two scopes: **global** (every session, every project) and **project** (only sessions sitting in that
project). Global is the common case and the default in the editor — that was the ask.

**The Mac resolves the scope key; the browser never computes it.** The rule is the icon's own rule,
because the icon is the button:

1. the registry path that `ProjectIcon.entry(forCwd:)` matched — exact, or a `path + "/"` prefix,
   which is what makes a session in a subdirectory the same project;
2. otherwise the repository's **common** directory, so a child running in a worktree under
   `~/Library/Application Support/Clawdline/worktrees/…` sees the snippets of the checkout it was
   cut from rather than an empty list of its own
   (`git rev-parse --path-format=absolute --git-common-dir`, then drop the trailing `/.git`);
3. otherwise the session's `cwd`.

`GET /v1/snippets?session=<id>` answers the resolved key beside the list, so the editor can say
*"只在 clawdline 這個專案"* using the same words the header uses.

Order in the sheet: **this project first, then global**, each in the order the person put them in.
Ranking by recent use is not in v1 — see the last section for why counting is not free.

## Where they live

```
~/.config/clawdline/snippets/<uuid>.json
```

One file per snippet, in `RemoteAuth.directory` — the same directory as `schedules/`, `remote.json`
and `orchestrator.json`, and therefore the same `CLAWDLINE_REMOTE_DIR` escape hatch the suite needs
so a test run does not edit the developer's own snippets.

This was decided on 2026-09-05: **project-scoped snippets are a setting of this Mac, tagged with a
project path — not a file inside the project.** The alternative, a committed
`.clawdline/snippets.json` beside `.devstack.json`, would travel with the repository and be
shareable with a team; it was turned down because pressing Save on a phone would then write an
uncommitted file into a working tree several agent sessions share, which is the one thing
[`AGENTS.md`](../AGENTS.md) is strictest about. It is still the natural second source and nothing
here blocks it; see the last section.

```json
{
  "id": "3f2a91c4-7e0b-4d68-8b41-6d0a2c9e5f31",
  "title": "Commit, push, deploy",
  "body": "commit（逐檔指名，不要 git add -A）、push、deploy。",
  "scope": "project",
  "project": "/Users/you/code/clawdline",
  "position": 300,
  "created_at": 1789700000,
  "updated_at": 1789700000
}
```

- `title` 1–60 **bytes of UTF-8**; `body` 1–4000; `project` 1–1024, and present if and only if
  `scope` is `"project"`; `scope` exactly `"global"` or `"project"`. ⚠ This read *characters*, and
  so did the store: `String.count` counts grapheme clusters, so four thousand ZWJ emoji were four
  thousand "characters" and about a hundred kilobytes on a disk, in an append-only audit file and
  on every snapshot broadcast — none of which pay in characters. `project` had no bound at all.
  The cost of counting bytes is real and worth saying out loud: sixty bytes is twenty Han
  characters, so a title in Chinese, Japanese or Korean is a third as long as one in English. ⚠ The `id` above read `3F2A…`; every id this
  store writes or accepts is a **lowercased** UUID, and one sent in any other case is canonicalised
  before it addresses anything.
- **The key set is exact.** An unknown key is `400 malformed_snippet` rather than a field quietly
  dropped, the way every other written object in this API behaves.
- At most **100** snippets on a Mac and **50** in one scope; over that is `409 snippet_limit_reached`
  with the count in the error.
- Typed refusals, each with a code of its own so a sheet can say which it hit:
  `malformed_snippet`, `snippet_not_found`, `snippet_too_long`, `snippet_limit_reached`,
  `snippet_scope_mismatch` (a `project` scope with no path, or a path with `scope: "global"`).
  ⚠ Three more came with the writing: `rate_limited` for the brake, and `write_failed` and
  `delete_failed` for a disk that would not take it — listed in
  [`api.md`](api.md#the-snippets-a-session-can-press) with what each one means.
- Writes are atomic — write to a temporary file, `replaceItemAt` — **and read back off disk
  through the same check that reads the list before the request is answered**, so a file this app
  cannot itself parse never survives the request that made it: a create removes it, an edit puts
  the previous bytes back. Deletes resolve `<id>.json` and
  nothing else, which is the whole of the path handling, exactly as
  [`docs/schedules.md`](schedules.md) does it. A file whose name is not a UUID has no id and is
  skipped rather than repaired.

## The routes

| | |
|---|---|
| `GET /v1/snippets` | everything, for the editor |
| `GET /v1/snippets?session=<id>` | `{"project":{"key":…,"label":…},"snippets":[…]}` — already filtered to that session and ordered |
| `POST /v1/snippets` | create; answers the stored record |
| `PATCH /v1/snippets/:id` | title, body, scope, project |
| `DELETE /v1/snippets/:id` | |
| `POST /v1/snippets/order` | `{"scope":…,"project":…,"order":[…]}` — the full order of one scope. `order` is the field name `POST /v1/orchestrator/landing-queue/order` already uses, and like that route it may reorder members and never add or remove one |

Reads take the ordinary read gate. **Writes take all three write gates in order** — the remote write
switch, this device's `send` capability, a non-empty `Idempotency-Key` — and land a line in
`remote-audit.jsonl` like every other write. That is deliberately the same bar as typing into a
session: both change what is on this Mac, and a device that may only read must not be able to
rewrite the owner's snippets. A brake of ten writes in ten minutes, copied from
`POST /v1/orchestrator/schedules`, keeps a looping client off the disk.

[`docs/api.md`](api.md#the-snippets-a-session-can-press) carries one section for the set, beside
the schedules routes.

⚠ **Two things this section got wrong.** These are paired-device routes and are *not* under
`/v1/orchestrator/`, so unlike the schedules reads the orchestrator token opens none of them — it
is never read here. And the brake is one bucket for all four writes rather than a copy of the
schedules one: ten writes in ten minutes across create, edit, delete and reorder together, `DELETE`
included, where `DELETE /v1/orchestrator/schedules/:id` is deliberately not braked at all.

## The list rides the snapshot

`RemoteServer.orchestratorSnapshot` gains `"snippets": Snippets.records()`, next to `"schedules"`.

**The quantity is not nothing, and this page used to say it was.** The 453 bytes in
`RemoteServer.swift`'s comment are a *measured* schedules payload beside a measured 1,056,958-byte
task payload — 0.043%. Snippets have no such measurement and a much larger ceiling: this page's
own limits are 100 records, each of them up to 60 bytes of title, 4,000 of body and 1,024 of
project path, which with the field names and punctuation is on the order of **520 KB — about half
the task payload, not a twenty-fifth of one percent of it.** The inventory is unfiltered by scope,
so every SSE stream and every cloud publication carries all of it. What makes that acceptable is
not the ratio: it is that a Mac with a hundred four-thousand-byte snippets is not a Mac anybody
has, and that the alternative on the Cloud path is no list at all. If somebody ever does fill the
store, this is the field to filter or to move off the snapshot.

Two things follow, and the second is the reason to do it this way:

- **On the Cloud path the sheet opens with no request.** `net/cloud-client.js` reads the rows out
  of the published snapshot, so there is nothing to wait for. **On the direct path it does make
  one**: `LocalClient.snippets` is a real `GET /v1/snippets?session=<id>`, because that is the
  answer that carries the resolved project and the per-session filtering the snapshot's unsorted
  inventory does not. It comes back in a few milliseconds from a server on the same machine, and
  the sheet is drawn before it arrives either way — but "no request" was never true here.
- **The Cloud path gets the list for free.** `net/cloud-client.js` already serves `schedules()` out
  of the published `orch/<machine>` rows; snippets arrive the same way, so a phone on the relay —
  which speaks no HTTP to the Mac at all and may ask for only [seven
  reads](api.md#the-reads-a-browser-on-the-cloud-path-may-ask-for) — can still press one, because
  `send` is already a command that path carries. **Editing** stays direct-path-only in v1: the
  create/edit/delete calls are absent from the cloud transport and every call site is guarded with
  `typeof api.createSnippet === "function"`, so the `編輯` and `＋` controls are not drawn there at
  all rather than failing when pressed. That is the pattern this app already uses for `/v1/places`
  and `/v1/push/key`.

## What the web app gains

| file | |
|---|---|
| `app/js/input/snippets.js` | new. The whole feature, modelled on `input/user-messages.js`: it injects its own stylesheet link, its own overlay sheet, and its own `#session-snippets` row into `#session-actions-main` before the Git row. |
| `app/css/snippets.css` | new. Rows, the scope headings, the editor. |
| `index.html` | the mark comes out of `#detail-info` into `#detail-snippets`. |
| `app/css/detail.css` | the two buttons read as one block; the new button gets its 44px box and its no-mark placeholder. |
| `app/js/view/transcript.js` | `renderDetailHead` sets `disabled`, `title` and `aria-label` on the new button the way it does for the two it already owns. |
| `app/js/net/live.js` | `snippets()`, `createSnippet()`, `updateSnippet()`, `deleteSnippet()`, `orderSnippets()`, on `LocalClient`. **Not** added to `ClawdlineClient.methods` in `net/client.js`, which is the contract every transport must satisfy. ⚠ This row said `net/client.js`; that file is the contract, and the direct transport is `net/live.js`. |
| `app/js/net/cloud-client.js` | `snippets()` only, from the published rows. |
| `app/js/net/mock.js` | a fixture: a couple of global snippets and one project-scoped, so `?mock=1` exercises the sheet, the grouping and the empty state without a Mac. |
| `app/js/view/project-mark.js` | new, and not in this table until the correction round. `generatedMark(key)` and `markForSession(session, projectKey)` — the mark a project has until somebody draws it one. Pure, importable into a bare Node process, and read by `Tests/web-snippets.mjs`. |
| `app/js/view/snippets-data.js` | new. The sheet's arithmetic with no document in it, plus the one thing the header borrows: the project the Mac last resolved for the open session. |

The sheet itself: a row is the title in the accent colour with the first line of the body under it;
pressing it inserts and closes. A row's own `⋯` gives 編輯 / 刪除 / 改成全域 / 上移 / 下移 — buttons
rather than drag, because dragging inside a scrolling sheet on a phone is a fight nobody wins. The
editor is one more sheet: title, body, a two-way scope control, Save, Delete.

Two small things worth building on the first pass rather than later:

- **The empty state offers the two snippets this design was written for**, as one-press starters. It
  is the cheapest possible answer to "what is this for", and it is the exact text the person already
  types.
- **`用上一則訊息新增`** — prefill the editor from the newest message you sent. `view/user-messages-data.js`
  already computes that list for the 我傳出的訊息 sheet, so it is a call, not a feature.

A read-only device shows the sheet and its rows, with the insert disabled and the reason on screen
(`S.write === false`), because the composer it would insert into is disabled too.

## Words

⚠ **Twenty-seven, not the twelve this said**, and they add up: the `⋯` row and the sheet's title,
one word between them (1); the two scope headings (3); the two empty states (5); the read-only
reason (6); the editor's two headings (8) and its three field labels (11); Save (12);
用上一則訊息新增 (13); the "needs a title and some text" refusal (14) and the "too long" one that
used to share its sentence (15); a row's own `⋯` label (16) and the six actions behind it (22);
the second press that confirms a delete (23); and the two starters, which are four strings because
each has a title and a body (27). The twelve was an estimate made before the editor and the empty
state existed; twenty-six was right until the correction round separated the two refusals, which
is what made a snippet with both fields full stop being told they were empty.

They are in `Sources/Strings.swift` and the fourteen `Copy` structs across thirteen `Copy+*.swift`
files — Chinese carries Traditional and Simplified in one file — with the English baked into
`core/i18n.js` as the fallback, and `RemotePage.swift` answering all twenty-seven on
`/v1/strings`.
That is the house rule, and the `⋯` menu is a shipped surface where every other row already comes
from there. `input/user-messages.js` carries its own two-language copy table instead; that was the
right call for a sheet reached from one place, and it is the wrong one here, because the shortcut on
the header is the first thing a non-English reader will press.

**The two starters are the one place a literal translation is the wrong answer.** Every other string
is the interface talking; those two are text a person sends to an assistant, so each language says
what somebody there would actually type. `Strings.swift` carries that instruction beside the
declarations so the next translator reads it before writing over them.

## Tests

- `Tests/SnippetStoreTests.swift` — round trip; unknown-key refusal; both limits; the atomic write;
  delete resolving `<id>.json` and nothing else; and the scope resolution, which is the part with
  real logic in it: exact match, subdirectory prefix, a worktree folding into its main checkout, and
  a cwd in no registry at all.
- `Tests/web-snippets.mjs` — the pure functions: grouping and ordering, the join rule when the
  composer already holds text, the guard that leaves the editing controls undrawn on a transport
  that lacks them, and the DOM contract that pressing the mark is not pressing Session info.

The correction round added to both. On the Swift side: a `%`-shaped tmux pane id end to end
through the route, the byte bounds and the bound on `project`, a `position` past its bound
isolating its file rather than trapping the next create, a project entered through a symlink, and
a counter proving each successful write republishes the snapshot and no refusal does. On the
browser side: the byte arithmetic against a second implementation of it, what the header may say
about a project, and three source-shape assertions for mutations that used to leave the suite
green — the read-only wiring reaching `draw`, the delete confirmation being reachable at all, and
the inert mark dropping its `aria-haspopup`.

One file each. `./test.sh` compiles every source file together with the tests in one `swiftc`
invocation with no cache between runs, so a second Swift test file is a real cost and this feature
does not need one.

[`docs/interface.md`](interface.md#the-mark-in-a-sessions-header) carries the paragraph on what the
mark opens; both READMEs carry the same feature line; `CHANGELOG.md` carries the entry.

## Not in v1, and what each would cost

Two of these were **declined outright on 2026-09-05**, not merely postponed, and are marked as such below. They stay on this list because a later reader will propose them again, and the answer is on the record rather than in a chat.

- **`send_on_tap`.** A per-snippet flag that sends instead of inserting. It is one boolean in the
  record and one branch in the row handler — but it puts a control on the header that runs code on
  the Mac in one tap, from a phone, in whichever session happens to be open. **Declined
  2026-09-05.** Every snippet inserts; the send button stays the only thing that sends.
- **Snippets committed in the repository** (`.clawdline/snippets.json`, read-only, merged under a
  third heading). This is the team-sharing answer and the reason the record carries a `scope` rather
  than just a path: adding a source later changes no stored file. Cost is roughly half the v1 work
  again — a second reader, a merge rule for identical titles, and the sheet having to say which rows
  cannot be edited here.
- **Placeholders** (`{{branch}}`, `{{project}}`, the current task's title). Needs an expander on the
  Mac, a preview in the editor so nobody presses a snippet whose text they cannot predict, and a
  decision about what an unresolved placeholder does. **Declined 2026-09-05.** A snippet is
  literal text; what you saved is what goes in the box.
- **Per-assistant snippets.** Claude Code and Codex take different slash commands; a snippet naming
  `/recap` is wrong in half the sessions. A `assistant` field, hidden by default.
- **Use counts and most-recently-used ordering.** The press never reaches the Mac — the whole design
  is that inserting is local — so counting needs a ping that exists only to be counted. Manual order
  first; if the list grows past a screen, that is the moment to reconsider.
- **A Mac-side editor in Settings.** The panel already lists schedules; snippets could sit beside
  them. Nothing about the store prevents it, and nobody has asked.

## Build order

Six pieces, each verifiable on its own, in this order — all six landed:

1. **Store and routes**, Swift only, no UI — provable with `curl` and the new test file.
2. **The snapshot field and the transports**, including the mock fixture.
3. **The sheet and the `⋯` row** — inserting works end to end; the editor is not there yet.
4. **The mark as a shortcut** — the header split, the tap target, the no-mark box.
5. **The editor** — create, edit, delete, reorder, the empty-state starters, `用上一則訊息新增`.
6. **The words and the pages** — the fourteen languages, `docs/api.md`, `docs/interface.md`, both
   READMEs, `CHANGELOG.md`.

Steps 3 and 4 are the ones a person can judge by looking, and step 4 is the one that touched a
header that already worked — it landed after the sheet had been used, not before.
