# What Clawdline changes

The public **What Clawdline changes** page lives at `https://clawdline.com/docs/transparency/`. It needs no Cloud account or paired machine. In the Console the entry is shown only to an administrator: it sits in the sidebar's Administrator section, which appears when Cloud grants operations access, and an old `#page=transparency` link forwards its supported filters and interface language only for that administrator; anyone else lands on Sessions. Settings → Workflow no longer links there. This hides a Console entry; it is not access control. The website page above is still public and is unchanged, so anyone with the address can read it.

The page describes a reviewed product revision, not a person's configuration. English and Taiwan Traditional Chinese share the website's language control. Original excerpts stay in their source language. Source links point to the imported product commit; the website build refuses an outdated content snapshot.

## Content ownership

The Session composer's `/resume` shortcut opens the existing resume picker and
does not send the command or inject a prompt into the current assistant. The
picker retains the existing optional role selection and resume launcher. The
English and Taiwan Traditional Chinese `claude-role`, `codex-role` and
`role-skills` explanations still describe those choices and their effects;
their source locations and review receipt follow the changed picker source.

`web/console/src/pages/transparency/catalog.json` owns stable entry IDs, filters, runtime locations, original excerpts, source locations and review receipts. Human copy lives under `transparency.*` in both `public/catalogs/en.json` and `zh-Hant.json`. The website imports this inventory, both language copies and filter logic at an exact public product commit using its `scripts/sync-transparency.mjs`. Its build compares the imported content with current product main. No instruction excerpts are shipped in the Console bundle.

Separate loading from applicability. In normal Clawdline-launched Codex Sessions, Board language and the Note rule are startup defaults; language falls through the configured language, machine, bundled language and `zh-Hant`. Claude's language and existing Project memory are automatic startup decisions. Roles, Skills, Board assignments, todos, notices and handoffs have their own triggers. Board assignment and reminder messages also carry the current Agent-language preference for Board text and person-facing task updates, subject to explicit Project/conversation rules and provider language choices. New Root briefing prose captures that preference until its startup message is delivered; English and Taiwan Traditional Chinese are translated, with English prose fallback for other locales. Reading a completion rule in a guide does not execute the completion action or reinject the guide every turn.

The macOS public CLI now delegates `serve` to the separately named daemon before
opening state. This changes executable identity, not provider instructions or
native-hook consent. The English and Traditional Chinese Antigravity original
launch context explanations were reviewed against the same launch-store and
managed-hook calls; their source link follows the moved startup location.

## Updating an instruction or entry

1. Change the implementation and update the matching entry, original excerpt, runtime path and source location. Add an entry for a new behavior; do not squeeze a new trigger into an unrelated existing card.
2. Review both English and Traditional Chinese, including loading behavior, agent, applicability and version scope. Existing user configuration and provider-native rules must not be described as Clawdline-installed defaults.
3. Run `node tools/refresh-transparency.mjs` only after this review. It refreshes fingerprints of the registered producers, guides and both language copies; it does not write or translate explanations.
4. Run `tools/check.sh`. Its unconditional `internal/config` repository guard refuses stale sources, missing source registrations, empty or missing card translations, and unreviewed copy changes. The Go AST scan also discovers new producers using known provider injection flags anywhere under `internal/` and `cmd/`.
5. Run the filter tests and review desktop/mobile layout for a user-facing change. Publish the reviewed product change, run the website sync command, and deploy clawdline.com. Cloud entry changes separately follow [hosted-console.md](hosted-console.md).

The inventory explicitly registers task-message composers, role/memory loaders and guide sources. The provider-sink scan catches the known `developer_instructions` and `--append-system-prompt` mechanisms, not arbitrary future protocols. A new transport or injection mechanism must register its producer and extend that discovery rule in the same change. A fingerprint requires human review; it does not prove a translation's meaning or automatically maintain prose.

To prove the freshness guard rejects drift, change one registered source in an isolated worktree without refreshing the receipt, run the focused repository guard, and retain its failing result. Restore the task-owned experiment before the passing commit gate. A new injection mechanism should also have a case proving its producer cannot remain unregistered.

This is a descriptive page. New permission switches, configuration scans, Cloud API changes and provider policy changes are separate work.


