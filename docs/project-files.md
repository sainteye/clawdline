# Project instruction and skill files

The gear on each Project row opens that Project's settings. Its first section inventories disk candidates for Codex instructions, Claude Code instructions, and Skills. It shows each filename, its path relative to the Project or the machine's home directory, its source, and whether it can be read or edited. The inventory does not claim that a running assistant has loaded a file: overrides, plugins, and the time a Session started can change the effective instructions.

The Project scope runs from the nearest repository root through the selected place. It includes `AGENTS.md`, `CLAUDE.md`, `CLAUDE.local.md`, `.claude/CLAUDE.md`, `.claude/CLAUDE.local.md`, and direct `SKILL.md` children under `.agents/skills` or `.claude/skills` at each layer. The home scope includes `.codex/AGENTS.md`, `.claude/CLAUDE.md`, `.claude/CLAUDE.local.md`, and direct skill children under `.agents/skills`, `.codex/skills`, or `.claude/skills`. Missing common instruction files remain listed as missing. Home files are read-only in this interface because changing one may affect every Project.

`GET /v1/projects/{place}/files` lists candidates; `GET /v1/projects/{place}/files/{file}` returns UTF-8 content and its SHA-256 version. `PUT` to the same file route takes `expected_version` and `content`, plus `Idempotency-Key`. The daemon resolves the place from its current Project catalog. It accepts only a file ID from the bounded inventory, never a browser-supplied path. Regular files are opened beneath bound directory handles; symlink or directory swaps are refused. A save requires the version the editor read, writes a temporary file in the same directory, syncs it, and renames it. Concurrent edits through this API are serialized; a change made elsewhere returns `file_changed` and leaves the browser's draft intact. The interface does not create, delete, or rename files.

The paired Cloud console carries three distinct words for the inventory read, content read, and save. The Cloud Project ID is resolved to its owning machine before a request is sent. Reads require the paired device's read access; save also requires the remote write gate and carries the editor's idempotency key. A lost save answer is treated as uncertain: the browser reads the file again and confirms matching content before it says the save succeeded. The Cloud relay carries encrypted request and answer content; it does not use a public document URL for these files.

The editor guards its draft when a person closes the modal, changes page, selects another file, or reloads the inventory. On a phone the groups and content stack in one column. Closing the modal returns keyboard focus to the Project gear that opened it.

## Project file tree

The same Project gear has a Files view beside Settings and instructions. It opens the selected
Project's own directory, not a repository ancestor or the machine's home directory. Folders are
loaded only when expanded; a file's content is requested only when selected. This view is read-only.
The existing instruction and Skill editor remains in Settings and instructions.

`GET /v1/projects/{place}/tree?directory=<relative>` lists one directory;
`GET /v1/projects/{place}/tree/file?path=<relative>` reads one regular UTF-8 text file. The server
resolves `{place}` from its current Project catalog. It rejects absolute, noncanonical, overlong,
deep, and `.git` paths. Each directory component and the final file are opened beneath bound
directory handles, with symlink changes rejected. Symlinks and nonregular entries can be named in
the tree but cannot be followed or previewed. The directory read stops after 1,024 entries and
says it is truncated; text above 128 KiB or containing NUL or invalid UTF-8 is refused by name.

The paired Cloud console carries separate `project-tree-list` and `project-tree-read` words. Both
are reads on the Project's owning machine, available to a paired device with read access even when
the remote write switch is off. Selecting a file sends its text in the encrypted relay answer;
folder expansion sends only that folder's immediate names and metadata. No tree write word exists.
On a phone, the folder list and preview stack vertically, and the preview offers a return control
that focuses the selected file. The native folder disclosure and file buttons remain keyboard
reachable without relying on pointer-only actions.

## Unify

Unify gives one Project's Claude Code and Codex sessions the same rules file and the same skills.
What each assistant loads was measured, not assumed (2026-10-06, Claude Code 2.1.291, codex-cli
0.160.1; the table is in `docs/shared-project-instructions.md` §2): Codex reads `AGENTS.md` and
`.agents/skills/<name>/`; Claude reads `CLAUDE.md`, reads `AGENTS.md` only when `CLAUDE.md` is absent
or carries an `@AGENTS.md` line, and reads `.claude/skills/<name>/`, following a relative link there.
A Project is **unified** when its rules are in `AGENTS.md` with `CLAUDE.md` absent or importing it,
and every skill lives in `.agents/skills/<name>/` with `.claude/skills/<name>` resolving to the same
directory. Unify works at the repository root the inventory above uses
(`internal/adapters/projectfiles/unify.go`).

