# AI squad v1 contract

This document is the implementation contract for the AI squad Epic. The
existing built-in persona catalog remains the compatibility source for its 42
short IDs. The squad adds shareable definitions, private settings, immutable
launch snapshots, and a truthful record of skill use. It does not install a
provider skill, fetch a marketplace, or run content from an imported package.

The Console can import a local Project, Claude Code, or Codex `SKILL.md` as
text, or capture its entire selected folder. Folder imports store file bytes
and safe relative paths in the versioned skill, then publish those exact files
beside `SKILL.md` in a fixed launch snapshot. Importing does not install the
folder into a provider's global skill directory or execute its scripts.

## Identity and ownership

- A definition has a stable `definition_id`, a version, source, license, body,
  icon, and content digest. Built-in IDs use `clawdline.persona.<short-id>`;
  the short ID is only a compatibility alias for that built-in definition.
  Imported definitions require a non-reserved namespaced ID. Neither an
  imported package nor a setting may redefine a built-in ID or alias.
- Teams and skills have their own namespaced IDs and versions. Team-to-persona
  and persona-to-skill references are ordered; a persona can occur in multiple
  teams and can reference zero or more skills. A skill can be referenced by
  multiple personas. Name and display order are not identity.
- The launch resolver maps an input short alias or definition ID to one exact
  definition version. All new-session entry points use it: the person's start
  action, Board assignment to a new session, agent dispatch, and restore.
  Unknown IDs fail with `unknown_persona`; a deleted catalog entry never
  invalidates an existing snapshot. Process discovery uses the immutable
  `launch_id`/`snapshot_id` rather than parsing namespaced IDs from argv.
- Project settings use a tagged `repo` or `place` scope. The former is based on
  the repository's common directory and is shared by its linked worktrees.
  The latter uses `place:` followed by `PlaceID` for a non-Git absolute path.
  `CanonicalProjectKey`'s boolean only reports an absolute path; it does not
  report whether a Git repository was found. A moved repository is a new scope;
  its prior settings remain under the prior ID until a person migrates them.

## Effective settings

For each setting, resolve definition default, global override, then Project
override. The result includes `value`, `source`, `present`, and `version`.
Project handbook text replaces the whole global handbook. An explicit empty
string is an override; an absent Project value inherits. Restoring inheritance
deletes only the Project presence marker. Persona auto-assignment eligibility,
skill ordering and enabled state, and motion preference use the same explicit
presence model. Built-in definition text is read-only. A write includes the
expected settings version; stale writes return `version_conflict` and current
version without dropping the submitted draft.

Every built-in persona is initially eligible for management-agent auto
assignment. Only sessions whose authenticated persona is `product-manager` or
`architect` use the enabled list as their automatic candidate set. Explicit
human selection in the new-session and Board flows may still choose a disabled
persona. The human-invoked AI suggestion returns a suggestion, never dispatches
a task, and labels disabled personas for the person's decision.

An agent dispatch must authenticate an actor separately from the machine's
shared local token. Launch mints a random per-session capability, stores its
hash against `launch_id`, and binds its terminal ID after terminal creation and
its conversation ID at first observation. The CLI sends that capability for
agent dispatch, child creation, and assignment. The server derives the actor
persona and Project from the binding; a supplied conversation ID is only an
equality check. Missing, invalid, unbound, revoked, or cross-session actor
capabilities fail with a typed refusal for a snapshotted Session. A pre-feature
Session, or a Session opened without a role, retains the legacy dispatch route;
it has no snapshot identity and cannot claim the new management-role candidate
list. Tests cover spoofing in both directions and direct HTTP requests.

This protects the API boundary against cross-session identity claims and
accidental misuse. Processes running as the same OS user share a trust domain:
0600 files alone do not isolate a malicious local process from another local
agent. This contract does not claim stronger same-user isolation.

## Launch snapshots and skill events

Before opening a terminal, persist a random `launch_id`, a pending intent, and
an immutable content-addressed snapshot of the resolved definition, handbook,
effective skill order, each skill's full text/version/source/digest, and
Project scope. Write complete temporary files, verify their digests, and
atomically publish them before launch. Provider prompt files reference the
snapshot, not mutable catalog files. Claude reads its fixed system prompt
file; Codex reads the fixed file named by its developer instruction. The
provider is instructed to read a skill when the task calls for it, not on
every turn. User instructions, safety boundaries, repository instructions,
and the task brief retain precedence over imported content.

