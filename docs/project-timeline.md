# Project Timeline

Project Timeline is a mobile-first history view within each Project, next to Board.
It distinguishes work recorded in Git from a deployment and from verified availability.
A commit, build, successful task, or narrative summary never means a feature is live.

## Reading a Timeline

Open **Projects → a Project → Board → Timeline**. The default view is production.
Use the environment and category filters to narrow the history; enable upcoming/Git
history to include entries that have not established production availability.
Load more appends entries without replacing the earlier page.

Entries show a title, category, time, availability status, source revisions and Board
relations. Open an entry for its target counts, GitHub commit links and source evidence.
A missing GitHub remote does not make a local commit unreadable; it simply has no
external commit link. Board relations use stable item IDs, not title matching.

Status distinctions:

- **In Git**: a commit or broker-verified target-branch landing exists; deployment is unknown.
- **Deployed · awaiting availability**: deployment succeeded, but matching readback is missing.
- **Available**: every required target in the selected environment has matching deployment
  and availability evidence for the same revision, with verification no earlier than deployment.
- **Limited rollout / Partially available**: a restricted audience or an incomplete set of targets.
- **Deployment failed / Rolled back / Partially rolled back**: retained operational evidence,
  not a deletion of the earlier successful history.
- **Applied · awaiting verification**: an operation without a commit, such as DNS work,
  awaits verification matching the latest producer, target, before/after digests and time.

Preview or staging evidence cannot turn production green. A new deployment cannot
reuse an older revision's health receipt. An operation cannot reuse an unrelated
verification receipt. Effective time and ingestion observation time are separate fields.

## Data ownership and performance

The personal Mac version keeps one versioned authority at
`~/.config/clawdline/project-timeline.json`. Schema version 1 bounds entries (2,000),
events (20,000), request receipts (4,096), and the complete file (8 MiB).

Entries have stable delivery identities. Events are immutable under
`(producer, sourceID)`: identical replay is deduplicated, while different content
under the same identity is refused. This check also applies within one batch;
a conflicting batch is rejected without partial persistence.

GET reads a cached materialized projection with bounded pagination. It does not run
Git, read transcripts or raw logs, or invoke AI. Background refresh is single-flight
with at most one consolidated successor. Timeline reads and commands have separate
bounded workers, so they do not occupy the Session interaction lane.
Environment projection and deterministic ordering happen before pagination.
A selected detail must belong to the requested Project.

The separate Timeline setting defaults on. Turning it off preserves history and
refuses new ingestion. Board and Timeline switches do not change each other's modes.
External AI is not required or enabled by this feature.

## Sources and trust

The initial automatic sources are deliberately conservative:

1. Broker-verified target-branch landing creates `landed_to_git` and retains its
   stable Board item relation. It does not create a deployment receipt.
2. Startup imports one read-only first-parent Git page for at most eight Start Points,
   with at most forty commits per canonical repository (nested or linked-worktree
   Start Points share one slot). A low-priority round-robin worker then
   resumes one repository per minute, at most forty commits plus one look-ahead commit
   per page. It never fetches Git remotes. Re-reading an existing identical commit
   counts as covered, not as an omission. GitHub remotes are normalized to
   `github:owner/repo`; other remotes keep local history without invented URLs.

Deployment and availability evidence must be supplied by a trusted local producer.
Automatic GitStory integration is deferred until its API, owner, authorization and
retention contract are defined. No private Git data is sent to an external AI service.

### Older history and coverage

Project-scoped checkpoints pin the repository identity, observed head, last covered
revision and next unprocessed revision. Each scan resolves a full commit SHA once and
passes that exact SHA to Git; a concurrent checkout cannot substitute another chain.
Entry persistence precedes checkpoint persistence:
an interruption can replay an identical entry but cannot skip it. The first rejected entry
stops a page; capacity never evicts retained history. A missing object, changed repository
identity, malformed read or failed checkpoint write is a named gap, not an empty success.
Legacy checkpoints without Project/cursor metadata start a bounded scan using the existing
idempotent entry identities. A completed scan can observe a newer HEAD on a later round;
an unfinished scan stays on its pinned older chain. Shallow clones explicitly retain unknown
older coverage. Git commits never establish deployment or availability.

The header separates pending older history, complete observed first-parent coverage, shallow
history, capacity and unavailable sources. Projects outside the bounded startup set show unknown
coverage, not zero remaining commits. Storage counts/limits describe the whole Mac Timeline,
not the number of matching cards or a count of unimported Git commits. No total Git-history count
is inferred. An unscoped response never claims global complete coverage from only the
checkpointed subset; per-Project complete and unknown remain distinct.
Timeline OFF pauses background ingestion; GET, Refresh and the Git filter only
read the materialized view and never start an import. Checkpoints survive restart. A capacity
stop remains paused in the background; no automatic deletion or retention expansion is performed.

## API and browser contract

Local and paired Cloud clients share `/v1/timeline`. Cloud requests use the encrypted
machine envelope: read type `timeline`, command type `timeline-command`.
An explicit machine identity must be preserved; an unavailable machine is refused,
never silently replaced with another Mac. Credentials never belong in URLs.

GET accepts only `project`, `entry`, `cursor`, `environment`, `category` and
`upcoming`. Repeated or unknown query fields are refused. Replies expose cached
model revision, current store revision, observation time and stale/error state.

POST uses `operation`, `requestId` and `expectedRevision`:

- `set_enabled` adds a Boolean `enabled` and requires administrative write authority.
- `ingest` adds `producer`, `sourceVersion`, `entry` and `events`, and requires
  the machine-authenticated local producer door. A paired remote viewer cannot ingest evidence.

The entry schema includes project/delivery identity, category, title, summary, required
targets, Board item IDs, relations and repository revisions. Events include their
producer/source identity, kind, target, effective/observed times, authority, revision,
result and optional operation digests/verification. The implementation's Codable types
and closed admission schema define the exact wire fields.

The browser module exports `bindTimelinePage(elements, environment)`, returning
`{enter, leave, refresh, escape, state}`. Deterministic command refusals clear the
pending request and refresh the current revision. Uncertain transport failures retain
the same request identity for safe retry. Timeline responses must never be applied to
Board mode controls.

## Verification and release

Coverage includes Git-only evidence, deployment without readback, matching production
readback, environment isolation, partial/limited availability, rollback/redeploy,
multi-repository revisions, operation identity, immutable replay and batch conflict,
Git reload, Project-scoped detail, pagination, mode isolation, bounded stale reads,
Cloud authority, and the broker-to-persistence-to-read chain.

A source commit or green test run is not a runtime deployment. Release owners must
record the deployed revision and availability evidence separately. Until rollout is
observed, the feature's delivery receipt must say that it is not yet live.
