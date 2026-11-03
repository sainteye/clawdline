# Ops documents

A Project's Ops list holds the documents its operations depend on: files under the repository
and links to documents kept elsewhere (Google Docs, Sheets, Slides, Forms and Drive; Notion;
any other http(s) page). The console shows them beside the Project; a Session reads them with
`clawdline ops` when a Playbook or the person names them.

## Identity

- Every record belongs to the Project's **canonical key** (`projects.CanonicalProjectKey`): a
  linked worktree resolves to its main repository, so every worktree of one repository shares
  one list.
- A file record's `path` is relative to the canonical root (the directory the key names), with
  `/` separators, never relative to a worktree's own directory.
- New Markdown files are created at `clawdline-ops/<name>.md` under that root.
- `{place}` in a route is used only to find the Project; the records are filed by key.

## Where the code is

| Part | File |
|---|---|
| Ids, bounds, names, link types (no disk, no network) | `internal/domain/opsdocs/opsdocs.go` |
| Link type table, read by Go through `embed` | `internal/domain/opsdocs/link_types.json` |
| Create, save, stat and rename beneath a root | `internal/adapters/projectfiles/ops.go` |
| `info/exclude`, folder state, GitHub visibility | `internal/adapters/opsdocs/opsdocs.go` |
| Tables `ops_entries`, `ops_collections`, `ops_pending_renames` | `internal/adapters/store/ops_documents.go` |
| The service: create, update, rename, resolve | `internal/app/ops_documents.go` |
| Routes | `internal/transport/http/ops_documents.go` |
| CLI | `cmd/clawdline/opsdocs.go` |
| Wire types | `api/v1/projects.schema.json` (`Ops*`) |

The tables live in the same database as the Session to-dos. Asking a Session to read a link
updates the entry and creates the to-do in one Work v2 transaction.

## Types

A link's `type` comes only from its URL; nothing is fetched:

| URL | type |
|---|---|
| host `notion.so`, `www.notion.so`, `app.notion.com`, `*.notion.site` | `notion_page` |
| `docs.google.com/document/…` | `google_doc` |
| `docs.google.com/spreadsheets/…` | `google_sheet` |
| `docs.google.com/presentation/…` | `google_slides` |
| `docs.google.com/forms/…` | `google_form` |
| host `drive.google.com` | `google_drive` |
| any other http(s) URL | `web` |

A URL that is not http(s), carries a user name or password, or holds whitespace is refused
with `ops_bad_url`. A file's type is `markdown` for `.md`/`.markdown`, `text` for other UTF-8
text (sniffed from its first 4 KiB), and `file` otherwise.

`status` is `ready`, `resolving` (a Session was asked to read the link), `needs_access` (the
Session could not; `access_note` says what the person must do), `missing` (the file is gone)
or `unreadable` (not a plain file, or not readable). A file's status is checked on disk on
every read; it is not stored.

## Routes

Reads need a machine or paired-device credential. Writes need a device that may send, or this
machine's orchestrator credential (`clawdline ops`). Every write needs an `Idempotency-Key`
and goes through the receipt table; the digest includes the method, path and body, so the
same key with a different body is `409 idempotency_key_reused`.

| Method, path | Body / query | Answer |
|---|---|---|
| GET `/v1/projects/{place}/ops` | — | `OpsList` |
| GET `/v1/projects/{place}/ops/{id}` | — | `OpsRead`; `content` and `version` only for a readable text file of at most 128 KiB |
| GET `/v1/projects/{place}/ops/setup` | `?probe=github` optional | `OpsSetup` |
| PATCH `/v1/projects/{place}/ops/setup` | `{git: "include" \| "exclude"}` | `OpsSetup` |
| POST `/v1/projects/{place}/ops` | `{kind:"link", url, title?, collection?}` · `{kind:"file", path, title?, collection?}` · `{kind:"markdown", name, content, collection?, git?}` · `{kind:"collection", name}` | 201 `OpsEntryAnswer` or `OpsCollectionAnswer` |
| PATCH `/v1/projects/{place}/ops/{id}` | entry: `{title?, collection?, position?}`, file only `{rename}` or `{content, expected_version}`; collection: `{name?, position?}` | `OpsEntryAnswer` (with `version` after a save) or `OpsCollectionAnswer` |
| DELETE `/v1/projects/{place}/ops/{id}` | — | `OpsDeleted`. The record goes; the file stays. A deleted collection's entries move to no collection. |
| POST `/v1/projects/{place}/ops/{id}/resolve-request` | `{terminal}` | `OpsResolveRequested` |
| GET `/v1/ops?session={terminal}` | — | `OpsList` with `place` |
| GET `/v1/ops/{id}` | — | `OpsRead` |
| POST `/v1/ops/{id}/resolve` | `{todo, title}` or `{todo, needs_access: true, reason}` | `OpsEntryAnswer` |

### Markdown files

