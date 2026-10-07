# Project memory: one store a Project's Claude and Codex sessions both read and write

Status: **built, 2026-10-07.** Decided as P1 (a) in `docs/shared-project-instructions.md` §5. A
lesson about a Project is recorded once, through `clawdline memory`, and every later Session on that
Project is given its index at launch, whichever assistant it runs.

## Store

- **Where.** `<state dir>/memory/<repo key>/`. The repo key is `orchestrator.RepoSlug` of the
  Project's canonical key (`memory.RepoKey`, held equal by a test): a linked worktree resolves to
  its main repository, so every child of a Project shares its memory. The key's shape
  (`^[a-z0-9-]{1,32}-[0-9a-f]{8}$`) is checked before it becomes a path.
- **Entries.** One Markdown file per entry, `<name>.md`, mode 0600:

  ```markdown
  ---
  name: kebab-case-name
  description: one line, used to decide relevance
  metadata:
    type: user | feedback | project | reference
  ---

  The body.
  ```

  A name is lowercase letters, digits and single hyphens, at most 64 bytes (it is also the file
  name). The description is one line of at most 512 bytes; the body at most 64 KiB.
- **Index.** `INDEX.md` is generated from the entries after every write and never edited by hand:
  a heading, then one line per entry, `- name — description`, grouped by type in the order above
  and sorted by name. It is bounded at 8 KiB; when it is cut, it is cut at a whole line and ends
  with a line saying how many entries it left out and that `clawdline memory list` shows them all.
- **Writes.** Each file is written to a temporary file, synced and renamed over the old one, under
  one lock per daemon, so concurrent adds from several Sessions neither interleave nor lose an
  entry. The same add twice answers `unchanged`; a different entry under a name already taken is
  refused, not overwritten.
- **Bounds.** 512 entries per Project (one more is refused with `memory_full`; only a person or a
  Session they asked forgets one), 64 KiB per body, 512 bytes per description, 64 bytes per name.
  All are registered in `internal/domain/capacity` (`memory.*`).
- **Unknown is not empty.** A store directory that does not exist is a Project with no memory; one
  that exists and cannot be read is `memory_unreadable` (500), never an empty list.

## CLI and routes

```sh
clawdline memory list   [--project <dir>] [--json]
clawdline memory show   <name> [--project <dir>] [--json]
clawdline memory add    --name <n> --description <d> --type <t> [--body-file <f>] [--project <dir>]
clawdline memory update --name <n> --description <d> --type <t> [--body-file <f>] [--project <dir>]
clawdline memory forget <name> [--project <dir>]
clawdline memory import --from-claude [--apply] [--project <dir>]
```

`--project` defaults to the current directory's git top-level. Without `--body-file` the body is
read from standard input, which must not be a terminal. Name, type and description are checked
before the daemon is asked. Exit 0 is done, 1 refused, 2 a usage mistake, 3 unknown (the daemon did
not answer).

The command writes through the daemon, not the state directory, because a Codex Session's sandbox
cannot write outside its workspace while it can reach the loopback daemon. The routes take the
orchestrator token like the other machine-scoped Project routes:

| Route | Answer | Refusals |
|---|---|---|
| `GET /v1/projects/{place}/memory` | `ProjectMemoryList`: key, entries, index, whether it was cut | `memory_unreadable` |
| `POST /v1/projects/{place}/memory` | `ProjectMemoryWriteAnswer`, 201 created / 200 unchanged | `memory_entry_invalid` 400, `memory_entry_exists` 409, `memory_full` 409 |
| `GET /v1/projects/{place}/memory/{name}` | `ProjectMemoryEntry` | `memory_entry_not_found` 404 |
| `PUT /v1/projects/{place}/memory/{name}` | updated / unchanged | `memory_entry_not_found` 404, `memory_entry_invalid` 400 |
| `DELETE /v1/projects/{place}/memory/{name}` | forgotten | `memory_entry_not_found` 404 |

The types are in `api/v1/projects.schema.json`. A store that cannot be written answers
`file_permission` 403.

## Launch delivery

Every launch Clawdline makes — a Session started from the console, a dispatched child, a handoff —
reads the Project's index once and gives both assistants the same text: one sentence telling the
Session where the index came from, how to read an entry and to record Project lessons with
`clawdline memory add` rather than its own assistant's memory, then the index.

- **Codex**: appended to the one `-c developer_instructions` value, after the role sentence and
  before the Note instruction. Codex is given the text, not told to read a file.
- **Claude Code**: one inline `--append-system-prompt` argument after its other arguments, beside
  the role's `--append-system-prompt-file`. Measured with Claude Code 2.1.292 on 2026-10-07: of two
  `--append-system-prompt-file` flags only the last is applied, in either order; an inline
  `--append-system-prompt` beside a file flag keeps both. One inline argument therefore needs no
  composed file and leaves the role file untouched.
- **Typing it.** A launch line that holds a newline is always written to a private script and
  sourced, whatever its length, so a multi-line index reaches the assistant as one argument under
  sh and zsh.
- **No memory, no change.** A Project with no entries launches with byte-identical arguments; a
  store that cannot be read is logged and the launch goes on without memory. A launch carrying more
  than 9 KiB of memory text is refused, which the 8 KiB index bound keeps from happening.

Measured end to end on 2026-10-07 with a daemon of its own on an empty state directory and the
exact shell command `projects.Admit` builds, invented facts in the descriptions:

- Codex (codex-cli 0.160.1, `exec -s workspace-write`) recorded an entry with `clawdline memory
  add`; Claude Code 2.1.292 launched with that memory and Read, Bash, Glob and Grep disallowed
  named the fact; without the memory argument it answered that it did not know.
- Claude Code recorded an entry; Codex launched with that memory and told to run no commands named
  the fact in 3 of 3 runs, running none; without it, it did not know.
- A fact only in an entry's body was not known to a session that could not run `memory show`: the
  index is names and descriptions, so a description should say enough to tell when to read on.
- codex-cli 0.160.1 ignores `-c` overrides placed before its `exec` subcommand; they are delivered
  when they follow it, and to the interactive Codex that Clawdline launches.

## Import from Claude Code

`clawdline memory import --from-claude` reads Claude Code's auto-memory for the Project,
`~/.claude/projects/<slug>/memory/`, where the slug is Claude's folder name for the Project's
directory (and for its main repository, when they differ). It prints a plan: the entries it would
import, the names already in the store that it will skip, and the files that are not entries with
the reason (an index such as `MEMORY.md`, no frontmatter name, a name or type this store refuses).
Only `--apply` copies, and it never overwrites a name already in the store. The source is opened
read-only; nothing is written under `~/.claude/`.

## Privacy

Memory entries are the person's. They live in the state directory, outside every repository, and
nothing in this repository holds one: the tests use invented entries, and no route or log line
prints an entry's body. A public Project's lessons that every Session needs on every task belong in
`AGENTS.md` instead (P3); the store is for the rest.

## Not built

A console view of memory, Cloud wording for it, memory entries in `clawdline project unify
--check`, and any change to Claude Code's own auto-memory.

<!-- clawdline-doc: kind=spec audience=agent -->
