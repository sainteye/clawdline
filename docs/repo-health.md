# Governance review: one process, started by hand, that keeps a project the right size

Status: **proposed design, 2026-10-06.** Nothing here is built. Every claim about how Clawdline
behaves today cites the file and line it was read from; anything not checked is labelled
*assumption*. The feature is called **Governance review** (治理檢視), not "health": `health` already
means a service health check in Project status (`internal/adapters/projectlinks/status.go:227,462`,
`docs/project-status.md:363-419`), and a second meaning of the same word on the same Project card
would make both unreadable. The file keeps the name `repo-health.md` because that is the path this
task was given.

## 1. The problem

A person runs several projects through Clawdline the way an executive runs a company: they decide
what gets built and leave the building to agents. On this repository they can no longer answer
three questions without reading code they do not want to read:

1. **How big is each part, and where is it redundant?** Go production code is about 194k lines and
   Go tests about 109k (measured 2026-10-06 with `git ls-files | xargs wc -l`, excluding
   `experiments/`). Two versions of the work system coexist (`internal/adapters/store/work.go`
   beside `work_v2.go`, `docs/work-system.md` 48 KB beside `docs/work-system-v2.md` 71 KB). Nobody
   has said whether the first is still needed.
2. **Which documents matter, for whom, and what do they cost?** `docs/` holds 117 tracked Markdown
   files, 2.88 MB, about 720k tokens at the ledger's estimate of bytes ÷ 4
   (`internal/adapters/transcript/ledger.go:1015`). 18 top-level docs are named by no other tracked
   file (basename search, 2026-10-06). The largest, `docs/limits.md`, is 125 KB and opens with a
   note that it is a dated record (`docs/limits.md:1`), yet nothing stops an agent reading all of it
   to answer one question about one bound.
3. **Which rules and guards still earn their place?** There are about 50 guard tests named
   `TestEvery…`/`TestNothing…` and seven `tools/check-*` scripts. No record says when any of them
   last caught something: `tools/check.sh` deletes its logs when every check passes
   (`tools/check.sh:98-99`).

What the person refuses is just as specific: **no new step or token on ordinary agent runs.** The
token ledger explains why that matters. In the week of 2026-09-25, 73% of cost was cache reads, and
a file's real cost is its size times the calls left after it was read (`docs/token-ledger.md:11-13`).
The always-loaded rules are already small (`CLAUDE.md` + `AGENTS.md` = 10.4 KB). The expensive thing
is an agent reading a large document once and carrying it for the rest of its session. Our documents
mix the current specification with decision history of the same size, so an agent cannot tell what
it may skip.

### Who it is for

- **The person who owns a project.** They start a review, read one page, and make at most three
  decisions. They never need to read a metric to approve a change.
- **Any Clawdline user, on any repository.** This is a product feature, not a tool for this
  repository. So nothing below may assume this repository's scripts (`tools/check.sh`), its Go-only
  layout, or its owner's machine. This repository is the first customer and supplies the baseline.

### Non-goals

These are decisions, not omissions.

- **No new always-loaded instructions.** No line is added to `CLAUDE.md`, `AGENTS.md`, `MEMORY.md`,
  the child briefing or the `clawdline` skill's core. *Why:* the ledger shows that resident text is
  paid on every call of every session.
- **No per-commit or per-turn agent review.** Nothing runs on commit, push or landing. *Why:* that is
  the daily cost the person refused, and fitness checks that matter already run in `tools/check.sh`.
- **Daily sessions never read review output.** Snapshots and reports live outside the repository
  (§6), and nothing points an ordinary session at them. *Why:* if they were read, they would become
  the next large document that agents carry around.
- **Not scheduled in the first version.** A run starts when the person asks for one (scope change of
  2026-10-06). Schedules come in a later, optional phase (§11, P5).
- **No automatic changes.** The process recommends and the person approves. Implementation goes
  through the Board's existing gates.
- **No new analysis services and no hosted analysis.** The daemon measures locally. Cloud never sees
  a plaintext metric (§6).

## 2. The process

A run is one action that starts a pipeline. The pipeline stops at one required human gate and then
continues through the Board as ordinary work.

