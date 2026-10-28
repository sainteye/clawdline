# Cloud community, answering service and multilingual FAQ

The hosted console's `/community/questions`, `/community/issues` and `/community/ideas` are public
account-service discussions, independent of machine pairing and the Session relay. Anyone can read;
a signed-in member can post and reply. `/community/mine` lists the current member's started or
participated discussions, including for an Admin. It never becomes an all-members inbox.
See the [member guide](user/help-center.md) for the interface and classification rules.

Questions seek usage guidance. Issues report a defect with actual and expected behavior. Ideas
propose a need or improvement. An answer is distinct from delivery: Issues and Ideas retain an
independent Admin-controlled progress record. A released update includes official release evidence
and a staff message in the same thread. The API validates evidence format and official origin;
operators still verify the deployment. Discussion alone never authorizes source changes or deployment.

The labeled `/help/admin` area remains the operator inbox, FAQ editorial queue and answering-service
configuration. Every Admin request rechecks operations access on the server. Public moderation links
do not grant authority. A staff reply is an explicit operator action; a member comment is never
presented as an official reply merely because its author has Admin access.

## The content boundary

New discussions, replies and attached images are public. The composer explains this once before
submission and warns against passwords, API keys and private Session content. The API requires
explicit public consent for new discussions and staged images. Public authors expose only a display
name and a verified GitHub avatar URL; account UUIDs, email, credentials and worker metadata are not
part of that projection. Uploaded images use separate public attachment routes and ownership checks.

Legacy `/v1/support` threads retain their original private boundary. An Admin must explicitly publish
a consenting thread before it appears in community reads; this also moves its images to the public
read projection. Unpublished threads never appear as a public fallback. The current composer offers
no private-support option. Community publication and anonymous FAQ editing are separate actions.

Submitted content is server-readable Cloud account data. This lane does not upload machine Sessions,
obtain content keys, change relay encryption or grant access to a member's machine. Its collections
are separate from local SQLite and encrypted Session traffic.

## From a question to a reply

1. The browser saves a question with an account-scoped idempotency key. Cloud atomically inserts
   the question, first message, answer job and webhook outbox in one Mongo document. An audit write
   is part of the same transaction; an audit failure rolls the submission back.
2. A bounded dispatcher binds only the operator's active, generation-pinned Schedule Webhook. It
   admits an ordinary empty-payload webhook through the existing entitlement, rate and queue gates.
   It carries no member content or arbitrary command through the webhook.
3. A dedicated Clawdline support schedule uses the scoped worker client to atomically claim one
   pending question. A worker credential grants only support claims, heartbeats and replies. It
   grants neither member Session access nor FAQ publication, account changes or deployment.
4. The Agent reads official documentation and public product source code in a dedicated read-only workspace and answers in the
   requested language, retaining public source URLs and the version consulted. Account-topic
   questions go directly to human support. Other uncertain, security-sensitive or unsupported
   questions are escalated instead of receiving a fabricated answer.
5. Cloud commits the answer and job completion together. Only this commit changes a thread to
   answered. Webhook admission, delivery and an Agent's process exit never substitute for it.
6. The member reads the same thread. Public discussion accepts replies from other signed-in members;
   only the question author or an Admin can mark a usage question resolved. An explicit Agent request
   schedules a new answer; ordinary community comments do not create new Agent jobs. Legacy private
   follow-ups retain their original rescheduling behavior. A visible thread
   refreshes on focus and on a bounded interval. A read cursor acknowledges only the message the
   browser actually saw, never a newer reply that arrived during that acknowledgement. Question
lists refresh when the browser regains focus or becomes visible, without a page refresh button.

The internal storage revision changes for leases and delivery metadata. The member-facing
conversation revision changes for content and explicit conversation actions; background delivery
updates do not invalidate a person's draft. Each explicit public Agent request, or legacy private follow-up, advances the input revision and fences
an older answer. A live claim binds the question, input revision, fencing token and lease. There
is one active support claim, a short renewable lease and a fixed maximum run deadline. A late or
superseded completion is refused. Repeating a completion with its original key returns its result
without appending another reply.

