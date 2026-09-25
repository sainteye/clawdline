# Project settings across machines

A project has settings that git does not carry: the name and icon Clawdline draws for it, and the
project-local assistant files a person keeps out of the repository — skills, commands and agents
under `.claude/`, and `CLAUDE.local.md`. On a second machine, such as a Linux host joined to the
same Cloud account, those are missing even when the repository itself is there.

This page is how one machine owns those settings and others read them.

## The shape

- **One side owns, the other reads.** A *source* offers its projects; a *mirror* applies them and
  keeps a record of what it applied. A mirrored project's icon cannot be changed on the mirror
  (`409 project_mirrored`), and a mirror never offers what it mirrors, so settings travel in one
  direction and two machines cannot echo each other.
- **The identity is the repository, not the folder.** Projects are matched by their `origin`
  remote spelled as `host/owner/name` (`git@github.com:Acme/Shop.git` and
  `https://github.com/acme/shop` are one project). A checkout without an origin, or whose origin is
  a local path, is listed as skipped with its reason and never matched by name
  (`docs/project-icons.md` explains why a path or a label is not an identity).
- **Only what git does not carry travels.** Untracked and ignored files under `.claude/skills`,
  `.claude/commands` and `.claude/agents`, and `CLAUDE.local.md` — except dot files and dot
  directories inside them (`.env` is where an ignored secret sits, not a skill). A tracked file
  arrives with the repository and is never written over. `.claude/settings.local.json` is deliberately excluded:
  it holds the source machine's permission grants and absolute paths, and copying a grant to
  another machine is an escalation nobody chose there.
- **A mirror does not destroy an edit.** Each apply compares every file with what the mirror last
  wrote. A file somebody changed on the mirror is kept and named (`local_edit`); a file the
  source dropped is removed only if nobody changed it. A write never passes through a symbolic
  link (`unsafe_path`). A file the source has but did not send — over the 256 KiB file bound, past
  the entry's budget, or unreadable — is named as `withheld`, and a mirror keeps its copy rather
  than treating it as deleted. When a mirrored checkout moves to another path, the files the mirror
  wrote at the old path stay there untouched.
- **A repository name cannot become a local path.** `host/owner:name`, which git reads as a local
  path, is refused, and no segment of the name may start with `.` or `~`, so a clone can never
  create `~/.git` or `.ssh`. An apply that arrives while its repository is still being cloned
  answers `cloning` and writes nothing.
- **A missing repository can be cloned** when the person asks, into the directory most of that
  machine's projects already sit in (or `CLAWDLINE_PROJECTS_ROOT`, else `~/projects`). The clone
  never prompts for credentials: a remote the machine cannot reach fails with git's own last line,
  shown on the page. At most two clones run at once.

## Transport

Each Cloud machine has its own content key, so two machines cannot read each other, and the relay
reads neither. The **browser is the courier**: it is paired with both, reads the source's offer,
and writes each project to the mirror over the mirror's own authenticated command channel. No
Cloud service stores project settings, and no protocol change was needed.

| Word | Kind | Route on the machine |
|---|---|---|
| `project-manifest` | read | `GET /v1/project-sync/manifest` — every offered project, hashes but no contents |
| `project-entry` | read | `GET /v1/project-sync/entry?repo=` — one project with its file contents |
| `project-mirror` | read | `GET /v1/project-sync/mirror` — what this machine mirrors, and its clones |
| `project-mirror-apply` | command | `POST /v1/project-sync/mirror` — apply one project |
| `project-mirror-detach` | command | `DELETE /v1/project-sync/mirror?repo=` — make it this machine's own again |

The two commands pass the same gates as every other Cloud write: the receiving machine must have
remote commands switched on (`clawdline cloud commands on`), and the device must be allowed to
send.

### In the console

Open the receiving machine in the hosted console, go to **Projects → 專案設定同步**, choose the
source machine, read its projects, pick the ones to mirror, and optionally allow cloning. After
that, opening the Projects page on the mirror compares every mirrored project with its source's
current revision and applies what changed. Nothing new is mirrored without a person choosing it.

This is "when a viewer looks", not a background service: neither machine can reach the other on
its own. A machine that is offline is reported by name and the others still update.

### Without Cloud

The free version runs on one machine, but the same data can be moved by hand:

```sh
clawdline project export --out projects.json          # on the source
clawdline project import --clone projects.json        # on the mirror
```

The bundle contains file contents; treat it like the files themselves.

## State and bounds

The mirror's record is `CLAWDLINE_NEXT_DIR/project-mirror.json` (0600, written through a synced
temporary file and a rename). An unreadable record refuses startup rather than turning every
mirrored project back into a local one. The capacity register covers: 256 projects per manifest,
64 files per project, 256 KiB per file, 512 bytes per path, 4 MiB per applied project, 512
mirrored repositories and two concurrent clones.

## What was left out

- A background push from the source. It would need machine-to-machine keys, which the Cloud
  design deliberately does not have.
- User-level skills under `~/.claude/skills`. They are not a project's, and this product does not
  write the person's home configuration.
- Mapping a project with no portable origin by hand.
