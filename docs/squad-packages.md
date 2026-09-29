# Offline squad packages

An offline squad package is a ZIP file for shareable teams, personas and skills. It never
downloads a dependency, runs a script, installs a provider skill or adopts a third-party
candidate by itself. Preview and human adoption are separate operations.

## Make an example

[`examples/ai-squad/`](../examples/ai-squad/) contains two teams, two personas and two
skills. Each team's `personas` and each persona's `skills` array is ordered. Both teams reuse
the same personas, and both personas reuse the same skills in a different order. The committed
[`ai-squad-example.zip`](../examples/ai-squad-example.zip) is built with:

```sh
go run ./tools/squadpack examples/ai-squad /tmp/ai-squad-example.zip
```

The builder refuses symlinks and over-limit source files. It creates a new output with mode
0600 and never includes `private` entries; the Console's authenticated export flow is the only
way to select private scopes. Remove an old output before building it again.

## Manifest

`manifest.json` is UTF-8 JSON with no duplicate keys or trailing value. The archive format is
`clawdline.squad-package`, version `1`. It declares a non-reserved namespace, source and
license, and arrays of `teams`, `personas`, `skills`, and optional `private` settings files.
All files except the manifest must be listed exactly once. Each definition has a namespaced
ID, bounded version string, display name, 8-by-7 icon, text `body` path and SHA-256 of
that file's original bytes. A team lists persona IDs; a persona lists skill IDs; each reference
must be present in the same package. Paths are lower-case ASCII relative paths under
`definitions/` or `private/`. A private entry identifies `global`, `repo` or `place` scope and
its Project ID, and has a JSON file path and SHA-256. The parser preserves the original file
bytes and ordered references; the contribution builder fills the SHA-256 values.
The manifest namespace identifies the package author; it does not constrain each definition's
namespaced ID, so a local catalog export can include definitions from multiple sources.
Numeric `major.minor.patch` versions support ordered upgrades. Other version strings remain
immutable and require a separate human decision when an existing ID has different content.
Optional `name_zh_hant`, `summary`, `summary_zh_hant`, `purpose`, and `purpose_zh_hant` fields
preserve the catalog's display text when packages are exported and imported again.
An optional persona `disabled_skills` array names a subset of its ordered `skills` IDs; the
remaining references are enabled by default.

A selected private JSON file contains `{"settings":[{"definition_id":"vendor.persona.writer",
"payload":{"handbook":"...","auto_assign":true,"skills":[]}}]}`. The empty definition ID
is reserved for a motion override payload such as `{"motion":false}`. Source-machine settings
versions are omitted: adoption compares the target settings state recorded at preview time.
Private scope IDs are matched exactly, and preview lists which scopes the ZIP declares.

The parser rejects every unlisted archive entry, paths that escape the ZIP, links, duplicate
names under case folding, unsupported compression or encryption, mismatched ZIP headers,
invalid UTF-8, stale format versions, duplicate IDs, unresolved references, built-in IDs and
digest mismatches. Its refusal is a stable code with no local path or private content in the
message. The size register is [N58 in limits.md](limits.md#n58-offline-squad-package-input).

## Privacy and adoption

Export defaults to shareable definitions only. Global and each `repo` or `place` Project scope
must be selected individually and confirmed by a write-capable person before its private
handbook or overrides can enter an export. Package metadata records the source and license
as the author declared them; Clawdline does not certify either claim. Imported text remains
untrusted data and cannot override user, repository, task or safety instructions.

Adoption requires a completed server preview of the same original ZIP bytes and target scope.
The service compares the preview's catalog version and its archive SHA-256, requires explicit
conflict choices, rechecks the complete reference graph, and publishes the selected definitions
and settings in one database transaction with a durable idempotent receipt. A changed catalog
requires a new preview.

## HTTP and Cloud contract

All three HTTP routes use `POST`, `Content-Type: application/json`, and base64 ZIP bytes in JSON.
The request body is limited to 1 MiB; the original ZIP is limited to 512 KiB.

| Route | Request body | Response |
| --- | --- | --- |
| `/v1/squad-packages/preview` | `{"archive_base64":"...","scope_id":"global"}` | `archive_digest`, `catalog_version`, `scope`, `source`, `license`, `private_scopes`, `changes`, `preview_digest`, `preview_token`, `expires_at` |
| `/v1/squad-packages/adopt` | `archive_base64`, `archive_digest`, `preview_digest`, `preview_token`, `catalog_version`, `scope_id`, `choices`, `private_scopes`, `confirm_private` plus `Idempotency-Key` header | `catalog_version`, `archive_digest`, `adopted_ids`, `private_scopes` |
| `/v1/squad-packages/export` | `{"private_scopes":[],"confirm_private":false}` | `archive_base64`, `archive_digest`, `file_name`, `mime_type: application/zip` |

Preview and public export allow an authenticated paired reader. Adoption and export with selected
private scopes require a local or paired human with Send permission. Preview grants expire after
15 minutes. A conflict choice is `{"definition.id":"keep"}` for each conflicting item; there
is no in-place replacement of an immutable version. A catalog change returns `version_conflict`,
a settings change returns `settings_changed`, an expired preview returns `preview_expired`, an
invalid credential returns `preview_invalid`, and reuse of an idempotency key for different bytes
returns `idempotency_conflict`. The ZIP is uploaded again for adoption and validated again.

Cloud sends the same request body in a `package` subobject beside `type`, `session`, and `request`.
The words `squad.packages.preview` and `squad.packages.export` are read operations; the latter
refuses any private scope. `squad.packages.adopt` and `squad.packages.export.private` are write
operations. Each local route repeats its own permission check.

## Contribution checklist

1. Pick a namespace you control; never use `clawdline.*` or an existing built-in alias.
2. Keep IDs stable across releases. Increase the version when a definition's contents change.
3. Put all referenced personas and skills in the same package and keep array order intentional.
4. Put shareable definitions under `definitions/`. Keep private handbooks out of a public ZIP.
5. Run the builder, parse the resulting ZIP through the preview flow and inspect every reported
   source, license, conflict and dependency before adopting it.