A webhook wakeup is durable in the question aggregate. Offline support does not reject member
submissions. Failed wakeups use bounded retry; an abandoned lease becomes claimable again, and
exhausted work becomes an actionable human-support item. A poisoned question cannot silently block
later work. Rotating/disabling the configured hook, revoking its machine or rotating the worker
credential fences future claims and completions.

## Language is part of the record

The existing interface language is the single user preference for FAQ content, new questions and
member follow-ups. Help adds no language control, and a legacy `?locale=` parameter cannot override
the interface. Operators edit and publish the FAQ version matching their interface language.
Stored original message tags, requested reply locales and per-language FAQ revisions remain
separate data fields; changing the interface does not translate or relabel earlier messages.
Operator replies honor the member's recorded reply locale. Keep valid BCP 47 original tags, including
regional subtags. Traditional and Simplified Chinese remain distinct. The supported content locales
match the product's nine locales; the maintained interface copy is English and Taiwan Traditional Chinese.

A FAQ has one stable article identity and separate language drafts and published revisions.
Search and article reads remain in the requested locale. Missing translations have an explicit
unavailable state, never an automatic cross-language substitution. People use the existing
interface language setting to read another language, including English.
Updating the source revision makes older translations stale and removes them from the current
public projection until reviewed again. A stale draft cannot be published. Every publication
requires privacy and evidence review against a specific revision.

## DB first, static pages when ready

Initially the public website FAQ and Cloud help page read only published FAQ DB projections.
The website uses the canonical `/faq/` entry and its existing language preference. The
Traditional Chinese document is a noindex `/_page-locales/zh-TW/faq/` resource; legacy
`/zh-TW/faq/` links redirect to the canonical entry. The content locales are `en` and `zh-Hant`.
Both render the same published records in the selected language.

An editor can publish a locale snapshot once the collection is ready. Snapshot generation uses a
publication fence and promotes one complete manifest atomically. Failure leaves the previous
snapshot available. The marketing export tool accepts only public projections, exports a chosen
language and writes an atomic local snapshot. A website build with that explicit snapshot renders
static FAQ content and local search; a build without it keeps DB-backed reads. Routine builds do
not fetch member data or make live API requests. Editing continues in DB after static publication.

The private service owns `SUPPORT_LIMITS`, migrations, deployment and the scoped worker transport.
The public daemon registers the help tab's request deadline, refresh interval and title bound in
`internal/domain/capacity`; its diagnostics explain that these limits are enforced by the browser.

## Restricted answering runtime

An operator installs a dedicated documentation and public source-code workspace and uses a trigger-only Codex schedule
with task kind `support_answer`, no repository claims and no checkout isolation. Admission rejects
ordinary dispatches, other providers and repository writes for this kind. Its launch always uses
the restricted support permission profile, regardless of the machine's general dispatch ceiling.

The workspace contains `docs/` (pinned public user guides and a `source/` product-code export with their source manifest),
`support-worker.py` (the scoped client), `private/worker-token` (mode 0600), and `state/` (mode 0700).
Only the client's state and the current task's own receipt directory are writable. Documentation, source code,
client and credential are read-only. Other machine data, repositories, Sessions, tasks and Cloud
deployment credentials are outside the filesystem grant. The network proxy allows only the
support API and loopback task receipts. A separate `.codex-runtime` home contains the operator's
ChatGPT login; model-generated commands cannot read that directory. API-key login, apps, plugins,
web search and further agents are disabled. The operator installs an empty runtime configuration
and no MCP servers, hooks or project configuration. Keep the answering workspace outside a
development checkout so it cannot inherit a project's tools or instructions.

Keep automatic answering disabled until the exact installed CLI has passed positive and negative
sandbox checks: official documentation and scoped state must work, while another task, a private
Session, a live development checkout, runtime authentication, writes to documentation and unrelated network
destinations must be denied. A prompt alone is not an isolation receipt. Then bind the webhook,
install the freshly rotated scoped token privately, enable the binding, and verify a real browser
submission through webhook delivery, local task execution, answer commit and member-visible reply.

## Verification and release boundary