The Antigravity path has a conditional inventory entry. Managed native hooks
must authenticate a private prepared launch, recheck the native process,
terminal and kernel workspace, and retain a positive conversation binding before
preparing original context. Unmanaged observations carry no Clawdline context.
The response receipt is PREPARED, not proof of provider loading or a role badge.
The guard discovers both native `injectSteps` strings and calls through
`PreInvocationResponse`; registering only the original Codex/Claude flags would
miss this transport. The public filter preserves the explicit Antigravity
provider while stripping Session and machine identifiers from documentation
links. Website publication still requires importing the reviewed product tree.

Managed Antigravity completion hooks can verify the already prepared response
against a later native system row and retain its exact-prefix proof. They do
not inject another response or synthesize a skill-applied receipt. PostInvocation
and Stop therefore do not require another model call to validate the first
response. The explanation distinguishes PREPARED from loaded evidence in both
languages; live role badges and public capabilities remain independently gated.

Policy-bearing Antigravity context responses additionally require current
explicit Project consent and the exact live runtime, Project-manager and
launch-store owners. Stopping or replacing an owner invalidates that path;
the check does not install hooks or infer consent from an old command receipt.
The English and Taiwan Traditional Chinese card explanations and source links
include this lifecycle boundary. This is conditional enforcement for prepared
policy-bearing launches, not a claim that native compatibility rows or public
HTTP/broker starts are enabled. Publication of the revised website snapshot
still follows the reviewed product commit reaching public main.


The hook refusal transport now half-closes its reply and drains no more than the
existing frame limit under the existing request deadline. This prevents unread
request bytes from hiding a typed refusal as a disconnect. Review confirmed that
this changes neither context preparation nor loaded proof, Project consent or
native tool permissions; the English and Taiwan Traditional Chinese card copy
therefore remains accurate. The source link and receipt track the reviewed
implementation after this transport-only change.


Owned Gemini ACP conversations have a separate conditional entry from managed
native hooks. The daemon captures the existing Squad snapshot, Project memory
and language, then includes an inline resource only with the first real user
input. The native replay can flatten that resource into user text; only the
exact saved first-turn expansion is normalized. Unknown text is retained.
The existing actor capability stays in its private file and is never prompt
content. Resume preserves the original snapshot without another injection or
automatic prompt resend. The provider-sink guard and refresh tool also discover
ACP `session/prompt` producers; the synthetic inventory test includes that case.
This product inventory still requires an exact website snapshot update after
landing, and does not claim deployed mobile or native role-dispatch acceptance.


Owned ACP tools now receive a private conversation-address file in addition to
the existing, separate actor capability path. Shared CLI commands read that
exact address rather than a parent assistant's environment. This changes root
identity resolution, not authorization or the existing Gemini worker refusal.
The report entry's original excerpt and both guide languages include the new
identity source; the conditional context explanation remains unchanged.


The owned ACP client now separates model/permission lifetime from short control
RPC and pipe-write timeouts. Review confirmed that the first-message embedded
resource, original text, role identity, snapshot and permission choices remain
unchanged. Both English and Taiwan Traditional Chinese context-card explanations
remain accurate; only their original code excerpt, source line and receipt change.
The website snapshot must follow this reviewed product source when published.


The macOS permission-timing correction defers protected Project catalog reads
until a Project is selected. Review of the Board entry in English and Taiwan
Traditional Chinese found no change to assignment text, language, injection
triggers or reminder delivery; those explanations remain accurate. The Board
source link and review receipt follow the changed catalog/resolution code in
`work_v2.go`. No instruction excerpt is added to the Console bundle.


Doctor adds a fixed local CLI version probe and a sanitized support report read.
It does not prepare provider context, run a model prompt or change instruction
injection. The new guide paragraph documents that explicit diagnostic read only.
Review of the entry-point changes and both guide languages leaves the existing
English and Taiwan Traditional Chinese injection explanations applicable.