`GET /v1/projects/{place}/unify` returns a `ProjectUnifyPlan` (`api/v1/projects.schema.json`) and
writes nothing:

- `status` is `unified`, `drifting`, or `unknown`. Unknown means a rules file, skills directory or
  skill could not be read (not UTF-8, over 128 KiB, a permission error, or more than 1,024 entries in
  one skills directory or skill tree); it is never reported as unified.
- `version` is a SHA-256 of every input the plan read: file contents, link targets, each skill's
  tree (names, types, executable bit, bytes), and whether links are available.
- `rules` says which files Claude and Codex read now and after, whether `CLAUDE.md` imports
  `AGENTS.md` (a `CLAUDE.md` that is a link to `AGENTS.md` counts), the non-blank `CLAUDE.md` lines
  Codex does not see, as text, and the other Claude-only files present (`.claude/CLAUDE.md`,
  `CLAUDE.local.md`, `.claude/CLAUDE.local.md`), which unify lists and never changes.
- `skills` has one row per name in either directory: where it is (`agents`, `claude`, `both_same` —
  identical trees or already linked — or `both_different`), whether it is linked or a watched copy,
  who sees it now and after, and its action.
- `actions`, in the order apply runs them, each with its paths, a sentence for a person, the link it
  creates, and for rules files the exact text before and after:
  `rules_create_agents` (no `AGENTS.md`; `CLAUDE.md`'s rules become `AGENTS.md` and `CLAUDE.md`
  becomes `@AGENTS.md`), `rules_add_import` (both exist; `@AGENTS.md` is inserted as the first line of
  `CLAUDE.md` and nothing else moves), `skill_link`, `skill_move_and_link`,
  `skill_replace_copy_with_link`, and `skill_copy` where links are unavailable.
- `conflicts` are what unify does not decide: `claude_only_lines` (shown while the import is
  missing: Codex does not see these lines; move the shared ones into `AGENTS.md` — once the import
  exists the lines are listed in `rules` as Claude-specific and are not drift),
  `import_without_agents`, `rules_link`, `skill_differs`, `skill_link_elsewhere` (including an
  absolute link), `skills_directory_link`, `name_taken`, `too_large` and `unreadable`.

`POST /v1/projects/{place}/unify` takes `{"version": "<plan version>"}` and `Idempotency-Key`. It
recomputes the plan under a per-repository lock and refuses `409 plan_changed` when the version
differs and `503 plan_unknown` when the plan is unknown; nothing is written in either case. Then it
performs each action, re-reading that action's inputs first:

- Rules files: `CLAUDE.md` must still hold the planned text and is replaced through the same
  temporary-file-and-rename save as the editor; a new `AGENTS.md` is written to a synced temporary
  file and hard-linked to its name, which fails rather than replaces when the name appeared.
- Skill links are relative (`../../.agents/skills/<name>`), created beneath the bound
  `.claude/skills` handle, refused with `409 name_taken` when the name exists, and checked to
  resolve to the `.agents/skills/<name>` directory before the action counts.
- A move is one rename within the repository, after checking the tree still matches the plan, and
  is checked again at its destination; replacing an identical copy renames the copy aside, creates
  the link and only then removes the copy. A failure renames back.
- A copy goes to a hidden staging directory, is compared with the planned tree, and is renamed into
  place only if the destination is still absent.

The answer is a `ProjectUnifyApplied` with the actions that ran and the plan read again from disk,
`unified` unless conflicts remain. When an action fails the run stops: the answer is `500` with
`outcome: "stopped"`, `ran`, `failed`, the refusal `error` and `detail`, and the recomputed plan.
Within one action nothing is left half-written. A retried key replays the first answer instead of
running again. Unify never commits to git, never edits a rules file to resolve a conflict, and
never follows or replaces a link it did not create.

From a terminal, `clawdline project unify [directory]` (default: the current directory's git
top-level, which must be a Project this machine lists) prints the plan for a person; `--apply`
applies the plan it just printed by sending its version; `--check` prints only `status:` and the
drift and conflict lines and exits 0 unified, 1 drifting, 3 unknown; `--json` prints the daemon's
answer. The `clawdline` skill's `/clawdline unify` follows the guide's Unify part: it shows the plan,
applies only after the person's message approves it, and shows the `--check` result.

The paired Cloud console has two words: `project-unify-plan`, a read available to a paired device
with read access even when the remote write switch is off, and `project-unify-apply`, which needs
the remote write gate and carries the request's idempotency key. A lost apply answer is uncertain:
read the plan again before saying it applied. Both are listed as deferred in the console's carry
table until the Project settings screen asks for them.
