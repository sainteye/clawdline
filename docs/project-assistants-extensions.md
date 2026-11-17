# Selected Bot skill folders and connector transports

This construction extends API 82/schema 8 at `81ea1d6a1920b04396c0b7279a83a5694c812e7f`.
Source API 83/schema 9 adds connector credential actions and additive credential/hold tables.
Historical archives, receipts and aggregate bytes are not rewritten. Omitted extension edits
still preserve the selection; explicit empty arrays deselect it. Network Bot and Cloud-assistant
reachability remains a separate construction unit. This document claims local fixture proof,
not installed vendor/model, production Cloud, phone or Release acceptance.

## Source-owned definitions

The installed Squad catalog supplies skill content and base64 file manifests. A Bot edit sends
only stable IDs and current fingerprints. Folder fingerprints include every asset byte, path,
and definition field. No package path, executable, environment map or archive is accepted in a
Bot edit. Selection is not permission to install or run package instructions.

Private `project-assistants/connectors.json` remains the connector definition port. Existing
HTTP definitions retain their fingerprints when the newly optional fields are absent. A stdio
definition supplies an absolute `Executable` and literal `Arguments`; it has no URL. HTTP
definitions retain their URL and cannot supply command arguments. The source checks exact
Project/Agent scope, definition permissions, executable kind and executable bytes. It never
uses a shell, expands arguments, imports a global CLI configuration or installs a dependency.

Optional credential destinations are `CredentialHeader` (`Authorization` or `X-Api-Key`) for
HTTP, or `CredentialEnv` (`MCP_CONNECTOR_TOKEN` or `MCP_CONNECTOR_API_KEY`) for stdio. A definition
has no secret value and cannot alter PATH, HOME, native permissions or the execution bearer.
The header value is exact: a backend requiring a scheme receives the scheme as part of the
dedicated secret value. Redirects never receive it. A metadata definition alone activates nothing.

## Materialization and process ownership

Before native startup, selected folder manifests are validated and copied into a fresh private
execution directory. Only regular byte files are created, with exclusive writes and no links
into mutable global folders. Traversal, links represented as paths, duplicate/case-colliding
paths, file/directory collisions and invalid base64 are refused. The native context contains
the selected SKILL.md reference and guidance boundary, rather than a prompt containing every asset.
Project instructions and independently configured native tools remain independent.

All folders together permit 256 files, including SKILL.md, and reuse the 512 KiB active-skill
byte budget. Each manifest also retains the existing 64 KiB Squad body validation. Fixture
measurement is two materialized files (SKILL.md plus a six-byte binary canary). Empty files
count toward the file ceiling. Source capacity reports actual directory occupancy.

An execution with folder assets reserves one durable slot; each stdio child reserves another.
There are four such reservations per source state, including unknown cleanup. A full namespace
refuses before starting another process. The reservation and owner record are synced before
native effects. Unknown/stale directories are quarantined and never automatically deleted,
reused or interpreted as permission to kill a PID after restart.

Selected connectors use the existing execution-owned loopback MCP listener and native bearer.
The native definition includes only its name and local capability URL. Stdio uses the existing
owned Process census, exact PID/start identities, bounded frame/notification/write/RPC policies
and shutdown receipts. MCP initialize, initialized, tools/list and tools/call cross a real newline
JSON-RPC process. There is one serialized exchange per connector; admission is rechecked after
waiting for that gate and before writing. Partial write, timeout, invalid reply or EOF poisons
that connection. No effect is retried and no replacement process silently starts.

Every call and reply publication rechecks selection, definition fingerprint, source authority
and captured credential revision. Revocation after an effect returns unknown outcome and
withholds the response. A successful shutdown joins native, MCP effects and connector cleanup
before removing owned assets. An ambiguous receipt retains the files/process reservation and
credential hold. It is not proof of a running process or permission to repeat its last effect.

## Dedicated credential handoff

`POST .../connector-credential` accepts prepare, status, remove, or an explicit prior-operation
lookup. `POST .../connector-secret` alone accepts a value. Both require the current source,
write authority, full Bot execution fence, version and selected connector reference. Targets
also bind account/machine, Project, Bot, Agent, source epoch, definition fingerprint and expiry.
Restart and feature suspension discard pending targets. The UI saves the selection first and
then offers the dedicated field; it clears that field before awaiting submission.

Credentials use a separate protected-file AES-GCM namespace and key, with purpose/source/identity
authenticated data. This is not an OS keychain. Lost or unreadable initialized keys never mint
replacement keys. Normal settings, events, export, model context, URLs, argv, browser storage
and request digests do not carry the value, length or content fingerprint. The UI retains only
the nonsensitive original command for explicit recovery. Replaying that operation with a
different or empty value acknowledges the original receipt without comparison or replacement.

The execution captures a credential revision on first use and durably acquires a hold before
receiving it. Re-keying revokes the old revision. Known cleanup releases only that exact hold;
unknown cleanup quarantines only that revision, never a replacement. Reopening a source with
unreleased captures marks matching ready credentials unknown and keeps the holds. Removed or
unknown encrypted bytes are retained instead of being treated as safe deletion/replay authority.
The 100-record source ceiling counts retained removed records too; targets and holds each reuse
the registered 128-row ceiling. Capacity readings query real SQLite rows and live targets.

## Evidence and limits

The pinned CodyNC comparison is commit `9f67eb09084db61b3cca9c84a11bc03df9de29b7`:
`market/skills.rs::skills_brief`, `market/mod.rs::Connector::acp`,
`agent/bot/session.rs::mcp_servers`, `mcp.rs::serve_local`, and `market/vault.rs`.
Its retry and OS/Cloud keeper choices are not copied into this protected-file implementation.

Rerunnable proof lives in `TestFolderAndStdioExtensionsCrossStoredHTTPAndNativeACP`,
`TestSelectedStdioConnectorUsesOwnedProcessAndStopsBeforeRevokedCall`, the other `mcp_stdio_test.go` cases,
`TestConnectorCredentialHTTPNativeIsolationAndFixedOperationReplay`, the vault package tests,
and `TestConnectorCredentialsBrowser`. They use owned installed definitions, real local native
ACP and MCP processes, authenticated HTTP, Host and SQLite. The browser entry uses the production
Bot settings and SourceClient at English desktop and Taiwan Chinese narrow widths; snapshots
are captured before entering fixture secrets, and request artifacts redact submissions.
No vendor/model or paid content API request is used.

Production Cloud still requires an API-83 source and console deployment plus its actual encrypted
round trip. The Relay transports encrypted envelopes; there is no private Relay Bot operation
vocabulary to mirror. Cross-computer and Cloud-assistant reachability is deliberately unchanged.