Machine workflow selection now preserves Clawdline standard as the default with
no added workflow prompt or compulsory stages. A previous skills opt-in keeps
its Superpowers selection. The selected debugging/review content is unchanged;
Poteto adds a bundled local pstack adaptation with task routing, verifiable units
and evidence mapping. Neither option grants additional authorization. Fresh roots
seal a source/version/body/stage/tool-choice snapshot; owned children inherit it,
and resume keeps its original context. Existing unsnapshotted conversations are
not relabeled from current settings. CodeGraph remains separate, with its launch
choice frozen in the same context. Launch records do not prove model application
or benefit. The reviewed source inventory and both language explanations include
the resolver, storage, local source bodies and launch producers. The public website
snapshot must follow this product tree when this feature is approved for landing.


Gemini subscription and compatibility checks now defer to authenticated provider
eligibility and advertised capabilities instead of plan names or a pinned CLI
release. Review of the managed-hook and owned-ACP cards in English and Taiwan
Traditional Chinese found that their context, consent and first-input explanations
remain accurate. The new account quota reader sends only the fixed `/usage`
metadata command with no launch instructions, role snapshot or model generation.
The ACP excerpt and reviewed source receipts follow the changed implementation.


The CodeGraph lookup launch instruction is unchanged by explicit local index
creation. The graph viewer now has a separate, user-triggered Build index action:
it prepares a hash-pinned private parser and runs code-only extraction without a
model API. The machine switch alone still installs or builds nothing. Review of
the original launch excerpt and both language cards distinguishes these triggers;
the existing cooperative per-query check now shares the larger bounded metadata
read budget used by native project graphs. The website snapshot must retain the
latest workflow-profile content when importing this reviewed product revision.

The Gemini normal-use audit removes the pilot's two-process count cap and admits
an offered model selection before publishing a new owned row. It also connects
successful native turns to the existing opt-in naming flow, using the configured
Claude/Codex namer and scrubbed context rather than injecting another Gemini
message. Review of both language versions of the managed-hook and owned-ACP
cards confirms that first-input resources, role snapshots, language, consent
and native tool permissions remain described accurately. Naming stays off when
previously off; it does not establish skill use or loaded-context evidence.
The reviewed source receipt and excerpt follow the revised launch code.



Native prompt-suggestion forwarding adds a content-free publisher invalidation
callback to the daemon wiring in `cmd/clawdline/main.go`. Review confirmed that
this does not change managed Antigravity context preparation, policy consent,
loaded evidence, role or memory injection, or tool permissions. Both English and
Taiwan Traditional Chinese Antigravity context explanations remain accurate.
Suggestion text is read from the existing native terminal and only becomes a
user message after the person accepts and sends it. The source receipt changes;
the instruction inventory and its human copy do not gain another injection.


The Session tool library adds a conditional user-message attachment, not a
startup default. The person chooses a repository and Skill, reviews installation
scope, prepares the visible draft, then sends it through the existing composer.
The instructions ask the receiving agent to inspect existing tools, pin and read
the source, perform permitted reversible setup, verify availability and execute
the task. Neither the daemon nor the recommendation catalog installs third-party
code. Delivery, installation, loading and application remain separate claims.
The inventory registers the new producer and composer in both languages.

Tool preparation also distinguishes evaluation frameworks from Skills and web
libraries. The Harness² path prepares an isolated experiment, independent
acceptance criteria, preserved baseline artifacts and explicit model-call cost
controls. It grants no sandbox bypass, paid calls or machine workflow changes.
Both language cards and the reviewed producer excerpt include this conditional
framework path; catalog inclusion itself performs no installation or execution.


ACP lifecycle observation and opt-in private wire recording do not change the
`gemini-acp-context` prompt/resource composition, startup snapshot, replay rule
or provider permissions described in either language. The reviewed source
locations follow the added observation calls. The separate recording file and
its bounds are described in the English and Taiwan Traditional Chinese Gemini
guide; raw records are not instruction excerpts for the public transparency page.


File and folder drops now insert visible path references into the editable draft,
while pictures remain attachments. The drop itself does not send, prepare a
provider prompt, install a tool or change permissions. Review of the English and
Taiwan Traditional Chinese tool-library explanations confirms that their explicit
send condition and removable attachment description remain accurate. The source
location and receipt follow the moved composer send code. The website snapshot
must follow this reviewed product revision when it is published.