The terminal API returns a terminal ID only after it opens a tab or session.
Commit that ID to the pending intent after success. Bind a conversation ID
once, after observation, and never infer a binding from ambiguous matches.
If the terminal opened but the update failed, keep a recoverable pending
intent for a scan; if the terminal never opened, clean only unreferenced
temporary data. Resume uses the original conversation snapshot even after a
catalog edit, Project move, or daemon restart. Pre-feature conversations are
reported as `legacy_unsnapshotted`, with no claim of historical immutability.

A bound session gets a capability for reporting only skills present and enabled
in its snapshot. An event has snapshot, conversation, definition, Project
scope ID, skill ID/version, client event ID, status `read|applied|failed`, timestamp, and an
optional failure code. Validation and a durable receipt are one transaction.
The same event ID and payload returns the original receipt; a different
payload with that ID returns `event_conflict`. A monotonic sequence and `after`
cursor let clients catch up after reconnect. `applied` means "Session reported
use"; it cannot prove the output was caused by the skill. The UI only animates
new receipts after its initial cursor, for the matching session, definition,
and Project scope, and never on replay, `read`, `failed`, or cross-session
events. Motion defaults
on, Project can override it, and `prefers-reduced-motion` always wins.

## Offline package and privacy boundary

The v1 package is an offline ZIP with a versioned manifest, namespace, source,
license, file paths and SHA-256 digests, plus zero or more teams, personas, and
skills with ordered references. Preview validates the whole archive before any
write: format and text encoding, path traversal, absolute paths, symlinks,
case-insensitive duplicates, duplicate IDs, size and expansion limits,
digests, missing references, and built-in ID conflicts. All limits belong in
`internal/domain/capacity`; drive its registry guard red before adding them.
Preview names additions, updates, conflicts, dependencies, archive digest, and
catalog version. Adoption supplies the same digest, expected catalog version,
and explicit conflict choices. A changed target requires a new preview.
Publish all definitions and references atomically, with an idempotent receipt;
a failed or repeated adoption never leaves half a package. Preview is not
installation. Source and license metadata are displayed for review, but the
parser cannot certify their truth or rights to external attachments.

Export defaults to shareable definitions only. A person explicitly selects
each global or Project private setting scope and confirms it before download.
An export containing a folder skill refuses with `skill_folder_export_unsupported`;
the current offline package format only carries skill text and must never
silently omit attachments or add them to a shareable archive.
Neither imported text nor a manifest may execute scripts, fetch URLs, send
data, authorize a paid API, or override existing user and repository rules.
Candidate research for all 42 built-ins is recorded separately from review
and adoption. Following the person's 2026-09-29 integration request, 11
reviewed candidates have Clawdline-specific, locally bundled adaptations in
the built-in skill catalog and are enabled by default for their matching
roles. The remaining 31 roles have no default skill. A global or Project
override can reorder, disable, or remove a role's effective skills. No skill
is shown as used merely because it is available; only a matching Session
receipt can report a read, application, or failure. Older Session snapshots
retain their original skills after this catalog change.

Catalog metadata can be public. Complete definitions, handbooks, skill bodies,
session snapshots, and private package data require an authorized local reader
or a Cloud device paired to read that machine. Settings writes, adoption, and
private export require a write-capable human route. A session capability only
reads its own snapshot and submits its own events through HTTP; a shared
machine token does not by itself authorize those operations. Current paired
reader permission is machine-wide, including Project content, and the UI must
say so. Anonymous and unpaired devices are denied; Cloud read-only devices
cannot write. The Cloud relay command table must explicitly include every
new route and retain its permission check. Do not put private content into
URLs, logs, telemetry, public Git, or unencrypted browser caches.

## Console acceptance

Use the existing Console shell and route registry. On entry show global scope;
switching Project shows effective values and their sources without mutating
another Project. The detail view shows full definition, source, version,
read-only global handbook alongside the Project handbook, ordered skill cards
and full skill content. The name, status, and team remain readable without
relying on icon color. The mobile detail view has a visible return to the
original card and restores keyboard focus; team and skill changes keep focus
in the updated UI. Dialogs have accessible names and return focus to their
opener. Loading, empty, offline, permission, validation, conflict, and retry
states are separate. Failed saves retain the draft. Verify all 42 built-in
personas, long text, zero skills, imported teams, 1440px desktop, 390px mobile,
larger text, keyboard, and screen reader semantics against real API data.

The prototype UX review found four blocking design gaps: Project setting
bleed, failure to show global and Project handbook text together, absent
mobile return, and lost keyboard focus. The integrated implementation needs a
separate independent UX review before merge.
