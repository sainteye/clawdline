# Local Antigravity Project hook consent

This page covers local consent storage and its private filesystem coordinator.
No public HTTP endpoint, consent screen or production launch permission is
enabled by this work. The provider is fixed to Antigravity. State lives in the
private local SQLite store, never in repository files, shared Project Unify
settings, a Cloud mirror, or native workspace trust.

## Caller contract

`Store.AntigravityProjectConsent(ctx, root)` reads the current row. A missing row
returns phase `absent`, revision 0, and no error. A failed or invalid read never
returns an absent success. Only a current `active` row with the expected helper
image and managed-hook plan SHA256 digests is a candidate grant. The caller must
also independently check each workspace's effective hook-file digest before use.
A response replayed from a command receipt is historical evidence, **never an
authority check**.

`Store.ApplyAntigravityProjectConsent(ctx, AntigravityConsentCommand)` accepts a
canonical local Project root, action, expected revision, operation identity,
helper and plan digests, actor, and request key. The caller must prove catalog
identity and worktree membership. Storage validates only a clean absolute path;
it neither accesses the filesystem nor resolves symlinks. The future consent UI
must explicitly describe whether it includes worktrees and which ones. Creating
a worktree never creates a grant. An arbitrary actor label is structural data,
not authenticated person or device identity; the HTTP boundary must enforce
actual authentication and explicit opt-in.

1. `prepare`: commit `preparing` with a stable operation identity and both
   candidate digests **before** installing anything. This immediately supersedes
   any older active grant. A preparation surviving a crash grants no authority.
2. `activate`: after verifying installation, submit the preparation's operation,
   revision and exact digests with a separate request key. Only that preparation
   can become `active`.
3. `disable`: commit `disabled` **before** attempting file removal. Digests must
   be empty. A failed cleanup cannot restore authority. Disabling an absent row
   creates a disabled tombstone. Repeated disable commands with fresh keys and
   the current revision advance the revision; an exact retry replays its result.

Each successful transition advances revision, including activation and disabling
an already disabled Project. Revisions never wrap or reset. There is no delete
API or automatic tombstone eviction. A SQLite delete trigger also refuses row
deletion so an unrelated cleanup cannot reset a Project's revision. Concurrent activation and revocation with
the same expected revision have one winner; a losing revocation must be retried
with a new key and freshly read revision before any cleanup is treated as safe.
There is no automatic retry inside storage.

## Atomicity, recovery and errors

The existing `request_receipts` table and `completeReceipt` primitive hold the
answer in the same `BEGIN IMMEDIATE` transaction as the phase change. The receipt
namespace is `antigravity.project.consent.v1`; actor and request key identify the
request. Its SHA256 digest covers the entire command, including root, action,
expected revision, operation, both image/plan digests, actor and key. A duplicate
returns the exact original state or admitted refusal without executing again;
changed input with the same actor/key conflicts. Another actor has a different
receipt identity. Admission never commits a pending receipt separately, so a
crash cannot strand a half-admitted storage operation. The durable `preparing`
row is the filesystem preparation marker, not a pending receipt.

An acknowledged or lost-acknowledgment command is recovered by replaying the
same command/key or by reading status. A write or receipt-completion failure
rolls back both state and admission, so the identical request can be retried.
Old preparation or activation receipts replay after revocation but never modify
the disabled row. A new delayed activation fails revision, operation or phase
checks. After restart a pending preparation remains preparing; it can complete
only if its original revision, operation and digests are still current.

Receipts use the store's existing 24-hour window and 4,096 live requests per
scope. Expired receipts refuse replay; standard receipt maintenance may later
prune random-key tombstones after seven days. Even after that horizon an old
command cannot pass the monotonically increasing Project revision. Expiry never
revives authorization. Receipt capacity refuses a new request, including disable;
it does not remove an existing receipt or report a revocation as successful.

