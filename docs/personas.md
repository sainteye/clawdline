# Session personas

A persona is a role definition a session is launched with: a few kilobytes of text in the
session's system prompt that make it work the way an architect, a reviewer or a security engineer
works. It is chosen when the session opens and stays for the whole conversation. Nothing is given a
persona that nobody chose: every route, flag and field below defaults to none, and none is the
session Clawdline always opened.

The word is `persona` in code, on the wire and in every flag. `role` already names the machine's
coordinator role and the dispatch role contract (docs/design-guidelines.md DG-5). The console may
call it "Role".

## The catalog

The catalog is closed and compiled into the daemon (`internal/domain/persona`). Each persona is
one Markdown file under `internal/domain/persona/catalog/`, with a frontmatter of exactly eight
keys (`id`, `team`, `name_en`, `name_zh`, `summary_en`, `summary_zh`, `suggested_kinds`, `source`) and a
body. The files are parsed when the package loads. `TestTheCatalogLoads` turns a malformed file, a
file missing from `persona.Order`, or a text past the bounds into a failing test, so a daemon
never finds one at run time.

Every persona belongs to one **team**, `engineering` or `marketing` (`team` in the frontmatter and
on the wire, a closed set `parse` refuses anything outside of). The console names the teams; the
catalog only says which one.

The engineering team:

| id | Name | Suggested for |
| --- | --- | --- |
| `architect` | Architect | epic |
| `backend` | Backend Engineer | feature |
| `frontend` | Frontend Engineer | feature |
| `minimal-change` | Minimal-Change Engineer | issue |
| `code-reviewer` | Code Reviewer | — |
| `reality-checker` | Reality Checker | — |
| `security` | Security Engineer | — |
| `technical-writer` | Technical Writer | — |

The marketing team, for a session working in a blog, a site or a docs repository. Each was
adapted, not copied: upstream emoji, hype, invented metrics and tool-specific pipelines were
dropped, and every one edits files in the repository, never posts or sends anything itself, and
writes in the site's own language and locale (Taiwan usage for zh-TW). None is suggested for a
Board kind.

