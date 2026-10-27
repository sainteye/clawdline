# Cloud help, private questions and multilingual FAQ

The hosted console's `/help` is an account service, independent of machine pairing and the Session
relay. Public readers can search published FAQs; signed-in members can submit questions and read
only their own threads. `/help/admin` is the operator inbox, `/help/admin/faq` is the editorial queue,
and `/help/admin/worker` binds the answering service. These screens, including direct links,
require operations access; ordinary members receive an access-denied view without mounting
editorial or worker controls. The private Cloud API rechecks account or
superuser authority for every request; hiding a navigation link grants no authority.

## The content boundary

Support questions are deliberately submitted **server-readable support content**. The composer
states where they are saved and asks people not to include passwords, API keys or private Session
conversations. The support API and the configured support worker can read those questions. This
new account-service lane does not upload machine Sessions, obtain content keys, change relay
cryptography, or grant a support worker access to a member's machine. Support data belongs to its
own Cloud collections, separately from the machine's SQLite store and encrypted Session traffic.

No attachment upload is included. FAQ publication creates separately authored, anonymous public
knowledge: private questions, account identifiers, job leases and editorial source associations
never appear in the public projection or static export.

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
4. The Agent reads official documentation in a dedicated read-only workspace and answers in the
   requested language, retaining public source URLs and the version consulted. Account-topic
   questions go directly to human support. Other uncertain, security-sensitive or unsupported
   questions are escalated instead of receiving a fabricated answer.
5. Cloud commits the answer and job completion together. Only this commit changes a thread to
   answered. Webhook admission, delivery and an Agent's process exit never substitute for it.
6. The member reads the same thread, can ask a follow-up and mark it resolved. A visible thread
   refreshes on focus and on a bounded interval. A read cursor acknowledges only the message the
   browser actually saw, never a newer reply that arrived during that acknowledgement. Question
lists refresh when the browser regains focus or becomes visible, without a page refresh button.

The internal storage revision changes for leases and delivery metadata. The member-facing
conversation revision changes for content and explicit conversation actions; background delivery
updates do not invalidate a person's draft. Each follow-up advances the input revision and fences
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

Push notifications and a separate attachment workflow are not enabled by this version. Replies
are delivered in the thread with unread state and refresh; notification transport is not a
prerequisite for reading an answer. A provider-backed automatic translation service is also not
configured: editors maintain separate language drafts through the same API.