All feature errors are path-free `AntigravityConsentError` values with stable
`antigravity_consent_` codes: `invalid`, `version_conflict`, `phase_conflict`,
`digest_conflict`, `operation_conflict`, `key_conflict`, `receipt_expired`, `busy`,
`unreadable`, `write_failed`, and `capacity`. Busy/cancellation writes nothing;
read failures remain unreadable. Storage errors never expose driver text or
Project paths through this API. A returned error is never proof of an active grant or successful revocation.
Recover an uncertain write outcome through the same operation/key or a fresh
current-state read before any dependent filesystem effect; do not assume a
failed acknowledgment means nothing committed. Admitted semantic refusals have
replayable receipts.

## Bounds, measurement and compatibility

- `store.antigravity_project_consents`: 1,024 Project rows, including preparing,
  active and disabled records. Full refuses a new Project; an existing Project
  can still disable, subject to ordinary receipt admission. There is no eviction.
- `store.antigravity_consent_root_bytes`: 4,096 bytes per clean absolute root.
- `store.antigravity_consent_identity_bytes`: 128 bytes each for actor, request
  key and operation identity, restricted to ASCII letters, digits, `.`, `_`, `-`.
- Image and plan digests use exactly 64 lowercase hexadecimal characters.

`Store.AntigravityProjectConsentCount` counts actual SQLite rows, including
disabled tombstones. The HTTP store capacity owner reports unknown on missing or
unreadable storage instead of a diagnostic zero. The register and measurement
snapshots and generated English/Traditional Chinese capacity guides include all
three new rows; their console explanations ship in both languages.

Opening an older database adds one table with `CREATE TABLE IF NOT EXISTS`;
opening it again is idempotent. No legacy setting is imported as consent. Foreign
settings and receipts remain intact. Older binaries ignore the added table;
rollback preserves these rows but cannot enforce or use the new feature. No
irreversible migration, filesystem operation or new background worker is added.

## Managed filesystem primitives

The native adapter now provides `PreviewInstallForState` and
`InspectInstallForState` for a caller-selected canonical private config root.
Each managed command explicitly selects that root through a safely quoted
`CLAWDLINE_NEXT_DIR` environment assignment before invoking the helper. This is
per-command routing; it does not change global preferences or account files.
`ApplyInstall` regenerates the proposal with that same root before replacing
bytes. Legacy default-root proposals remain available to existing private
fixtures, but cannot pass explicit-root installation inspection.

Inspection reads the exact six managed events, refuses absent, partial,
disabled, conflicting or unknown owned fields, and returns both the named plan
digest and the current whole-file digest. Unowned top-level blocks are preserved
and contribute to the whole-file digest; changing one invalidates a launch's old
file snapshot without changing the named helper/root plan. An installed block
is not explicit consent or proof of effective native permissions.

`PinnedExecutable` shares the existing bridge's one-time SHA256 and kernel
file/change-stamp checks with future helper ownership. Replacement, in-place
rewrites with restored mtime, mode changes, removal and cancellation refuse.
Subsequent fingerprint reads use metadata, not repeated full executable reads.
These primitives start no vendor process or worker and publish no production
policy. Their private transactional coordinator is described below. Authenticated
UI, concrete catalog composition and effective-mode proof remain required before
production launch admission can be enabled.

## Private transactional coordinator

`internal/adapters/antigravityproject.Manager` composes the existing store and
native filesystem adapter, with no worker, queue, map or new resource limit.
Its constructor receives one immutable helper pin and one trusted catalog
resolver. The resolver must prove canonical Project and linked-worktree
membership; a path prefix is insufficient. The manager authenticates no person.
A future HTTP owner must enforce explicit person/device consent and recover its
own durable operation identity. Its `Plan` is a private preview and must never
be accepted as caller-chosen roots, workspace membership or helper paths.

The fixed scope is `project-and-linked-worktrees`. The consent screen must
explicitly describe that scope before enabling it. An active Project grant can
cover verified linked worktrees without implicitly trusting them in the vendor.
`EnsureWorkspace` installs and independently checks that workspace's managed
block under the current grant; it does not create a new grant or advance the
Project revision, so preparing one worktree does not invalidate existing
Sessions. A newly created worktree is not ready until its own block is verified.
Another Project, or an unverified path, cannot borrow the grant. This contract
does not enable automatic installation in any current public launch path.

