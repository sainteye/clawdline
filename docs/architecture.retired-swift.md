> **Retired Swift-generation record (through 2026-09-19):** The product reasoning is preserved, but Swift/AppKit/iTerm implementation details, source paths, route inventory, port 7717, `/tmp/.clawdline`, and claims about the running Mac app do not describe the Go daemon. References to unavailable retired files are rendered as code instead of live links.

# Architecture

One page for how this system is meant to be shaped, what shape it is actually in, and which
existing documents say otherwise. The owner set four directions on 2026-09-16; each has a section
below with what is true today, counted rather than recalled.

The bar this is all held to is [`acceptance.md`](acceptance.md). This page says what to build;
that one says how you know it worked.

---

## The one principle

The four directions are four views of a single rule, and it is worth stating on its own because
every measured defect below is a place it is broken:

> **A request serves what is already known. Work that cannot finish inside the request's budget
> happens outside the request — on a schedule, or on a change signal — and the request reads its
> result with a freshness stamp attached.**

The corollary, which is where the bytes go:

> **A response carries exactly one resource, plus the links to reach the others.**

Everything below is an application of those two sentences, or a place where they were not applied.

---

## 1. One core, two hosts

**Target.** The Mac app and the Linux build share a core: Git reading, transcript reading and
display, and everything else whose answer does not depend on which operating system asked.

### What exists

There are **two source roots**, not one:

| target | files | where the bytes live | compiled by |
|---|---:|---|---|
| `Clawdline` (Mac) | ~171 | `Sources/` | `build.sh`, and `test.sh`'s flat single-module compile |
| `ClawdlineCore` | 4 | symlinks into `Sources/` | via `ClawdlineApplication` |
| `ClawdlineApplication` | 16 | symlinks into `Sources/` | as above |
| `ClawdlineLinux` | 14 | **real files in `Packages/ClawdlineLinux/`** | `swift build --product ClawdlineLinux` |
| `ClawdlineLinuxTests` | 5 | **real files in `Packages/ClawdlineLinuxTests/`** | as above |

`tools/swift-source-manifest.sh` — the deterministic inventory both the app build and the test
build compare against — covers `Sources/` and contains **zero** entries under `Packages/`. So
nineteen production and test files have no manifest entry, and adding, removing or renaming one is
invisible to the check that exists to make exactly that visible.

Twenty of 194 files are in a shared target. That is the boundary carrying about **ten percent** of
the code.

### The template already exists, and so does the counter-example

The two things the owner named are one of each, which makes this section unusually easy to give
direction for.

**Git reading is right.** `Packages/ClawdlineLinux/LinuxGitReader.swift` is 1,669 bytes. Its own
header says what it is: *"Linux host adapter for the shared Git projection. Parsing, totals,
payload shape and the eight-second request budget live in `ClawdlineApplication.GitChanges`; only
bounded process execution belongs to the host."* One projection, two process runners. **Copy this
shape.**

**Transcript reading is wrong.** `Packages/ClawdlineLinux/LinuxNativeTranscriptReader.swift` is
37,197 bytes and declares its own vocabulary — `LinuxNativeTranscriptIdentity`,
`LinuxNativeTranscriptReadOutcome`. `Sources/Transcript.swift` is 2,230 lines and is in no shared
target. Two implementations of one thing, and the one that ships to the owner's phone and the one
that ships to a Linux runner can disagree about what a transcript says without anything noticing.

The work, then, is not "share more code". It is: **take the parse, the projection, the bounds and
the payload shape out of both transcript readers into `ClawdlineApplication`, and leave each host
holding only its file descriptors** — which is precisely what `GitChanges` already did.

### What `dispatch` actually is, and the order to take it apart in