Assisted Cloud pairing tasks now include an optional structured observation in
`result.json`. Review of the child briefing in English and Taiwan Traditional
Chinese includes this existing detached-task trigger and its fixed pairing
instructions. The browser compares the observation with the authenticated
handover, requested machine and exact offer digest before closing the dialog.
The observation trusts the helper assistant's reading of its existing remote
access; it is not an independent execution audit. No new root defaults, remote
credentials or generic provider-instruction mechanism are introduced. Missing
or mismatched evidence keeps manual comparison. The public website snapshot
must follow this reviewed product revision when published.


CodeGraph replaces the retired Graphify integration. Review of both languages
covers the independent default-off setting, schema-2 consent, preserved historical
schema-1 receipts, the bounded local query wrapper, cached service startup,
watching and periodic reconciliation, explicit first-build downloads and separate
SQLite visualization. The source excerpt contains the actual new launch guidance;
source matches do not certify inferred relationships. No Gemini launch path gains
CodeGraph instructions. The earlier Gemini host/HTTP source changes were also
reviewed: terminal identity/screen/keystroke forwarding and PromptAccepted naming
callbacks do not change the saved ACP resource composition or provider permissions.
The public website snapshot must follow this reviewed product revision when it
is published; a source receipt alone is not website deployment acceptance.

The Antigravity launch store preserves the explicit step-explanation switch when
it freezes the context. Empty-context hook checks explicitly disable that switch;
a blank native-hook launch also stays empty when no choice was supplied. Both language
explanations retain the distinction between original launch choices and current
settings on resume.

CodeGraph and the managed workflow now also apply to Gemini through the same explicit machine opt-in and frozen parent snapshot. Owned ACP context includes that original workflow text, records its native conversation before first input, and retains the workflow launch ID for recovery. Native terminal context preserves the original workflow bytes. Neither receipt establishes model use; native Project/hook consent and tool permissions remain independent. The reviewed bilingual entries register both providers and transports. The source scope now includes every pinned SDK extension, its special filenames, and validated project custom mappings; offline checks and live reconciliation use the same scope.


The editable starter Playbook inventory now includes Cross-Surface Code Review
in English and Taiwan Traditional Chinese. Selection sends its localized text;
merely seeding or displaying it injects nothing. Existing authored and edited
definitions retain their exact text. A separate conditional gate-review entry
describes the shared assessment, full D integration checklist and omission rule
in the immutable verify_gate checker brief. It preserves one checker, the closed
verdict and bounded evidence protocol. Both language explanations distinguish
instructions from measured application and retain the experiment limits. The
public website snapshot must follow this reviewed product commit.


SessionInfo now uses a daemon-owned conversation preference rather than appending guidance to every message. Both language explanations describe one provision with the next ordinary human send, launch-context deduplication, unchanged slash commands, unknown outcomes and explicit re-provision after clearing context. Selecting it leaves the composer unchanged and sends no terminal input. Disabling sends one preference update; it cannot retract context or revoke tool permissions. The shared bilingual instruction document and ledger are included in the reviewed sources. Local HTTP and carried Cloud writes preserve the same conversation and revision checks. Original request bytes remain stable across retries even when the daemon adds a suffix. Neither provision receipts nor frozen workflow snapshots establish model use. The public website snapshot must follow this product commit.

## Long conversation reminders

The conditional `session-rotation` Note records the new person-triggered user
message. The reminder reads file size or current delivery/activity evidence;
rendering it sends nothing. Its localized request preserves state and asks for a
milestone handoff, receiver/Board verification and a dry-run close check. It never
force-closes the source or deletes provider history. Both English and Taiwan
Traditional Chinese explain that the old Session stays open for the person's
inspection. Session info also displays the observed conversation file size; its
existing CodeGraph instruction provision behavior is unchanged by that display.
The suggestion lives in the existing attention panel; it does not add a footer
or composer block. Dismiss sends nothing and is remembered by this browser for
the exact machine, route Session and conversation, independently of ordinary
Agent-authored Notes.


Space-saving image review is a separate machine opt-in, disabled by default.
New executions, including children of older Sessions, sample that choice while
retaining their inherited workflow. Existing conversations and resumes are not
reinjected. The optional snapshot field is omitted when false, retaining historical
canonical bytes and digests. The guidance prefers thumbnails, labeled overviews
and original-size detail crops, with direct original viewing on explicit request.
It also suggests saving long tool output to files. This changes no provider tool
permissions and never edits stored conversations. The dedicated inventory entry
and both language explanations distinguish guidance from measured savings.