The feature is implemented across the public console and private Cloud/marketing repositories.
Local HTTP and replica-set tests cover duplicate submissions, transactional rollback, isolation,
late completion, lease/revision fencing, language-specific publication and failed snapshot export.
The explicit loopback browser harness is development-only and uses isolated sample data; it does
not pair a machine or contact an inference provider.

A release still needs the independent `support-questions-v1` migration, Cloud API and hosted-console
rollout, the website rollout, and an explicitly configured restricted answering schedule. The
migration is expand-only and leaves the frozen head-one inventory unchanged. An older API ignores
these collections. Keep automatic work disabled until the restricted schedule and its credential
have been installed and validated. No paid model or translation API is provisioned or invoked by
this implementation; recurring usage-priced calls require separate approval and a cap.

Admin new-question notifications and private image attachments are supported through separate
service paths. Replies remain in the member thread with unread state and refresh; notification
transport is not a prerequisite for reading an answer. A provider-backed automatic translation service is also not
configured: editors maintain separate language drafts through the same API.

## Question images

Members write the question itself; the server derives its list title. A member can select or
paste up to five JPEG, PNG or WebP images with a question or follow-up. The browser prepares
small previews and uploads the images before the idempotent question submission. Only image
references enter the pending question body; image bytes do not enter its session-storage draft.
Members can enlarge a received thumbnail and close it with Escape or the close button.

Images stay private with the question and are never copied into FAQ drafts or static exports.
The service checks file signatures and dimensions, decodes and re-encodes to JPEG without source
metadata, and keeps bytes in separate private storage. Account ownership protects member reads;
operator reads require the existing admin authority. Worker downloads require the current worker
binding and an unexpired question lease. The client downloads only API-owned image identifiers,
verifies size and SHA-256, and keeps private task images until confirmed completion. Text and
instructions inside an image are untrusted member content, with the same restrictions as text.


## Documentation coverage and reporter identity

The restricted Agent documentation export includes the [help-center user guide](user/help-center.md)
and its Traditional Chinese counterpart. When a normal product question is escalated because the
pinned export has no evidence, check the approved public source snapshot as well as the guides.
Update the pinned export when coverage is missing. The installer includes committed product code
from `cmd`, `internal`, `web`, `api` and `shell`, plus public README, license and Go dependency files.
It exports only regular UTF-8 source files, omits project instruction files and never follows Git
symlinks or reads untracked development files. Source files are evidence to read, not programs to
execute. Every exported file has its public GitHub URL, exact revision and SHA-256 in the manifest. Keep
the dedicated schedule paused and its worker idle while replacing the read-only docs and manifest;
retain its credential, state, tool limits and ChatGPT subscription authentication. Requeue the
original question through the Admin action and verify the member-visible Agent answer. Do not
insert an operator answer and call it an AI reply.

Admin list/detail/mutation responses include the reporter's current display name and stable member
ID. Only the authenticated Admin projection obtains these fields. Member and answering-worker
responses, FAQ snapshots and push payloads remain free of reporter identity fields.

GitHub sign-in retains a validated avatar URL and refreshes it on later sign-ins. Existing accounts
can resolve an avatar directly from their already verified numeric GitHub identity, without another
OAuth grant, profile API request or rewrite of past questions. The account's authenticated session
and Admin reporter projection expose the current avatar; member question and worker projections
remain unchanged. Only the canonical HTTPS GitHub avatar CDN path is accepted. The browser omits
credentials and referrers when loading it and falls back to the local member icon on failure.
Agent replies retain the brand mark and operator replies retain the staff icon: a reporter's avatar
does not establish which staff member wrote a reply. Public FAQ exports remain anonymous.

## Evidence routing and request types

Support submissions distinguish usage questions, issue reports and feature requests independently
from product topic. Existing records default to usage questions. Classification does not grant
implementation authority, create a Board item or mean a feature has shipped. Admins can correct a
classification with revision and idempotency checks.

The answering Agent reads the [product evidence map](product-evidence.md) before choosing which UI
source to inspect. A missing control on the general Session start sheet does not establish that
the Board assignment flow lacks it. Verify the actual entry point, relevant request path and
pinned revision before claiming absence; distinguish role recommendation from model selection.
