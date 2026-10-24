# Project instruction and skill files

The gear on each Project row opens that Project's settings. Its first section inventories disk candidates for shared Codex/Antigravity instructions, Claude Code instructions, Antigravity-only candidates, and Skills. It shows each filename, its path relative to the Project or the machine's home directory, its source, and whether it can be read or edited. The inventory does not claim that a running assistant has loaded a file: overrides, plugins, and the time a Session started can change the effective instructions.

The Project scope runs from the nearest repository root through the selected place. It includes `AGENTS.md`, `CLAUDE.md`, `CLAUDE.local.md`, `.claude/CLAUDE.md`, `.claude/CLAUDE.local.md`, direct `SKILL.md` children under `.agents/skills` or `.claude/skills`, and direct `.md` files under `.claude/commands` at each layer. The home scope includes `.codex/AGENTS.md`, `.claude/CLAUDE.md`, `.claude/CLAUDE.local.md`, direct skill children under `.agents/skills`, `.codex/skills`, or `.claude/skills`, and direct `.md` files under `.claude/commands`. Missing common instruction files remain listed as missing. Home files are read-only in this interface because changing one may affect every Project.

`GET /v1/projects/{place}/files` lists candidates; `GET /v1/projects/{place}/files/{file}` returns UTF-8 content and its SHA-256 version. `PUT` to the same file route takes `expected_version` and `content`, plus `Idempotency-Key`. The daemon resolves the place from its current Project catalog. It accepts only a file ID from the bounded inventory, never a browser-supplied path. Regular files are opened beneath bound directory handles; symlink or directory swaps are refused. A save requires the version the editor read, writes a temporary file in the same directory, syncs it, and renames it. Concurrent edits through this API are serialized; a change made elsewhere returns `file_changed` and leaves the browser's draft intact. The interface does not create, delete, or rename files.

The paired Cloud console carries three distinct words for the inventory read, content read, and save. The Cloud Project ID is resolved to its owning machine before a request is sent. Paired devices have Read and Send; save carries the editor's idempotency key. A lost save answer is treated as uncertain: the browser reads the file again and confirms matching content before it says the save succeeded. The Cloud relay carries encrypted request and answer content; it does not use a public document URL for these files.

The editor guards its draft when a person closes the modal, changes page, selects another file, or reloads the inventory. On a phone the groups and content stack in one column. Closing the modal returns keyboard focus to the Project gear that opened it.

## Project file tree

The same Project gear has a Files view beside Settings and instructions. It opens the selected
Project's own directory, not a repository ancestor or the machine's home directory. Folders are
loaded only when expanded; a file's content is requested only when selected. This view is read-only.
The existing instruction and Skill editor remains in Settings and instructions.

The Playbook editor uses this inventory's ready Skills and Claude commands to fill a
new launch-message draft. A command import asks the assistant to read that Project's
command; the command file remains necessary when it runs. A manually written Playbook
can stand alone. See [Project Playbooks](playbooks.md).

`GET /v1/projects/{place}/tree?directory=<relative>` lists one directory;
`GET /v1/projects/{place}/tree/file?path=<relative>` reads one regular UTF-8 text file. The server
resolves `{place}` from its current Project catalog. It rejects absolute, noncanonical, overlong,
deep, and `.git` paths. Each directory component and the final file are opened beneath bound
directory handles, with symlink changes rejected. Symlinks and nonregular entries can be named in
the tree but cannot be followed or previewed. The directory read stops after 1,024 entries and
says it is truncated; text above 128 KiB or containing NUL or invalid UTF-8 is refused by name.

The paired Cloud console carries separate `project-tree-list` and `project-tree-read` words. Both
are reads on the Project's owning machine. Selecting a file sends its text in the encrypted relay answer;
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

### In the console

The Project gear's Settings and guidance view has a block titled “Shared by Claude and Codex” above the file
editor (`web/console/src/pages/projects/ProjectUnify.tsx`; the view model is `project-unify.ts`). It
reads the plan when the view opens and shows one line: Shared, Drifting (n differences) where n counts the
planned actions plus the conflicts, or Unknown (reason) — the paths an `unknown` plan could not read,
or the refusal when the plan itself could not be read, with a Check again button. A failed read is never
shown as unified or as no drift.

Its button View changes opens the preview, which is the confirmation; there is no second dialog:

- **Two columns, Claude and Codex**, listing every rules file and every skill by name, each marked
  Unchanged, Added (visible after applying) with "Cannot see now → Can see after applying", or Not visible (this is the gap) for something
  that stays unseen after apply. Claude-only rules files are listed as unchanged.
- **Steps to take (in order)**: the plan's `actions` in apply order, each as a sentence the console writes
  from the action's kind and paths (the plan's English `description` is for the terminal). A rules
  edit is drawn as its after-text with inserted lines marked `+` and moved-out lines marked `−`; long
  unchanged runs fold to a count. A skill action is drawn as `from → to` with what the arrow means
  (Links, Move it and leave a link at the original location, Replace the identical copy with a link, Copy). Below them: which files move, which are created,
  and "Nothing will be deleted" — or, when an identical copy is replaced by a link, which copies are removed.
- **What unify cannot decide for you**: each conflict with a sentence and what the person can do; for
  `claude_only_lines` the lines Codex cannot see.