The [image-review setting and local sample](image-review.md) document its scope,
compatibility checks and measured limitations.


Offline Clawdfather bindings now stop occupying the active registration while the
broker retains their delivery ledger. Live Project Roots can request cooperative
pauses directly; notices identify the actual requester, and wake/retry remain
limited to that requester or the current online Clawdfather. The English and
Taiwan Traditional Chinese coordination guide sections were reviewed together.
The machine-workspace and owned-ACP cards still describe the same instruction
loading boundaries: executable recovery uses the installed CLI's public ACP
entry point without changing the configured profile or adding model input.
Missing profile data is a setup failure. Source receipts follow these changes;
this review does not establish an authenticated Gemini conversation.

## Playbook send cards and close confirmations (2026-11-09)

Playbook launches now reuse the message card, transcript following and native
message reconciliation. Their cards retain the durable to-do ID and send through
that row's existing action route, including after a browser reload or an explicit
retry; they do not become ordinary composer sends. The original request text and
CodeGraph suffix handling on ordinary sends remain unchanged. Both language
explanations for optional CodeGraph guidance were reviewed against these paths
and remain accurate. Closing a Session summarizes Playbooks by name and scrolls
long work inside the confirmation; this adds no provider instructions. The
reviewed source receipts follow the three message-card files. Website snapshot
publication remains separate from this source review.


Official Grok Build conversations have their own conditional ACP entry and
provider filter. The first user turn carries the frozen role, shared memory,
Board language, answer-Note rule and selected workflow. Private native tool
addresses are separate from authorization and prompt content. Resume retains
that snapshot without replaying interrupted input. Gemini step-explanation,
turn-end and restart-continuation instructions are not applied to Grok. An
inactivity timeout records an uncertain outcome rather than successful work.
The English and Taiwan Traditional Chinese explanations describe these actual
provider boundaries; the website must import this reviewed product revision.

The standalone ACP 1.5.0 permission vocabulary separates edit mode from tool
approval. Maximum approval uses the native `always-proceed` profile policy with
an offered `accept-edits` execution mode; the old `yolo` token is retained for
legacy runtimes. Clawdline validates this explicit policy and native mode ACK
instead of treating ordinary edit approval as full tool permission. A lower
requested dispatch permission is refused while the native global override is
active. Review of both language versions of the owned-ACP context explanation
confirms that this changes permission negotiation, not injected instructions,
profile identity, AI-credit policy or model input. The source receipt follows
the native-policy reader. Real native startup acceptance used metadata only.

Coordination notice cards were reviewed against the unchanged pause/wake
receiver instructions in `session_pause.go` and both language versions of the
notice explanation. The wire now also carries the requester conversation ID,
reason and wake condition as inert fields for display. Cards neither run the
receipt commands nor change delivery, safe-point or lease semantics. Stored
body-only notices keep the original instructions under technical details.

## Public Gemini CLI quota and approval compatibility (2026-11-09)

Public ACP 1.5 does not implement the legacy `/usage` metadata prompt. The
fixed CLI print report now supplies quota, and unknown ACP versions refuse the
prompt-based probe. The fixed `/usage` and `/quota` send actions use this reader
without inserting model input or chat history. An explicit saved full-approval
policy now reaches public ACP through its native startup flag and an acknowledged
mode selection after load/resume. Ordinary review-mode conversations receive no
new approval override. Both `gemini-acp-context` and `gemini-restart-continue`
explanations and original excerpts were reviewed in English and Traditional
Chinese: their injected text, first-turn context, identity binding and restart
continuation semantics are unchanged. Source locations and review receipts follow
the changed files; website snapshot publication remains a separate acceptance.

An explicitly confirmed user close now stops the exact owned Gemini worker even
during a model, in-flight restore or metadata call, and cancels the staging or
quota process so a closed conversation does not reappear when restore unwinds.
The close intent is persisted before stopping;
history and native identity remain available for explicit resume. The existing
HTTP closeability version and force confirmation remain required. Both language
versions of the ACP context and restart-continuation cards were reviewed again:
closing adds no instructions and does not send a model prompt or automatically
resume a closed conversation. Their injected text and excerpts are unchanged.