**Measured 2026-09-16.** `RemoteServer.dispatch` is lines 928–3239 — **2,311 lines, 110 cases**,
not the "108 cases in 2,286 lines" quoted earlier in this document's life. Sixty-two match a
literal path; **forty-eight** match with `where path.hasPrefix(…)` and recover their own parameters
with `dropFirst`/`dropLast` arithmetic. The forty-eight hold the value, and they fall into five
families, each of them one id and one verb — which is exactly what `HTTPRoute.Table` expresses.

| family | cases | shape |
|---|---:|---|
| `/v1/sessions/{id}/…` | ~12 | `/links`, `/info`, `/skills`, `/git`, `/screen`, `/end`, `/title`, `/kill`, `/key`, `/focus` |
| `/v1/orchestrator/tasks/{id}/…` | 9 | the dispatch lifecycle verbs |
| `/v1/orchestrator/sessions/{id}/…` | 5 | state, complete, workflow |
| `/v1/orchestrator/{waits,schedules}/{id}/…` | 3 | release, run |
| `/v1/places/{id}/…` | 3 | start, resume |
| static assets | 3 | `/splash-`, `/icon-`, `/project-` prefixes |

The order follows the value, not the file:

1. **`/v1/sessions/{id}/…` first.** The largest family, and the one carrying the percent-encoding
   hazard: a terminal id is spelled `%18`, all twelve recover it by hand today, and `HTTPRoute`
   gives every one of them the same decode refusal in a line each.
2. **The seventeen orchestrator cases.** Same shape.
3. **`/v1/places`.** The three asset cases stay as `hasPrefix` lines: they are prefix matches on a
   filename (`/icon-256.png`), not segment patterns, and a route table that grew globbing to absorb
   three static files would be worse than the three lines it replaced.
4. **The sixty-two literal cases, last or never.** `case ("GET", "/v1/health")` is already
   declarative; moving it buys uniformity and no safety. A half-moved switch where some routes are
   declarative and some are not is the state that produced the string surgery in the first place,
   so this is worth doing only once 1–3 are all done.

`Sources/HTTPRouteTable.swift` is built and covered by `Tests/ReadPathTests.swift`, and nothing
outside the Board resources calls it yet.

### Where the transcript unification stands, 2026-09-16

The types a shared parser is written in are now shared: `SessionImageArtifact` (with its raster
decode injected, as `GitChanges` injects its process runner), `ClawdlineMessage`,
`ClawdlineSessionMessage`, `SessionImageMarker`, `Paths` and a new `TranscriptPaths` that gives
`Drop.directory` and `StartPoints.slug` one definition instead of two. `Ansi.plain` needed nothing
— it forwards to `TerminalSessionPresentation.plain`, which has been in `ClawdlineCore` all along.

**The partition is computed and it is clean.** Of `Transcript.swift`'s 103 members, 61 are
Mac-bound and 42 are not, and **no Mac member calls a `private` member that would end up on the
other side of the boundary** — which is the thing that would have made the split impossible rather
than merely tedious. `Codex.swift`'s parser produced no boundary errors at all.

| stays Mac | why |
|---|---|
| `record`, `sessionID`, `IdentityPass`, `freshIdentityPass`, `freshClaudeHookSessionID`, `claudeSessionID` | `TargetSession`, `SessionRegistry`, `HookBridge`, `Targets` |
| `title`, `customTitle`, `readTitle`, `lastTitle`, `locate`, `tail`, `tailData`, `signature`, the title caches | file I/O and its locks |
| `render`, `prose`, `cachedRender`, `remember`, `forgetRenders` | `NSAttributedString`, `NSFont`, `Style` |
| `Question`, `askPayload`, `openQuestions` | `QuestionSteps`, `SessionState` |
| `distinct`, `markdown`, `firstAssistantMessage`, `thinkingSignatureKind` | `Markdown`, which is 490 lines of AppKit rendering around a small pure part |

Everything else — the entry model, both parsers, `forEachLineFromEnd`, the text projections, the
question and workflow readers — names nothing a Linux host cannot see.