`name` has no extension, no `/` or `\`, no leading dot and no control characters; the file is
`<name>.md`. When `clawdline-ops/` does not exist yet and the root is in git, the request must
say `git` (`include` or `exclude`), or it is refused with `409 ops_git_choice_required` and
nothing is written. The file is written to a temporary sibling and hard-linked to its name, so
an existing name — including one that appears while the file is written — is
`409 ops_name_taken` and is never replaced.

### Saving and renaming

A save names the version it replaces; a changed file is `409 file_changed`. A rename stays in
the same directory and works on a plain file only:

1. The rename is recorded in `ops_pending_renames` before the disk is touched.
2. Both names are locked in sorted order; the old name must be a regular file (`Lstat`).
3. The new name is made with `os.Root.Link`, which fails rather than replaces an existing name
   (`ops_name_taken`); both names are checked with `SameFile`.
4. The record moves to the new path and the pending row is removed in one transaction.
5. The old name is removed. If the record could not be written, the new link is removed and
   the old name kept.

The daemon settles any pending row when it starts and before a Project's list is read: if the
record still holds the old path the new link is removed; if it holds the new path the old name
is removed. Refusals: `ops_rename_instruction_file` (an instruction, Skill or command file,
by name or in the `projectfiles.List` inventory), `ops_rename_case_only`, `ops_name_taken`,
`ops_rename_unsupported` (the volume could not make the second link).

### git

`PATCH …/ops/setup` adds or removes the one line `/clawdline-ops/` in the file
`git rev-parse --git-path info/exclude` names when run in the canonical root, which for a linked
worktree is the common directory's. Tracked files are never touched. Removal deletes only that
exact line.

`?probe=github` asks `GET https://api.github.com/repos/{owner}/{repo}` for a github.com
`origin`, with the connected GitHub integration's token when there is one and anonymously
otherwise. 200 with `private:false` is `public`, `private:true` is `private`; anything else,
or no answer in 3 s, is `unknown` with a `visibility_detail`. Answers are kept for ten minutes,
at most 64 repositories. Without the query `visibility` is `unchecked` and nothing is asked.

### Asking a Session to read a link

`resolve-request` takes a Session terminal working in the same canonical Project
(`409 ops_session_other_project` otherwise). It creates — and does not send — a direct to-do
filed under that Session's conversation, completes an earlier resolve to-do of this entry that
was not yet read, and marks the entry `resolving`, all in one transaction. The caller sends the
to-do with the existing `POST /v1/work/v2/session-todos/{terminal}/{todo}/send` (Cloud:
`work.v2.todo-action`). The to-do tells the agent the URL, to read it with whatever connector
it has, never to paste the content anywhere, and the two `clawdline ops resolve` commands.

`POST /v1/ops/{id}/resolve` is accepted only with this machine's orchestrator credential and
not from a Cloud viewer (`403 ops_resolve_agent_only`), and only for the entry's current
`resolve.todo` (`409 resolve_stale`). A title the person set (`title_source: "user"`) is never
replaced; otherwise the title becomes the agent's and `title_source` is `agent`.

## Bounds

Registered in `internal/domain/capacity` (`projects.ops_*`):

| Bound | Limit |
|---|---|
| entries per Project | 500 (`ops_full`) |
| collections per Project | 64 (`ops_full`) |
| title | 200 bytes (`ops_bad_title`) |
| collection name | 100 bytes (`ops_bad_name`) |
| Markdown file name, with `.md` | 255 bytes (`ops_bad_name`) |
| URL | 2048 bytes (`ops_bad_url`) |
| `access_note` / `reason` | 1000 bytes (`ops_bad_reason`) |
| ids per `clawdline ops read` | 16 |
| GitHub probe | 3000 ms |
| GitHub answers kept | 64 |
| file content | 128 KiB (`projectfiles.MaxFileBytes`) |
| write body | 256 KiB (`projectfiles.MaxWriteBytes`) |
| resolve to-do text | the direct to-do limit, 8 KiB |

## Cloud words

Reads: `project-ops-list`, `project-ops-read`, `project-ops-setup`. Commands (write switch,
paired device): `project-ops-create` (POST `/ops`), `project-ops-update` (PATCH `/ops/{id}`;
`id: "setup"` is PATCH `/ops/setup`), `project-ops-delete`, `project-ops-resolve-request`.
Sending the to-do is `work.v2.todo-action`. Resolving is not a Cloud word: only the agent on
this machine reports.

## CLI

```sh
clawdline ops list     [--session <terminal>] [--project <dir>] [--json]
clawdline ops read     <id>... [--json]
clawdline ops add-link <url> [--title <title>] [--session <terminal>] [--resolve] [--project <dir>]
clawdline ops resolve  <id> --todo <todo id> (--title <title> | --needs-access --reason <text>)
```

Without `--session` the Project is the current directory's Git top level. `add-link --resolve`
creates the resolve to-do in the `--session` Session; sending it is the person's (the send
route refuses a Session's own credential).