| # | Stage | Who | Unattended? | Output |
|---|---|---|---|---|
| S1 | **Measure** | daemon, deterministic | yes, zero tokens | `snapshot.json` (§4) |
| S2 | **Diagnose** | conductor Root (`zero-review-lead`) + read-only reviewer children | yes | findings, each citing snapshot rows or `file:line` |
| S3 | **Recommend** | conductor | yes | refactor, document and governance recommendations, each with reason, cost and expected benefit |
| G1 | **Approve** | the person | **no — required gate** | up to 3 `answer` Notes, each answered by a tapped reply |
| S4 | **Implement** | Epic children (Feature/Issue), any persona the conductor assigns | per item, through the Board's own gates | commits, landed by the owner |
| S5 | **Slim documents** | `technical-writer` children, as Epic children | same as S4 | smaller docs, records moved (§7) |
| S6 | **Close and compare** | conductor | yes | `report.md`, the delta against the previous run, and the run's own cost (§9) |

G1 is the only gate this design adds. Every later approval reuses an existing one: the Epic plan
gate before implementing (`internal/domain/work/v2.go:555,737`, `clawdline guide epic`), the
person's per-Feature **Needs independent review** switch, and the verification gate that dispatches
a checker child automatically when an item enters `verifying` (`internal/app/work_v2.go:1594-1609`,
`internal/app/work_v2_gates.go:337-344`).

### How the person approves (G1)

The conductor creates one `answer` Note per decision, at most three per run. Each Note has 2–4
suggested replies, for example *"Approve R1: retire the work v1 store; create the items"*, *"Defer
R1 to the next review"*, *"Reject R1: v1 is still needed because …"*. This is the existing mechanism,
and it already does what G1 needs:

- Note kinds, and 2–4 `{label, draft}` options only on `answer`: `internal/app/human_interventions.go:127-151`.
- A tapped reply is sent as a conversation message through `POST /v1/sessions/{id}/send`
  (`docs/human-interventions.md:11`). That route is the only one that **issues a run**, the proof
  that the person said something (`internal/transport/http/runs.go:20-24`).
- With that run, the conductor may create the approved Board items, because the person's own message
  asked for them (`clawdline guide board`). A Note marked handled by hand sends nothing and
  authorizes nothing.

Why not a Board `Decision` (`internal/domain/work/proposals.go:885-958`)? A Decision **applies its
default after a due time** (7 days by default). An approval that happens by itself is the opposite of
G1. Notes never time out.

## 3. Starting a run on demand: what exists today

The person asked for a run to start from one action in Clawdline. Here is what the code offers now.

**Starting a session or child with a persona works today, on four paths:**

| Path | Evidence |
|---|---|
| `clawdline dispatch --persona <id>` (a child) | flag at `cmd/clawdline/dispatch.go:115`, checked at `:203-207`, written into `task.json` at `:479-480`; admitted by `internal/app/orchestrator/draft.go:88,99-113` |
| A Root assignment with a persona | `RootAssignmentRequest.Persona` at `internal/app/orchestrator/handoffs.go:664`; `ASSIGNMENT.md` gets a persona section at `:775-788`; route body at `internal/transport/http/handoffs.go:95` |
| A Board item assigned to a new Session with a persona | `POST /v1/work/v2/items/<id>/assign` takes `persona` (`internal/transport/http/work_v2.go:1942-1944`) and opens a Root assignment with it (`:2229-2231`); the console sends it from "assign to new Session" (`web/console/src/pages/work/api.ts:543-550`) |
| An Epic owner assigning its children with personas | `workV2EpicAssignRequest.Persona` (`internal/transport/http/work_v2.go:3484-3491`); at most 32 children (`internal/domain/work/v2.go:770`), Feature or Issue only (`internal/app/epic_children.go:23`) |

The persona reaches the system prompt through a squad launch (`internal/app/orchestrator/squad_launch.go:65-84`
→ `internal/adapters/squadfiles/launch.go:94-220`), passed to Claude as `--append-system-prompt-file`
(`internal/adapters/projects/launch.go:276-290`).

**What does not exist is a pipeline.** Each dispatch stands alone. A task graph only refuses a node
whose dependencies are unfinished, and something still has to dispatch each node
(`internal/app/orchestrator/graphs.go:14-46,335-384`). A child cannot dispatch
(`clawdline guide child`). Long-running orchestration therefore belongs to a **Root**, and the Root
that fits is an **Epic owner**. That is exactly what the `zero-review-lead` persona was written for:
it "owns a review Epic, assembles role reviewers, and turns their evidence into a target design"
(`internal/domain/persona/catalog/zero-review-lead.md:6`), and it gives every reviewer child the same
fact pack (`docs/personas.md:120-128`). Here the snapshot is that fact pack.

