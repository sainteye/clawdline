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
one Markdown file under `internal/domain/persona/catalog/`, with a frontmatter of exactly seven
keys (`id`, `name_en`, `name_zh`, `summary_en`, `summary_zh`, `suggested_kinds`, `source`) and a
body. The files are parsed when the package loads. `TestTheCatalogLoads` turns a malformed file, a
file missing from `persona.Order`, or a text past the bounds into a failing test, so a daemon
never finds one at run time.

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

Three launch paths take a persona. Every one of them goes through `Admit`, so all three refuse a
name the catalog does not have:

| Path | How it names one |
| --- | --- |
| Start or resume from a place | `POST /v1/places/{place}/start/{assistant}[/{model}]/as/{persona}` and `POST /v1/places/{place}/resume/{assistant}/{conversation}/as/{persona}` |
| A dispatched child | `persona` in `task.json`, or `clawdline dispatch --persona <id>` |
| A Root Assignment | `persona` in `POST /v1/orchestrator/root-assignments` |

A handoff's receiver and a schedule's run carry no persona in this version.

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

## Reading it back

Clawdline does not keep a persona in a side table that could drift from the running processes. It
reads the persona off the process's command line instead. `persona.FromCommandLine` recognises the
following in the command line that `ps` reports:

- **Claude:** `--append-system-prompt-file …/personas/<id>.md`, with or without `=`.
- **Codex:** the `clawdline-persona:<id>` marker.

It accepts only ids in the catalog, so a file with the same name elsewhere reads as none. The
process scanner sets `Session.Persona`. When the terminal row and the process row are merged, the
merge keeps it whichever side it arrived on (`richer`). `SessionRow.persona` reports it.

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