**What is not done, and why it stopped where it did.** The remaining step is to cut those 42
members out of a 2,230-line file into one shared file, leaving the other 61 as an `extension`. A
script was written for it and produced a partition with four bogus member names, which means it
mis-parsed some blocks. `Transcript.swift` is the most-read surface in the product; a mechanical
rewrite of it that is *almost* right corrupts a transcript in a way nobody notices until somebody
is reading one. So the cut is specified here rather than performed by a splitter that had already
shown it could be wrong.

Two whole-file moves were tried and reverted on the way: `Transcript`, `Codex`, `SessionState` and
`QuestionSteps` each pull a deeper Mac closure (`Log`, `Activity`, `RemoteServer`, `ITerm`,
`StartPoints`). Moving a file and letting the compiler find the boundary works for a leaf type and
does not work for a file with deep ties.

### The guard that was supposed to hold this does not exist

`tools/check-architecture-boundaries.sh` is referenced by `Package.swift` and by seven documents as
the thing that "fails closed if a link is missing, repointed, or drifts from the file it names".
**It is not in the tree.** It was removed with the other guard-shaped checks; nothing replaced it.

So the symlink integrity of `Packages/ClawdlineCore/` and `Packages/ClawdlineApplication/` is
currently unguarded, and every statement in those documents about ceilings, ratchets and
boundaries that "the guard holds" is a claim about a file that is not there. See
[What the existing documents get wrong](#what-the-existing-documents-get-wrong).

---

## 2. Two webs, one app

**Target.** The web front end has a cloud version and a local version; the cloud version goes
through `app.clawdline.com`.

### What exists

One application, ~43,000 lines under `Resources/web/app/js/`, with the split at the transport:

| area | files | lines |
|---|---:|---:|
| `view/` | 27 | 13,318 |
| `net/` | 21 | 12,958 |
| `input/` | 35 | 11,843 |
| `core/` | 13 | 2,842 |
| `session/` | 5 | 1,726 |
| `door/` | 1 | 267 |

This is the right shape: one app, one set of views, a transport chosen at boot. It is **not** two
front ends to keep in step, and it should not become two.

The asymmetry worth watching is inside `net/`: `cloud-client.js` is 4,224 lines against
`live.js`'s 928. A transport abstraction whose two implementations differ by 4.5× is an
abstraction that is leaking — the Cloud side is carrying protocol, pairing, replay and sequencing
concerns that the local side gets from the connection itself. That is not wrong on its face; it is
the first place to look when a behaviour works locally and not through Cloud.

### What this direction actually needs

Not a split. **A shared conformance test.** Whatever contract `live.js` and `cloud-client.js` both
implement should be exercised against both by one suite, so that "works locally" and "works through
app.clawdline.com" stop being two separate claims a person has to make twice.

---

## 3. Paginated reads that carry only what is needed

**Target.** The API and model layers page efficiently and give only the information needed.

This is the direction with the most work already done and the most measurement behind it. The
analysis, the mechanisms and the phase order are in
[`read-path-architecture.md`](read-path-architecture.md); this section is only its standing.

**Measured 2026-09-16.** `GET /v1/board?project=<id>` was 1,000,265 bytes and silently dropped 58
items to a byte budget (62 by the afternoon — the loss grows). `GET /v1/orchestrator/tasks` is
4,300,745 bytes for 716 records, unpaged. Twenty idle seconds of `GET /v1/events` cost one viewer
1,098,140 bytes, of which four `orchestrator` frames at 234,234 bytes each differed from their
predecessor in two or three clock fields.

**Landed:** identity dedup on the local event stream; the card contract and the removal of two
sub-objects with 62,801 bytes and no reader; `ETag`/`If-None-Match` with a real `304`; the split
resources `/v1/board/projects`, `/v1/board/projects/{id}`, `.../items`, `/v1/board/items/{id}` and
`.../history`, answering on the existing bounded read lane; an append-only history log; a route
table; and a delta engine that refuses when it has lost its base.

The byte budget is also gone from the paged route: `ProjectBoardReadCache.Model.cards` keeps every
card untruncated and the resources page over it, so the 62 unreachable rows are later pages. The
single `/v1/board` route keeps its bounded list and its wire unchanged.

History now lives in an append-only log beside the document, which keeps a twenty-entry window —
about 1.69 MB off every durable write. `GET /v1/orchestrator/tasks` is bounded and projected, with
every unfinished task on every page. `GET /v1/sessions/<id>/links` serves a maintained projection
instead of re-running a `git` per request. And the Board page asks for the audience it draws, on
both transports.

**The durable store is not the write cost, and this is measured rather than assumed.** One
`persist` is about 42 ms: 38.9 to encode 4.03 MB with `JSONEncoder(.sortedKeys)`, 2.9 to write it
atomically and 0.7 more to `fsync`. Ninety-three percent is the encode. So moving `receipts`
(685 KB) and `evidence` (942 KB) into segment files — which needs a journal, because a receipt and
the state change it acknowledges have to commit together or the idempotency guard lies in one
direction or the other — would save roughly **16 ms of a 1,230 ms write**. It is not worth the
journal, and the reason is a number rather than a preference.

What was worth it was one line of the same rule: every project envelope carried its own copy of
the catalog — 33 × 35 KB of dictionary copying inside the reply to every command — and the catalog
is now held once and spliced in on the way out. A Board command's reply went from **1.23 s to
0.20 s**.

**Not landed:** the read model is still rebuilt whole rather than incrementally, which no longer
costs enough to be the largest defect but is still the shape; `?since=` is built in
`SnapshotDelta` but not wired to a route or to the stream;
`dispatch` still holds its route cases in one function — measured below — and the transcript parse
is still implemented twice.

---

## 4. Nothing waits behind anything

**Target.** The system routes what a user needs immediately, is not blocked, and uses threads
effectively.

### The lanes exist. That is not the problem.

`RemoteServer` already separates work: a transcript lane, a slow-reading lane
(`/v1/places`, `/v1/screens`, `/v1/sessions/*/info`, `/v1/sessions/*/live`), a project-reading
lane, a Board command lane and a Board read lane, each bounded, none of them on the interactive
queue. That design is sound and should be kept.

**And yet:**

| operation | bytes | seconds |
|---|---:|---:|
| `GET /v1/sessions/<id>/info` | 1,732 | **0.719** |
| `GET /v1/sessions/<id>/links` | 691 | **0.383** |
| `GET /v1/screens` | 90 | **0.349** |

These are already on their own lanes, so **they are not queued behind anything. The work inside
them is slow.** A 90-byte answer that takes 349 ms is doing synchronous external I/O — an Apple
event, a process, a file walk — inside the request.

**Corrected by repeating the measurement**, which is the whole reason to repeat one. Five
consecutive calls each: `/info` is 574 ms then 1 ms — it has a cache and only the cold path is
expensive. `/screens` is 4 ms then 1 ms; the 349 ms above was a cold or contended first call.
`/links` is 380, 345, 365, 319 and 390 — flat, every time, because nothing was cached. So this was
**one route with no projection plus one cold start**, not three slow routes, and a plan built on
the first reading would have spent its effort in two places that did not need it.

Adding lanes cannot fix this, and neither can adding threads: the caller waits the same 349 ms
whichever thread it waits on. This is the principle at the top of the page, unapplied. `/v1/board`
serves a maintained projection and answers in 31 ms for a megabyte; `/v1/screens` performs its work
and takes 349 ms for 90 bytes. The difference is not concurrency, it is **when the work happened**.

### What this direction actually needs

For each slow route, in order: what is it doing inline, can that be maintained outside the request,
and what freshness stamp does the answer carry so a reader knows how old it is? `SessionWatch`
already does this for the session list. `ProjectBoardReadCache` already does it for the Board.
Neither is a new mechanism — they are the two examples to copy.

---

## What the existing documents get wrong

`docs/` holds 62 pages and 2.28 MB. `api.md` alone is 475 KB. The volume is itself a finding:
a reference nobody can read is a reference nobody checks, and several of the errors below survived
because they are on page four hundred of something.

### Claims about a guard that is not in the tree

`tools/check-architecture-boundaries.sh` does not exist. These state or rely on it in the present
tense:

| file | what it claims |
|---|---|
| `Package.swift` | the guard "fails closed if a link is missing, repointed, or drifts" |
| `docs/architecture-refactor.md` | the guard "holds the boundary", "pins that declaration", carries the `Orchestrator.swift` and `RemoteServer.swift` ceilings, and renders the governance table |
| `docs/mac-app-shell.md` | `RemoteServer.swift` has "**only 5 lines** of headroom" under `remote_server_ceiling=5570` at `:184`, with a reverse guard at `:488` |
| `docs/adr/0001-platform-boundary-and-evidence.md` | names it as the build/package evidence that reads SwiftPM resolved sources, edges and products |
| `docs/ubuntu-headless-runtime-plan.md` | cites its "real SwiftPM" check |

`docs/api.md` and `docs/machine-resource-scheduling.md` also name it, but as an example path inside
JSON samples about claim contention — those are illustrations, not claims, and are fine.

`docs/generated/platform-architecture-inventory.md` records the file as existing, and is **not**
wrong: it says on its first line that it is a fixed-tree baseline at `75314e95` and not a freshness
claim about HEAD. It is the model for how a generated artifact should describe itself.

The `mac-app-shell.md` row is the one that does damage rather than merely being stale: it tells the
next person that adding a case to `RemoteServer` will hit a ceiling, so they will design around a
constraint that is not there.

### The corrections

The guard's removal is a decision that was made and not written down anywhere. Until it is
replaced, the truthful statement is that **the `Packages/` symlink boundary is maintained by
convention and by `swift build` failing on a broken link, not by a check that inspects it.**
Restoring a boundary guard is listed in the order of work below; it is not the same thing as
restoring the file, because most of what that file guarded — line ceilings on two source files —
is not what the boundary needs.

---

## The order of work

Ranked by what unblocks the acceptance bar soonest, not by size.

| | work | direction | why here |
|---:|---|---|---|
| ~~1~~ | ~~Finish the read-path phases~~ | 3 | **done** except `?since=` |
| ~~2~~ | ~~Move the slow session reads onto maintained projections~~ | 4 | **done** for `/links`; `/info` and `/screens` turned out to be cold-start, not flat cost |
| ~~3~~ | ~~Correct the five documents above~~ | — | **done** |
| ~~5~~ | ~~Put `Packages/` under the source manifest~~ | 1 | **done** |
| ~~6~~ | ~~A boundary check worth having~~ | 1 | **done** — `tools/check-package-boundary.sh`, in the suite's guard phase |
| 4 | Cut the 42 shared members out of `Transcript.swift`, then point `LinuxNativeTranscriptWire` at them | 1 | the types are shared and the partition is computed; what is left is the cut and the Linux switchover |
| 7 | Adopt the route table across `dispatch` | 3 | stops the rest from being undone one special case at a time |
| 8 | One conformance suite over both web transports | 2 | ends "works locally" and "works through Cloud" being two claims |
| 9 | Wire `?since=` deltas to the reads and the stream | 3, 4 | the engine and its refusal are built and tested; nothing calls them |

---

## What this page does not establish

- Counts are of the tree at `404958d3`, 2026-09-16. File counts and line counts move; re-count
  rather than quote.
- The latency rows were taken at loopback on a machine whose load average had just fallen from
  17.74 to 4.32 after a runaway daemon was killed. They are good enough to separate *size* from
  *blocking*, which is what they are used for here, and not good enough to rank the slow routes
  against each other.
- No Cloud-path reading has been taken since the acceptance budgets were written, so direction 2's
  standing is a reading of the code, not of a measurement.