- One line saying unify does not commit to git.
- A `unified` plan shows only the columns and “Claude and Codex already see the same rules and skills”; an
  `unknown` plan says what could not be read and offers no apply.

Apply these changes is enabled only when the plan has actions and is not unknown. It sends the plan's
`version` with a fresh `Idempotency-Key` per press. `plan_changed` says the files changed since the
preview and rereads it; a stopped run shows which actions ran, which failed and the plan read again;
success shows the new plan. An answer that was lost (no response, an unreadable body, or Cloud's
`outcome: "unknown"`) rereads the plan and says what it shows, and says Shared only when the reread
says so. Any other refusal that is not one sent before an action ran (`plan_unknown`, `forbidden`,
`bad_request`, the Cloud gates) also rereads the plan and says some actions may have run. At phone
width the preview fills the screen and the columns stack; closing it returns focus to View changes.

The Projects health check's readiness card has a “Claude and Codex share rules and skills” row
(`web/console/src/pages/projects/project-setup.ts`): Shared, Drifting (n differences) with the same count, or
Unknown. Its status comes from the places answer: `GET /v1/places` carries `setup.unify` (`unified`,
`drifting` or `unknown`) and, when drifting, `setup.unify_count`, computed by the same read-only
`Plan` for each place (`internal/transport/http/project_setup.go`). A place that cannot be planned
is `unknown`, never `unified`. A daemon older than the field sends neither; the row then says Unknown,
offers no button and is left out of the card's score, and a missing count is not shown as 0. The
row's View sharing opens that Project's settings view and, once its plan is read, the preview above; a
plan that cannot be read leaves the preview shut and moves focus to the block's status line, which
says why.

`Plan` reads two rules files, three Claude-only paths and the two skills directories, and limits
each skill tree to `MaxScanEntries`. The start-place route now reads Project setup in a bounded
background lookup: at most four readers run, the response waits at most one second for new reads,
and a blocked filesystem open does not hold the place list or the installer's first-session check.
An unfinished setup is omitted, so the console treats that row as unknown. Completed values are
reused and refreshed after sixty seconds; a later list request can show setup that finished after
the first response. This bound was added after the v1.0.1 installer still timed out while
`projectfiles.Plan` waited in `os.OpenRoot` on one checkout.

The paired Cloud console carries both words (`web/console/src/cloud/carry.ts`):
`project-unify-plan`, a read available to a paired device, and `project-unify-apply`, which carries the
request's idempotency key. A stopped run crosses whole: the Cloud bridge sends the
`ProjectUnifyApplied` body — `ran`, `failed`, the recomputed plan, and the refusal's `error` and
`detail` — as the answer's `body` beside status 500 (`cloudops.stoppedUnify`), not as an `error`,
whose fields the reader filters by code. The console's Cloud writer answers it as the local route
does, `500` with that body, so the screen shows the same stopped view on either transport. Any
other refusal still crosses as an `error`, and a lost answer is still uncertain and rereads the
plan.

## Antigravity CLI applicability

The inventory retains one canonical row and ID per candidate. `assistant` remains the
legacy primary provider; optional `assistants` reports all applicable providers.
Project `AGENTS.md` and `.agents/skills/<name>/SKILL.md` apply to both Codex and
Antigravity CLI. Global `.agents/skills` remains Codex-only: the CLI documents its
own global `.gemini/antigravity-cli/skills` directory.

The bounded inventory also lists existing `GEMINI.md`, `.agents/AGENTS.md`,
`.agents/GEMINI.md`, direct `.md` children of `.agents/rules` and legacy
`.agent/rules`, and `.agent/skills/<name>/SKILL.md` at each selected project layer.
These Antigravity-only candidates are read-only, including project files. Global
candidates include `.gemini/AGENTS.md`, `.gemini/GEMINI.md`, the corresponding
`.gemini/config` files, direct `.md` children of `.gemini/config/rules` and
`.gemini/antigravity-cli/rules`, and direct native CLI skills. Plugin directories,
custom `rules.json` inheritance, and arbitrary descendants are not traversed.
Rules with activation frontmatter remain candidates, not confirmed active rules.

Unify now reports Antigravity applicability in `rules.now/after.antigravity` and
`skills[].now/after.antigravity`. Arrays are explicit even when empty; skill
booleans preserve false. Missing or null fields from an older machine mean
unreported. The console and CLI do not derive these fields from Codex data.
`native_only` lists root Antigravity-only candidates with their path, kind and
inspection status; an empty array means checked, and missing/null means unreported.
These files participate in the preview digest but no Unify action moves, links,
copies, overwrites or deletes them. Unify's status describes its managed shared
workspace layout; separate native rules and skill activation can still differ.
Existing `.claude` link/copy operations expose `.agents` skills to all three
providers without introducing a third copy.

Source boundaries: the [official CLI skills documentation](https://antigravity.google/docs/skills)
and [CLI rules documentation](https://antigravity.google/docs/rules), read on
2026-10-22, document the native locations. The root's synthetic native probe
observed a distinct workspace `AGENTS.md` marker alongside hook context; it did
not prove every global path, activation mode or skill execution. This inventory
and its synthetic browser tests do not establish a running Session's context,
launch permission, successful skill receipt, Cloud acceptance, or paid-model access.