| id | Name | 中文 | Adapted from |
| --- | --- | --- | --- |
| `seo` | SEO Specialist | SEO 專家 | [`marketing-seo-specialist.md`](https://github.com/msitarzewski/agency-agents/blob/053ddbbf392a1688fc7043d81529f47ef2cf86c8/marketing/marketing-seo-specialist.md) |
| `content-writer` | Content Writer | 文章寫手 | [`marketing-content-creator.md`](https://github.com/msitarzewski/agency-agents/blob/053ddbbf392a1688fc7043d81529f47ef2cf86c8/marketing/marketing-content-creator.md), with the long-form voice of `marketing-book-co-author.md` |
| `ai-search` | AI Search Optimizer | AI 搜尋優化師 | [`marketing-ai-citation-strategist.md`](https://github.com/msitarzewski/agency-agents/blob/053ddbbf392a1688fc7043d81529f47ef2cf86c8/marketing/marketing-ai-citation-strategist.md) and `marketing-aeo-foundations.md` |
| `social-media` | Social Media Strategist | 社群策略師 | [`marketing-social-media-strategist.md`](https://github.com/msitarzewski/agency-agents/blob/053ddbbf392a1688fc7043d81529f47ef2cf86c8/marketing/marketing-social-media-strategist.md) |
| `instagram` | Instagram Curator | Instagram 經營 | [`marketing-instagram-curator.md`](https://github.com/msitarzewski/agency-agents/blob/053ddbbf392a1688fc7043d81529f47ef2cf86c8/marketing/marketing-instagram-curator.md) |
| `email` | Email Strategist | 電子報策略師 | [`marketing-email-strategist.md`](https://github.com/msitarzewski/agency-agents/blob/053ddbbf392a1688fc7043d81529f47ef2cf86c8/marketing/marketing-email-strategist.md) |
| `growth` | Growth Hacker | 成長駭客 | [`marketing-growth-hacker.md`](https://github.com/msitarzewski/agency-agents/blob/053ddbbf392a1688fc7043d81529f47ef2cf86c8/marketing/marketing-growth-hacker.md) |
| `pr` | PR & Communications | 公關傳播 | [`marketing-pr-communications-manager.md`](https://github.com/msitarzewski/agency-agents/blob/053ddbbf392a1688fc7043d81529f47ef2cf86c8/marketing/marketing-pr-communications-manager.md) |

`suggested_kinds` tells a picker which personas to show first for a Board item's kind. It only
orders the choices: no kind gives a persona to anything.

Every persona has a pixel bot. It is eight columns by seven rows, in the same `Icon` shape a
project's mark uses (`common.schema.json`), and it is defined in `icons.go`.

### What a session is given

The text injected is `persona.Persona.Text()`, built from three parts:

1. **A fixed preamble.** It says the persona shapes how the session thinks and never overrides
   `CLAUDE.md`, `AGENTS.md` or any other project instruction file, the task brief, `CHILD.md`, or
   the Clawdline protocol. When any of them disagrees, they win.
2. **The body** of the catalog file.
3. **A source line** naming the upstream file and its licence.

The daemon writes each text to `<state dir>/personas/<id>.md` when it starts
(`app.WritePersonaFiles`), with the upstream licence beside them as `LICENSE.agency-agents`. The
directory is `0700` and each file is `0600`. A file that already holds the same bytes is left
alone. One that differs (an upgraded daemon's new text, or a hand edit) is replaced through a
temporary file and a rename. A session that is already running keeps the text it was launched
with.

## How each assistant is given it

`projects.Admit` takes `LaunchRequest.Persona` and `PersonaDir` and appends the persona's
arguments last, pre-quoted with `ShellQuoted` like every other launch argument. That makes the path
exactly one shell word whatever it holds, whether a space, `'` or `"`. `launch_persona_test.go`
splits each line the way a POSIX shell does and checks this.

- **Claude Code:** `--append-system-prompt-file <state dir>/personas/<id>.md`. Claude Code reads the
  file and appends it to its own system prompt.
- **Codex:** `-c developer_instructions=<TOML basic string>`. The string is
  `persona.CodexInstruction`: one ASCII sentence that opens with `clawdline-persona:<id> - `, names
  the role, and tells Codex to read the persona file completely before its first answer. The value
  is escaped as a TOML basic string (`projects.TOMLString`), and the test decodes it back to the
  exact sentence.

  **Caveat:** a `-c developer_instructions=…` override *replaces* any `developer_instructions`
  the person set in `~/.codex/config.toml` for that session. It does not add to them. A Codex
  session launched with no persona is unaffected.

  A Codex session given any `-c` override also runs without Codex's shared background server:
  codex-cli 0.157.1 shows the startup warning "command-line configuration overrides (-c, …)
  requires embedded mode". Children and Root Assignments already start Codex that way (their
  directory-trust answer is a `-c` too), so a persona adds nothing new for them; a Codex session
  started from a place with a persona is the one that newly runs embedded. It binds its
  conversation and reports its persona as any other row does (measured 2026-09-27: a disposable
  daemon started `claude` as `code-reviewer` and `codex` as `architect` in tmux; `ps` showed each
  value as one argument, both rows carried `persona`, the Codex row bound its conversation, and
  asked for their role both answered from their persona file).

Three launch paths take a persona. Every one of them goes through `Admit`, so all three refuse a
name the catalog does not have. A Board assignment to a new Session reaches the third:

| Path | How it names one |
| --- | --- |
| Start or resume from a place | `POST /v1/places/{place}/start/{assistant}[/{model}]/as/{persona}` and `POST /v1/places/{place}/resume/{assistant}/{conversation}/as/{persona}` |
| A dispatched child | `persona` in `task.json`, or `clawdline dispatch --persona <id>` |
| A Root Assignment | `persona` in `POST /v1/orchestrator/root-assignments` |
| A Board item assigned to a new Session | `persona` in the assign body (below), or `clawdline item child --assign-new --persona <id>` / `clawdline item assign <id> --new --persona <id>` |

A handoff's receiver and a schedule's run carry no persona in this version. No path gives one by
default: a Board item's kind and a dispatch's kind (`plan_review` included) only suggest.

## Routes and contract shapes

`GET /v1/personas` returns `PersonaCatalog` (`api/v1/personas.schema.json`):

```json
{
  "personas": [
    {
      "id": "architect",
      "name": { "en": "Architect", "zh-Hant": "架構師" },
      "summary": { "en": "…", "zh-Hant": "…" },
      "suggested_kinds": ["epic"],
      "icon": { "accent": "#…", "cells": [["#…", null, …], …] },
      "source": "https://github.com/msitarzewski/agency-agents/blob/053ddbbf…/engineering/engineering-software-architect.md"
    }
  ],
  "license": "MIT"
}
```

The injected texts stay on the machine and never travel on this route. Any method other than GET
gets `405 method_not_allowed`.

**Start and resume.** `…/as/{persona}` is read as a path segment, like every other input on these
routes, so it is part of the idempotency digest. `as` is recognised only as the second-to-last
segment of a route that already names its assistant:

- `/start/claude/as` is still a model called `as`, and gets 404.
- `/resume/{conversation}/as/{id}` with no assistant gets 404.
- A name the catalog does not have gets `400 unknown_persona` before anything opens.
- A success answer (`PlaceStarted`, `PlaceResumed`) carries `persona`.

**Dispatch.** The `persona` string in `task.json` is optional. A non-string is refused as
`bad_task` with "persona must be a string". An unknown name is refused as `bad_task` with "persona
must be one of: architect, backend, …". `clawdline dispatch --persona` refuses an unknown name
locally, before anything is written or sent. The task record keeps it, and `TaskRow.persona` and
`BrokerTask.persona` report it.

**Root Assignment.** `RootAssignmentRequest.persona` is optional, and an unknown name is
`bad_root_assignment`. The Feature Root is launched as that persona. Its `ASSIGNMENT.md` gains a
`PERSONA` section naming the persona and its file. The persona is part of the request the receipt
compares, so the same `request_id` with another persona gets `request_conflict`. A request without
one digests exactly as it did before the field existed. `BrokerRootAssignment.persona` reports it.

**Board assignment.** Three routes assign a Board item to a Session, and each takes an optional
`persona` string beside `mode`:

- the person's `POST /v1/work/v2/items/<id>/assign`: `{"expected_version", "mode": "new_session",
  "assistant"?, "model"?, "persona"?}`;
- the Epic owner's `POST /v1/work/v2/agent/items/<epic id>/children`, in its `assign` object;
- the Epic owner's `POST /v1/work/v2/agent/items/<child id>/assign`, beside `mode`.

An empty or absent `persona` is none. With `"mode": "new_session"`, a name the catalog does not
have is `400 unknown_persona`. With `"mode": "existing_session"`, any persona is
`422 persona_not_applicable`: that Session's system prompt was fixed when it opened. Both are
refused before anything is written; on the children route that means before the child is created.
The persona goes into the `RootAssignmentRequest` that opens the Session, so its `ASSIGNMENT.md`
carries the `PERSONA` section.

The assignment records it: `work_v2_assignments.persona` (`TEXT NOT NULL DEFAULT ''`, no CHECK —
the catalog is closed in Go, so a new persona needs no table rebuild). A table from before the
column gains it empty. Each item's `assignments[]` on the Board wire carries `persona` when the
assignment opened one, and leaves it out otherwise.

`clawdline item child … --assign-new --persona <id>` and `clawdline item assign <id> --new
--persona <id>` send it. `--persona` with `--assign-terminal` or `--terminal`, or without a new
Session, is a usage error (exit 2), and so is an id this build lacks; nothing is asked of the
daemon.

## Reading it back

Clawdline does not keep a persona in a side table that could drift from the running processes. It
reads the persona off the process's command line instead. `persona.FromCommandLine` recognises the
following in the command line that `ps` reports:

- **Claude:** `--append-system-prompt-file …/personas/<id>.md`, with or without `=`.
- **Codex:** the `clawdline-persona:<id>` marker.

It accepts only ids in the catalog, so a file with the same name elsewhere reads as none. The
process scanner sets `Session.Persona`. When the terminal row and the process row are merged, the
merge keeps it whichever side it arrived on (`richer`). `SessionRow.persona` reports it.

## Console and Cloud

The console calls a persona a "Role" (角色). It reads `GET /v1/personas` once per page
(`web/console/src/personas.ts`); a console shows one machine for its whole life, so that is once
per machine. A read that fails for any reason is an empty catalog, and an empty catalog draws no
chips and no bots: an older daemon, or a machine on Cloud that cannot answer, shows the console it
showed before this feature, never a sheet that breaks after a press.

- **The team switcher.** The start sheet, its resume step and the Board's new-Session assignment
  draw one role row (`session/RoleRow.tsx`). When the catalog has personas in more than one team,
  its "Role" label is a native select, 工程團隊 / Engineering and 行銷團隊 / Marketing, styled like
  the label with a caret so a phone opens its own picker; the chips to its right are "No role"
  and that team's personas only. The team shown is the chosen persona's own team, otherwise the
  team last picked in this browser (localStorage, `clawdline.persona.team`), otherwise
  engineering, so the Board's kind default (epic → architect, issue → minimal-change) shows the
  engineering team. Switching to a team that does not hold the chosen persona resets the choice
  to "No role": a start never sends a persona the person cannot see. A catalog without `team`
  (a daemon older than the field, or a Cloud machine not yet updated) is all engineering, and
  with one team the label stays the plain "Role" it was. The decisions are pure functions in
  `personas.ts` (`shownTeam`, `switchTeam`, `teamsOffered`, `personasOfTeam`).
- **Session list.** A row whose `persona` the catalog names shows the bot alone, at two pixels a
  cell (16×14, the height of the line's 11 px text), at the head of its second line, before the
  path; the full name and summary are its title and accessible name. It first rode in the title
  line after the assistant's name, where a 390 px phone cut the name to its first character, and
  then had a line of its own under the state line with the bot at three pixels a cell and the full
  name, which the person found too large for the row. Measured 2026-09-27 with mocked rows and the
  real catalog: a row with a persona is now as tall as one without (phone 112.9 → 87.9 px; desktop
  101.6 → 77.6), and the page does not scroll sideways at 390 px.
- **Session detail header.** A row whose `persona` the catalog names shows the bot alone, at two
  pixels a cell, at the head of the line under the session's name, before the path. The name and
  summary are its title and the session button's description (`aria-describedby`), never drawn.
  It was a 32×28 bot between the name and the tools, the largest thing in a phone's header after
  the project mark, and it took 40 px from the session's name. Measured 2026-09-27 with the header's
  markup and stylesheets at 390 px and 1200 px: the line under the name is 15.9 px with and without
  a role, and the name is as wide as with no role. No persona, or one the catalog does not name,
  draws nothing (`headPersona`).
- **Start sheet.** A row of role chips under the assistant chips, "No role" first and chosen by
  default. It scrolls sideways on a phone. The choice becomes the start route's `/as/{persona}`
  and is remembered in this browser only.
- **Resuming from a place.** After the person picks a place to resume in, the same role row stands
  over its past conversations, "No role" chosen. A past conversation's list does not say which
  role it had, so nothing is preselected and the start sheet's remembered choice is not carried
  over. A role makes the resume route `…/resume/{assistant}/{conversation}/as/{persona}`; both
  sheets build their paths in `web/console/src/session/place-routes.ts`, and
  `place-routes.test.ts` checks that Cloud's `writeRoute` reads each one back as the same resume.
- **Restoring after a reboot.** Each row on the restore sheet shows the role it was launched with
  (`RestorableSession.persona`). Restoring relaunches it with that role; the restore request has
  no field to change it, so the sheet shows it rather than offering chips.
- **Board.** A new-Session assignment shows the role chips beside the assistant chips. The item's
  kind picks the default when exactly one persona suggests that kind (epic → architect, issue →
  minimal-change); feature, which two suggest, starts with none. The button names the role, and
  `persona` goes into the assign body only when one is chosen. The existing-Session picker and an
  Epic's children list show the owner's bot when its session row has a persona.

On Clawdline Cloud the same bundle reaches the machine through the encrypted relay
(`internal/app/cloudops`, `web/console/src/cloud`):

- **`personas`** is a read the machine answers as `GET /v1/personas`. It is in `Implemented()`, so
  it appears in the machine descriptor's `machine.commands`, and in `carry.ts`. The copied client
  refuses a read the descriptor does not list before anything is sealed
  (`cloud_machine_unsupported`), which is what hides the chips in front of an older daemon.
- **`start` and `resume`** take an optional `persona` and build the `/as/{persona}` route, naming
  the assistant (`claude` when none was given). The relay writer parses `as` only as the
  second-to-last segment, as `placeRoute` does. The copied client's `startPlace` and `resumePlace`
  cannot carry a persona, so a route with one is sent with `_machineRequestAs` under the
  request's Idempotency-Key; one without keeps the old path. When the descriptor says the machine
  does not offer `personas`, a start with one is refused before it is sealed, because an older
  daemon would refuse the extra key as malformed.
- **Board assignment** crosses as the route body, verbatim, so its `persona` needs nothing of the
  relay.

## Restore

`restore_sessions` has a `persona` column (`TEXT NOT NULL DEFAULT ''`). A table from before the
column gains it empty. Each boot's reading records the persona a session was running as.
Restoring it resumes the conversation with that persona. If a later build no longer has that
persona, the conversation is restored with none rather than not at all.
`RestorableSession.persona` reports it.

## Bounds

The catalog holds at most **32** personas (`persona.MaxPersonas`). Each injected text is at most
**8 KiB** (`persona.MaxPersonaBytes`), counting the preamble, the body and the source line. Both
are registered as `personas.catalog` and `personas.text_bytes`, and `/v1/diagnostics.capacity`
reports them. See docs/limits.md N52. The eight shipped texts are 4–5 KiB each.

## Attribution

The texts are adapted from [agency-agents](https://github.com/msitarzewski/agency-agents) at commit
`053ddbbf392a1688fc7043d81529f47ef2cf86c8`, which is MIT-licensed:
Copyright (c) 2025 AgentLand Contributors. Each persona's `source` names the upstream files it was
adapted from. The licence is kept word for word in
`internal/domain/persona/catalog/LICENSE.agency-agents`, and a copy is written beside the texts on
disk.