## Browser recording recovery copy (2026-11-09)

The voice recovery labels added to `next-strings.ts` are browser UI text, not
provider instructions. The Session rotation request and its English and Taiwan
Traditional Chinese explanations remain unchanged. Reviewing the two language
catalogs found no change to `transparency.*` or rotation request values; only the
shared source-file receipt changes. Public website snapshot publication remains
separate from this local source review.

## Grok Session facts and missing quota (2026-11-09)

Info and its related reads now resolve a verified Session execution before the
rich row arrives. Full and summary remain distinct pinned reads. Grok model
controls accept native offers; absent usage and quota are explained without
inventing a percentage. An acknowledged native rate limit has a provider-specific
display code and does not replay the failed input or create completion.

The changed shared files also contain producers registered by the Grok context,
Gemini context and restart, CodeGraph, and Session rotation cards. Their English
and Taiwan Traditional Chinese explanations and original excerpts were reviewed
against the diff: launch instructions, applicability, permission ceilings and
continuation semantics are unchanged. The new strings are UI explanations, not
provider instructions. Refreshing the source receipt does not establish website
publication or successful provider inference.

The SessionInfo title update now publishes acknowledged facts without an
immediate readback. Review of the English and Traditional Chinese
`machine-codegraph` explanations confirms that guidance selection, provisioning,
consent and launch snapshots are unchanged. Its source receipt follows the
reviewed title-only UI change.


## Grok account quota and response diagnostics (2026-11-09)

Both Session Info channels now read Grok account quota from the same local
provider cache as the global bar. The Session footer distinguishes stale,
unknown and measured zero without an assistant-wide cross-machine overlay.
Response diagnostics retain only ID type and relation to our request sequence,
never provider IDs or message bodies. Grok display errors no longer expose the
shared Gemini adapter name, including when viewing historical entries.

Review of the registered shared source diff and both language explanations
confirmed no change to context injection, role snapshots, permissions or native
turn completion. ID mismatch still fails uncertain and never resends the input.
The source receipt tracks UI and read-path changes, not successful native
inference or website deployment.

## Grok string-ID response routing (2026-11-09)

The shared ACP reader now discards valid string-ID responses during an active
Grok prompt before metadata or completion handling. Client request IDs remain
numeric; numeric mismatches, malformed replies and control RPCs retain their
existing refusal behavior. A discarded response cannot finish or replay work.
The optional diagnostic counter retains no provider IDs or content.

Review of both English and Taiwan Traditional Chinese `grok-acp-context` and
`gemini-acp-context` explanations confirms that launch resources, frozen roles,
workflow selection, native permission choices and no-resend semantics remain
unchanged. No instruction text or applicability changed, so those bilingual
explanations remain accurate. The new routing helper is registered as reviewed
source. Native interruption origin and live-model acceptance remain unproved;
this receipt does not establish website publication or successful inference.

## Grok idle response registration (2026-11-09)

The earlier string-reply change covered active prompts but left idle responses
blocking the next caller. The transport now registers the pending numeric RPC
before its write and retires it before matching response delivery. Grok orphan
responses at idle and unrelated string-ID replies during controls or prompts
are discarded before metadata, configuration or completion handling. Active
numeric mismatches, invalid responses, matching RPC errors and Gemini routing
retain their refusal semantics. Sanitized lifetime orphan counters preserve
evidence across requests without provider ID values or content.

Both language versions of the Grok and Gemini context cards were checked
against the shared-source diff: launch resources, roles, workflow snapshots,
permission selections and no-resend rules are unchanged. Their explanations
remain accurate. The reviewed source receipt follows the transport correction;
it does not prove the native origin of an orphan, a live coding turn, installation
or website deployment.

Gemini owned ACP conversations now carry their launch context in the first
prompt's own text, wrapped in `<clawdline_instructions>`, because AGY 1.5 replays
an embedded resource as a bare link and its model was measured not to follow
instructions sent that way. With step explanations on, later prompts end with a
one-sentence `<clawdline_reminder>`. Replay restores the original input only on an
exact match of either form. Grok keeps the embedded resource. Both language
explanations of the Gemini context and workflow cards were revised to match.