`Enable` binds the whole approved plan, including the prior file digest, scope
and caller-owned operation identity, into the SQLite operation digest. Stable
phase keys use the existing receipt table; enable and disable share the initial
key namespace so changing the action under the same key conflicts. Prepare
commits before filesystem installation; activation requires the exact current
preparation, helper pin, managed block and catalog binding. A changed preview
is refused before a new preparation. A crash or uncertain installation answer
leaves preparing without authority. The same operation can recover by inspecting
an already installed block rather than writing it again. A lost activation
answer recovers as a status read, not another activation.

`Disable` commits disabled before inspecting or removing files. A missing or
replaced helper cannot block revocation; a manager constructed with a nil helper
pin supports disable while refusing preview, enable, workspace preparation and
live policy checks. Cleanup errors return disabled with hooks `unknown`, not a
successful removal receipt. `InspectRemovedForState` positively verifies removal.
Retries can recognize completed cleanup without another write. A historical
disable cannot remove the files of a newer grant, and a historical enable cannot
restore a revoked grant. Cleanup covers the selected workspace; other covered
workspaces immediately lose authority through the shared durable revision even
when their inactive hook files remain for later cleanup.

A revocation racing filesystem installation can leave inactive hook residue,
but activation CAS refuses and current policy stays disabled. There is no
rollback that re-enables a grant and no automatic model or filesystem retry.
`Check` validates current active state, exact helper image, named plan, current
whole-file digest and catalog membership, then rereads durable consent after
its dependencies. Its revision digest binds the Project and current consent;
the separate file digest binds each workspace. Neither digest proves native
permission semantics, role loading or a tool decision. No public production
policy publisher is enabled by this coordinator.

## Catalog membership and lifecycle

The server now owns one private consent manager and immutable helper pin for
its boot. Stopping or cancelling that owner makes its captured resolver refuse;
a new boot cannot be borrowed by a stale owner. Startup publishes neither a
native behavior row nor production permission inputs. This provides the owner
for the forthcoming authenticated Project settings route, without enabling a
public grant endpoint in this change. Missing helper bytes leave revocation
available while preview and live authority remain unavailable.

The resolver requires the current Project catalog and
`projects.VerifyProjectWorkspace`. A registered Project root is exact; a nested
Git directory must belong to its nearest checkout. Linked worktrees require
regular bounded metadata files, the exact Project `.git/worktrees` registration,
its backlink to that worktree, its common-directory link and an existing HEAD.
A copied `.git` pointer alone, another nested repository, a symlinked marker,
missing metadata or a mismatched backlink cannot borrow the Project grant.
This observes local membership; it is not native trust or a Git integrity audit.
No Git command, repository hook/config, account or network is read by the verifier.

The pre-existing catalog identity lookup now uses the same regular-file bounded
reader for Git pointer/common-directory files, so lookup does not perform an
unbounded preliminary read before verification. Each metadata file is bounded
by `projects.workspace_metadata_bytes` (4,096 bytes); that input guard has its
registered measurement, both snapshots and English/Taiwan Traditional Chinese
capacity explanations. The capacity guard was observed rejecting the new
unregistered declaration before its registration was added.

## Behavioral verification

`TestAntigravityConsent*` runs real SQLite transactions, trigger-injected write
failures, two handles, reopen, exact cap and CAS races. Transport tests check a
real disabled-row count and unreadable/missing-store readings. Artifacts retained
by the task contain guard-red and behavioral mutation evidence; they are not
part of the public repository. `TestConsent*` in the coordinator additionally
covers actual file effects, lost storage/file acknowledgments, reopen, stale
replays, revocation during installation, helper loss, cleanup uncertainty,
linked-worktree revision stability and cross-Project refusal. A one-time source
counterfactual removes the final live read: the targeted test fails after the
revocation actually commits. The original sources remain untouched by that
experiment. Root still owns authenticated HTTP/catalog composition, UI and
native effective-mode acceptance and any release.