### Options for the trigger

| Option | What the person does | Gain | Cost | Reversible? |
|---|---|---|---|---|
| **A. Do nothing new** | Creates an Epic by hand, pastes a brief, assigns a new Session as `zero-review-lead` | Works today, zero code | Several clicks plus a pasted brief; the agent measures with shell commands, which costs tokens and differs every run; no stored comparison | yes |
| **B. One action = snapshot + Epic + assignment** | Presses **Run governance review** on the Project | One action; measurement costs no tokens and is identical between runs; the brief is fixed and versioned | A daemon endpoint, a console button, a stored snapshot format | the button and endpoint yes; the snapshot format **no** once two runs compare against it |
| C. A general workflow engine | Same as B | Could serve other pipelines | A new orchestration layer with no other present need (role rule 6); duplicates the Epic flow | costly to remove |

**Decision: A for phase 0, B from phase 2. C is rejected.** A costs nothing and gives the baseline that
tells us whether B is worth building. B reuses the Epic flow, Root assignment, Notes and gates as they
are. The only new pieces it adds are the measurement and one call that chains three existing ones.

What B's single call does, in order:

1. Takes a snapshot (S1). If that fails, it stops and shows the error. It does not create an empty
   Epic.
2. Creates an Epic in the Project, titled `Governance review <date>`. Its description is the fixed
   brief, plus the snapshot path and the previous run's id.
3. Assigns it to a new Session with persona `zero-review-lead`, through the same code path as the
   Board's "assign to new Session" (`internal/transport/http/work_v2.go:2229`).

The person is the one who presses the button, so the Epic is theirs. No Session files a card on its
own initiative.

## 4. Measurement layer (S1)

Deterministic, inside the daemon, with no external tool assumed. Each metric says how it is computed
and what is shown when it cannot be measured. **"Not measured" is never drawn as 0**
(`docs/unknown-as-certain.md`). Every unmeasured row carries its reason, for example `no detector`,
`not a git repository`, `history shorter than window`, or `parse error in 3 files`.

The input is the set of files tracked at `HEAD` (`git ls-files`), never the working tree. The same
commit therefore gives the same snapshot, and other sessions' uncommitted edits do not leak into it.

**Exclusions, in order:**

1. Files with the Go generated-code header `^// Code generated .* DO NOT EDIT\.$`. This is the Go
   convention; both of our generated files carry it (`internal/contract/zz_generated.go:1`,
   `web/contract/src/generated.ts:1`).
2. `.gitattributes` `linguist-generated` / `linguist-vendored` attributes, if any. This repository
   has no `.gitattributes`.
3. The per-project config (§8).

Excluded files are still counted, on their own "excluded" line, so the total stays visible.

| # | Metric | How | When it cannot be measured |
|---|---|---|---|
| M1 | **Size per module** | Module = directory at a configurable depth (default: Go package directory; other languages: second path level). Go: code, comment and blank lines via `go/scanner` (stdlib). Other text files: lines and non-blank lines only, labelled "no comment split". Tests are a separate column (`_test.go`, `*.test.ts(x)`, `test/` dirs). | Binary or undecodable files are listed by count only. |
| M2 | **Go complexity** | Cyclomatic complexity per function via `go/ast` (stdlib): 1 + `if`, `for`, `case`, `&&`, `\|\|`. Per file: max and sum. | Languages other than Go: "not measured — no parser". Files that fail to parse are counted and named. |
| M3 | **Churn and hotspots** | `git log --numstat --since=<window>` (default 90 days). Hotspot = rank(churn) × rank(complexity) per file, the method of Tornhill and code-maat. Language-neutral: where M2 is unavailable, size stands in for complexity, labelled as such. | Shallow clone or history shorter than the window: shown as "history covers N days". |
| M4 | **Duplication and dead code** | Only when a detector is on `PATH` (`jscpd`, `deadcode`, `knip`, `staticcheck`), and the detector's version is recorded. One deterministic stand-in is always available: **parallel-version leads**, i.e. file or directory pairs named `x` / `x_v2` / `x-v2`. These are shown as leads, never as findings. | "not measured — no detector installed". None is installed on this machine (2026-10-06). |
| M5 | **Document inventory** | Every tracked `*.md`, `CLAUDE.md`, `AGENTS.md` and skill file: bytes, estimated tokens (bytes ÷ 4, the ledger's own estimate, `ledger.go:1015`), last change (`git log -1`), inbound references (other tracked files naming its basename or path), and the doc marker (§7). | No marker: "unclassified", not a guessed kind. |
| M6 | **Instruction and protocol cost** | From the existing token ledger over the window: share of `rules` and `protocol` (`docs/token-ledger.md:66-79`), and the measured size of each instruction file in the resident base (`Composition.Instructions`, `ledger.go:163-180`). | Per-document *carry* cost: **"not measured"**. The ledger classifies a later read of `docs/limits.md` as `impl` and does not keep the path (`ledger.go:1157-1175`). `rules` is an upper bound and is shown as one (`docs/token-ledger.md:83-85`). Codex sessions have no price, so their cost is unknown. |
| M7 | **Governance inventory** | Guard scripts and guard tests found by name pattern; rule sections in `AGENTS.md`/`CLAUDE.md` by heading; for each, the last time it went red (§9). | "Last caught: never recorded" until P4 adds the record. It is never shown as "never caught". |

### Size per module: embed scc, or count ourselves

scc is MIT-licensed (checked 2026-10-06 at github.com/boyter/scc, `LICENSE`, SPDX `MIT`) and is a Go
library. But its current `go.mod` (module `github.com/boyter/scc/v4`) requires **Go 1.26.4** and pulls
in go-git, mcp-go, cobra, zerolog and others. This repository is on `go 1.25.0` (`go.mod:3`) with four
direct dependencies (`go.mod:5-10`).

| Option | Gain | Cost | Reversible? |
|---|---|---|---|
| **Own counter** (M1 as above) | No new dependency; exact for Go; honest "no comment split" elsewhere | No comment/code split for TS, Swift or Python | yes |
| Embed scc | ~200 languages with comment detection | Toolchain bump, a large transitive tree in a daemon that ships to three platforms, licence bundle in `docs/licenses/` | yes, but it costs a build-system change both ways |
| Run `scc` if found on `PATH` | Better numbers for users who have it | Two code paths; the numbers change with the user's install | yes |

**Decision: own counter in P1; use scc on `PATH` as an optional upgrade in P3, with its version
recorded.** Embedding is reconsidered only if a non-Go customer asks for comment-split numbers.

### What the snapshot holds

`snapshot.json`: `{"governance_snapshot": 1, "project": <origin host/owner/name>, "commit", "taken_at",
"window_days", "metrics": {M1…M7, each {"status": "measured"|"not_measured", "reason"?, "rows"}}}`.
The rows are numbers and paths, never file contents. The version field is the one irreversible
contract in P1: a later run must be able to read every earlier version, or comparison breaks (§12, R3).

## 5. Roles

No new persona. Each existing one does what it was written for.

| Stage | Persona | Why this one |
|---|---|---|
| Conductor (Epic owner, S2–S3, S6) | `zero-review-lead` | Owns a review Epic and gives reviewers one fact pack (`docs/personas.md:120-128`) |
| Architecture and redundancy review (S2) | `architect` | Plans against code that has been read; must give options and their costs |
| Document review and slimming (S2, S5) | `technical-writer` | Reader first, problem before mechanism |
| Cost review (S2, S6) | `finops` | Makes spend traceable before cutting |
| Claims check before G1 | `reality-checker` | "Not yet proven" until the evidence says so; it is also the verification checker that the gate already picks for Epics (`internal/adapters/store/work_v2_gates.go:859-880`) |
| Implementation (S4) | the conductor's choice per item, e.g. `minimal-change`, `code-reviewer` for Issues | Existing per-kind behaviour is unchanged |

The reviewer children are read-only (`--claims ""`) and receive the snapshot path. They do not get
the repository's large documents: the brief tells them to open a document only to check a specific
claim.

## 6. Where snapshots and reports live

**Outside the repository, in the daemon's state directory.** The repository is public, and the
review data belongs to the user.

- State dir: `CLAWDLINE_NEXT_DIR`, otherwise the platform config dir + `clawdline-next`
  (`internal/config/config.go:103-121`).
- There is no per-project state directory today. The only per-repository layout is
  `worktrees/<RepoSlug>/<task>`, and reclaim clears it, so it is scratch space, not a home
  (`internal/app/orchestrator/broker.go:908-918`, `reclaim.go:964-974`).
- New: `<state>/governance/<RepoSlug(origin)>/<run-id>/{snapshot.json, report.md}`. These are plain
  files, so P1 needs no SQLite schema change, and deleting the directory removes the feature's data.
  The run id is `<UTC date>-<short commit>`.
- **Cloud carries ciphertext only.** The relay sees an envelope; the payload is AES-256-GCM and the
  content key never reaches Cloud (`docs/cloud-wire.md:38,58-67,252`). There is no Cloud store for
  arbitrary blobs: the channels are fixed (`docs/cloud-wire.md:104-114`). So the phone reads a report
  the way it reads a project file today, through an encrypted request to the Mac that answers it
  (`docs/project-files.md:9,30`). That needs a new cloudops word, `governance.report` (P3). Cloud
  stores nothing. A report is unreadable from the phone while the machine is offline, and that is
  shown as "machine offline", not as "no reports".

## 7. Document policy

**The finding to act on:** cost comes from an agent reading a large file that mixes the current
specification with history. Agents do not load `docs/` by default; they open what a table points them
to (`AGENTS.md`, "The single pages"). So the fix is to make each specification small and move history
out of it, not to add rules about reading.

**A marker, reusing the discipline the repository already has.** `TestNothingSaysTheRetiredAppIsStillRunning`
already accepts a `retired-app-record` marker with a date in a file's first 60 lines
(`internal/config/retiredapp_test.go:31-42`). Ten docs carry that marker. Another 42 docs start
with a dated Retired Swift-generation record banner (39 directly in `docs/`, three in `docs/adr/`);
these carry `<!-- clawdline-doc: kind=record audience=both -->`. The proposal generalises the
marker instead of adding YAML front matter:

```
<!-- clawdline-doc: kind=spec audience=agent -->
```

- `kind`: `spec` (how it works now; kept true) | `record` (what was measured or decided, dated; never
  updated) | `retired` (about something that no longer runs) | `user` (help for people, e.g. `docs/user/`).
- `audience`: `human` | `agent` | `both`.
- One HTML comment, invisible when rendered, inside the first 60 lines like the existing marker. A
  file with `retired-app-record` counts as `kind=retired`, so existing files need no edit.
- An explicit `clawdline-doc` marker is required only for root-level Markdown documents and
  `docs/*.md`. `docs/user/` counts as `kind=user` and `docs/adr/` as `kind=record` by directory;
  their explicit markers, when present, agree with that classification.

**Records move to `docs/records/`**, and no table in `AGENTS.md` points there. A spec links to the
records it was decided from, so history is one click away and never inlined.

**Checked by the review, not by a commit gate.** M5 reports unmarked files, specs over a size
threshold (default 40 KB), specs not changed in 90 days that were named in recent commits, and docs
nobody references. All of these are findings for G1, never failures. The person turns a finding into
a guard only by approving it as a governance recommendation (§9).

Not decided by this design: which of the 18 unreferenced docs are deleted, moved or kept. That is the
first run's S5 output, and each move is an item the person approves.

## 8. Per-project configuration

There is no in-repository Clawdline config today. The daemon reads only `.devstack.json`
(`internal/adapters/devstack/devstack.go:33`) and the instruction and skill files
(`docs/project-files.md:5`). A committed `.clawdline/snippets.json` was considered and rejected on
2026-09-05, because saving from a phone would write uncommitted files into working trees that sessions
share (`docs/snippets.md:153-159`).

| Option | Gain | Cost | Reversible? |
|---|---|---|---|
| **No config: defaults only** (P1–P2) | Nothing to design; generated code is found by header | Cannot exclude byte-copied code such as `web/console/src/legacy/` (~35k lines, `tools/check-legacy-css.sh`), which then inflates M1 and M4 | yes |
| In-repo `.clawdline/governance.json`, read-only for the daemon | Travels with the repository to every user and machine; reviewed like code; the console never writes it, so the snippets objection does not apply | A public file name and schema: irreversible once other repositories use it | **no** |
| Daemon state per project | Private; no repository change | Per machine; a second machine or user starts from defaults | yes |

Defaults when absent: modules by package directory; docs = every tracked `*.md`; exclusions = the
generated header, `.gitattributes`, and `node_modules/`, `vendor/`, `dist/`, `build/`.

**Recommendation:** ship P1–P2 with defaults only. Decide the file at P3, after the first run shows
which exclusions were actually needed. This is open decision D2.

## 9. Visualisation and meta-review

### The one-page report (`report.md`, written in S6)

- One line per area (size, hotspots, redundancy leads, documents, rule cost, governance) with
  **red / amber / green**, the number behind it, and the change since the previous run. An area that
  could not be measured shows a grey "not measured" with its reason. It is not counted as green.
- **At most three decisions**, the same three that went to G1, with what was answered.
- The run's own cost: tokens and price from `clawdline usage --item <epic id>` (`cmd/clawdline/usage.go:24-77`).
- Thresholds start at fixed defaults (e.g. red: a file over 2,500 lines, or a spec over 40 KB) and
  are listed on the page. They are not hidden.

### Console page (P3)

`web/console/src/pages/governance.tsx`, picked up automatically like every page, by
`import.meta.glob` (`web/console/src/App.tsx:74-82`), at `#page=governance` (`web/console/src/page-route.ts:13-16`).
It shows the list of runs per Project, the report, a treemap of M1 by module coloured by M3 hotspot
rank, and the document table from M5, sortable by tokens. The "Run governance review" button lives
on the Project's card and on this page.

### Meta-review: does the system still earn its place

Each run's S2 includes one governance review. **Every rule, guard and memory entry must answer: when
did it last catch something?**

- **Guards.** Today nothing records a red run (`tools/check.sh:98-99`). The smallest product-wide
  addition is in `clawdline heavy`, which every check here already goes through (`AGENTS.md`). It
  appends `{project, command, exit, at}` for each run it wraps to `<state>/governance/<slug>/checks.jsonl`
  (P4). "Last went red" per command comes from that file. A Go guard test inside `go test ./...` is
  visible only at the command level. Per-test is "not measured" unless the run used `go test -json`.
- **Rules** (sections of `AGENTS.md`/`CLAUDE.md`). No machine signal exists. The reviewer checks
  whether the incident a rule cites (`docs/working-rules.md`) recurred or was referenced in commits
  within the window, and says which evidence it used.
- **Memory entries** live outside the repository and belong to the user. They are reviewed only when
  the person opts in for that run. The run reports counts and age, and it never copies their content
  into a report.
- **The process reviews itself.** The report compares the run's own cost with the previous run's.
  The conductor must recommend dropping or shrinking a stage whose output the person rejected twice
  in a row.

Every "drop this guard or rule" recommendation is a G1 decision, like any other.

## 10. Reused as is, and deliberately different

**Reused as is:** the Epic flow and its plan gate; Root assignment with persona; Notes with suggested
replies, and runs as the proof of approval; the Board verification gate and its checker personas;
the persona catalog; the token ledger and `clawdline usage`; `RepoSlug`; the state directory; cloudops
for phone reads; the `import.meta.glob` page registration; `clawdline heavy`.

**Deliberately different:**

- **Notes, not Decisions, for G1.** A Decision applies its default after a due time; an approval must
  never happen by itself.
- **Marker, not YAML front matter.** It follows the existing `retired-app-record` convention and
  leaves rendered docs unchanged.
- **Not "health".** The word is already taken (see the top of this page).
- **On demand, not scheduled.** QuantEcon's pattern of a scheduled read-only report first
  (QuantEcon meta#331) is kept in its *order*, read-only before acting, but not in its trigger.
- **No external analysis tools assumed.** Tornhill's hotspot method is used, but computed from `git`
  and `go/ast`. CodeScene and code-maat are not needed.

## 11. Phased plan

Each phase leaves the system working and has a check that can fail.

**P0: baseline of this repository, with existing machinery only (trigger option A).** The person
creates an Epic "Governance review 2026-10" and assigns it to a new Session as `zero-review-lead`,
with a brief that lists M1–M7 as shell commands (`git ls-files`, `wc`, `git log --numstat`). Output
goes to `<state>/governance/<slug>/<run-id>/` by hand.
- Accept when: `report.md` exists, and every M1–M7 row is either a number or "not measured" with a
  reason. Go production lines are within ±2% of 194k. G1 got ≤3 Notes and each was answered by a
  tapped or typed reply. The run's token cost is recorded from `clawdline usage --item`. That cost is
  the **budget** P2 must beat.

**P1: deterministic snapshot in the daemon.** `clawdline governance snapshot [--project <dir>]` and
`GET /v1/projects/<id>/governance/snapshot`, computing M1–M5, plus M6 from the ledger. Files in the
state dir. No console change.
- Accept when: on this repository, M1's Go production total matches `git ls-files '*.go' | grep -v
  _test | xargs wc -l` minus excluded files exactly. Two snapshots of the same commit are
  byte-identical apart from `taken_at`. A run takes < 60 s through `clawdline heavy` on the 2-core
  machine. A directory that is not a git repository returns every metric as "not measured — not a git
  repository", not zeros (a test drives that red first).

**P2: one action runs the pipeline (trigger option B), with comparison.** A **Run governance review**
button and route chain snapshot → Epic → assignment. The brief embeds the snapshot path and the
previous run's id.
- Accept when: one click creates exactly one Epic, assigned to a `zero-review-lead` Session whose
  `ASSIGNMENT.md` names the snapshot path. A snapshot failure creates no Epic. The report has a
  "since last run" column. The run's total cost (`clawdline usage --item`) is **below P0's**.
  Ordinary sessions' `rules` share in the week after is not higher than the week before (ledger
  comparison, `docs/token-ledger.md:440`).

**P3: console page and phone.** `#page=governance`, the treemap and document table, the
`governance.report` cloudops word, scc-on-`PATH` upgrade, and the config file if D2 says so.
- Accept when: the page renders a run and an unmeasured metric shows grey with its reason (a test
  asserts that "not measured" never renders as `0`). The phone opens the report through Cloud, and
  with the machine offline it shows "machine offline".

**P4: meta-review data.** `checks.jsonl` written by `clawdline heavy`; the ledger keeps per-path
read sizes, so M6 can show per-document carry cost (size × calls remaining, the ledger's own model,
`docs/token-ledger.md:11-13`).
- Accept when: a forced red check appears as "last red at …" in the next snapshot. `docs/limits.md`
  shows a carry-cost number on a fixture transcript that reads it, and the ledger's
  categories-sum-to-total test still passes (`docs/token-ledger.md:64`).

**P5 (later, optional): scheduled runs.** Today a schedule cannot carry a persona: the template accepts
only the fields in `allowedTaskFields` (`internal/domain/schedule/schedule.go:215-220`), and anything
else is refused as `unknown task field` (`:423-425`). The smallest addition is to allow `persona` there.
`Broker.DispatchScheduled` turns the template into `task.json` (`internal/app/orchestrator/scheduled.go:57-83`),
and the brief already admits a persona. But a schedule starts a **child**, and a child cannot dispatch
or own an Epic. So a scheduled run could only do S1 plus a read-only report. G1 and everything after
it still need the person to start the Epic. Not verified in depth, by instruction.

## 12. Risks and early signals

| # | Risk | Early signal | Fallback |
|---|---|---|---|
| R1 | The run itself becomes the token sink it was meant to find | P0 cost is high; reviewer children open large docs | Children get the snapshot only; reading a document needs a cited claim; cut the stage that cost most |
| R2 | Recommendations are generic ("split large files") | The person rejects ≥2 of 3 decisions | `reality-checker` pass before G1; each recommendation must cite snapshot rows and name its cost |
| R3 | Snapshot format churn breaks comparison | A P2 run cannot read P1's snapshot | Version field from day one; readers for every past version are kept |
| R4 | Numbers differ from what the person measures by hand | P0/P1 acceptance diff > tolerance | Show the exact command equivalent beside each number |
| R5 | Doc moves break links | `tools/check-doc-links.sh` red on the S5 item | Moves land only as Board items through `tools/check.sh` |
| R6 | Mistaken for a quality gate and run on every change | Run frequency > weekly | The button says when the last run was; nothing calls it automatically |

## 13. Open decisions (the person's)

- **D1: conductor persona.** `zero-review-lead` (recommended: written for exactly this) or `architect`
  (the console's default for Epics, `docs/personas.md:127-130`). Cost of `architect`: it plans one
  goal rather than assembling reviewers, so S2 would be one lens.
- **D2: per-project config location** (§8). Defaults only (recommended until P3), in-repo
  `.clawdline/governance.json` (portable but a permanent public name), or daemon state (private, per
  machine).
- **D3: are memory entries in scope?** Opt-in per run (recommended), always, or never. Memory belongs
  to the user and lives outside any repository. Reviewing it is useful on this machine but is not a
  repository property.
- **D4: does P0 run now?** It needs only the person's two clicks and roughly one Epic's worth of
  tokens, and it is the only way to set P2's budget honestly.
