# What Clawdline changes

The public **What Clawdline changes** page lives at `https://clawdline.com/docs/transparency/`. It needs no Cloud account or paired machine. In the Console the entry is shown only to an administrator: it sits in the sidebar's Administrator section, which appears when Cloud grants operations access, and an old `#page=transparency` link forwards its supported filters and interface language only for that administrator; anyone else lands on Sessions. Settings → Workflow no longer links there. This hides a Console entry; it is not access control. The website page above is still public and is unchanged, so anyone with the address can read it.

The page describes a reviewed product revision, not a person's configuration. English and Taiwan Traditional Chinese share the website's language control. Original excerpts stay in their source language. Source links point to the imported product commit; the website build refuses an outdated content snapshot.

The rounded brand update changes only the image shown beside a voice-assistant
reply in `RealtimeCopilotModal.tsx`. Review of the registered diff and the
English and Taiwan Traditional Chinese `voice-work` explanations confirms that
model context, tools, consent and execution behavior are unchanged. Only that
source receipt is refreshed; this review does not deploy the public website.

Person edits of Bot settings, memory notes, routines, lead setup, login
profiles and connector credentials no longer fail merely because chat
activity advanced a version: each now compares only what the person edited.
Reviewed the cards that cite those sources in English and Taiwan Traditional
Chinese: model context, tools, approvals and execution are unchanged. Only
those source receipts are refreshed; this review does not deploy the public
website.

A Session report now shows its first sentence before it is opened, and the
Bots list says when its preview comes from one. Reviewed the persistent-bot-lead
and persistent-bot-review cards in English and Taiwan Traditional Chinese: the
roster projection only adds the latest message's kind, and the chat change only
affects the closed Session report card. Model context, tools, approvals and
execution are unchanged. Only those source receipts are refreshed; this review
does not deploy the public website.

Work a Bot's private thread worker dispatches at another Bot's request now
belongs to that Bot's main chat: the Host writes a finished host-notice turn
there ("Dispatched task … at …'s request." in English and Taiwan Traditional
Chinese) and the broker records it as the task's owner, so the completion
review turn runs in the main chat. Reviewed the persistent-bot-review card in
both languages: the notice turn never runs a prompt, and the fixed Bot frame,
the peer-request prompt, the work-completion prompt and the tool descriptions
are unchanged. Only the actor, provider, provider message and runtime source
receipts are refreshed; this review does not deploy the public website.

A Bot answering another Bot's request on this machine can now call
`dispatch_work` into its own Project and use browser tabs it opened in that
turn. Reviewed the persistent-bot-review card in English and Taiwan Traditional
Chinese: the peer-request prompt, the fixed Bot frame and the tool
descriptions are unchanged, and none of them said a request turn cannot
dispatch or browse. Another Project, Board items, completion callbacks and
requests from another machine stay refused at the Host. Only the provider
source receipt is refreshed; this review does not deploy the public website.

A Bot's reply to another Bot's request, and a decision answer forwarded to the
requester, now carry all the text after the turn's last tool call instead of
only its last text segment. Reviewed the persistent-bot-review card in English
and Taiwan Traditional Chinese: its prompts and tools are unchanged, and the
reply stays data inside the existing correlated-reply frame under the same
reply limit. The chat's fallback reply is still the last segment. Only the
ACP client, compaction and runtime source receipts are refreshed; this review
does not deploy the public website.

Bots are now told when to use `ask_person`. Reviewed the persistent-bot-review
card in English and Taiwan Traditional Chinese against the fixed Bot frame,
the peer-request and correlated-reply prompts and the `message_bot`
description. The new instruction limits cards to permission, authorization,
a choice between options, or irreversible, paid or data-changing steps; it
forbids relaying authorization text and states that a card answer authorizes
only the question and reply's scope. Claude Code's allowed owned tools gain
`ask_person`, which creates an offer and answers nothing. No tool, approval
choice or execution path is added; the real-agent behavior is not proven by
this receipt.

A truncated Bot reply now says so. The correlated-reply prompt gains one
sentence when the stored reply ends with Clawdline's truncation line: the
reply is not the complete answer, and the requester asks for the rest or tells
the person. The `persistent-bot-review` effect copy in English and Taiwan
Traditional Chinese now states the 32,000-byte reply bound, the marker and the
diagnostics count. No tool, approval choice or execution path is added.

A Bot handling another Bot's request now tells its own person. Reviewed the
persistent-bot-review card in English and Taiwan Traditional Chinese against the
peer-request prompt, the fixed Bot frame for every agent and the request turn's
`send_message` route. The recipient is told to say briefly in its own chat when
it cannot do part of the request, a tool is refused, the person must act, or it
makes a decision affecting the person; `ask_person` stays the only way to ask for
a decision. `send_message` from a request turn is delivered only to the
recipient's main chat, prefixed with the requester's name, at most 3 messages of
2,000 bytes per request; files, artifacts and network requests stay refused. The
recipient's chat shows the first 160 characters of the reply under the request's
capsule. Real-agent behavior is not proven by this receipt.

A Bot can follow up an earlier request with `reply_to_exchange_id`. Reviewed
the peer-request prompt, the correlated-reply prompt, the `message_bot`
description and both language versions of the `persistent-bot-review` effect.
When the requester names a finished exchange it sent to the same target, the
recipient's request prompt gains that round's stored request and reply, and
up to 4 linked earlier rounds within 96,000 bytes, under a bilingual header
saying they are context data, not the person's message, an authorization or a
new instruction. The text comes only from stored exchange records. Without the
parameter nothing is added. The correlated-reply prompt now names the exchange
id, and a truncated reply tells the requester to follow up with it instead of
pasting the reply back. One optional tool argument is added; no approval
choice or execution path changes.

## Content ownership

The details address menu opens or copies only literal HTTP candidates supplied
by the source. Reviewed both persistent-bots language versions for loopback
origin plus local transport, conservative remote selection on LAN/Cloud,
unknown-write inspection, clipboard refusals and the difference between an
address candidate and reachability. The section imports its shared menu styles
rather than depending on a chat reaction being mounted first. Native clipboard failures retain their DOMException category or adapter code;
they do not become a fabricated source-offline refusal or expose arbitrary
exception messages. No native tool, model instruction, approval choice or source
mutation is added. Isolated
browser evidence does not establish installed or Cloud acceptance. A socket-loss
fixture observed two identical-key requests with one applied effect; it is not
zero retransmission evidence. A separate invalid-JSON case checks one
application call with an unknown result. Original failure observations remain
preserved; opening a popup does not prove its body was read through the browser
channel, and no foreign-tab ownership is bypassed.

The source listener response binds each OS observation to the complete current
server attempt and revision before and after the read. Reviewed English and
Taiwan Traditional Chinese persistent-bots explanations for literal source
addresses, loopback-only access, unknown reads and the distinction between a
TCP listener and HTTP/reachability acceptance. No model instruction, tool,
approval choice or process mutation is added. Optional observations share the
existing native-frame budget and become wholly unknown on overflow. The
source-only response does not establish address UI/background refresh, Cloud
carriage, installed UI or physical recovery. Register the Host address mapper
and retain prior failed-gate and destination boundaries.


The optional source TCP observer adds no model instruction, native tool or
human control. Reviewed the existing English and Taiwan Chinese explanations:
installed owned-port display and physical recovery still remain unqualified. Its kernel
root pin belongs only to combined-output source commands, and a fresh rooted
identity check fences each bounded read. A full probe pool refuses without
starting another OS query; its actual count has a capacity measurement. Register
the observer source with persistent-bots, retaining the separate Host/UI/Cloud
and crash-recovery construction gaps. No website deployment follows a receipt
refresh alone.

The details server section reuses the existing source API and anchored menu;
it adds no model tool or instruction. Reviewed both language explanations for
known-ID controls, plain-text logs, incomplete tails and the inspection lock
after an uncertain write. No log-derived address, arbitrary human command,
automatic write replay or new provider permission is introduced. Desktop/mobile
component checks use a synthetic HTTP source and do not qualify Cloud carriage,
installed UI or physical process recovery.

The known-ID human server API adds no model tool or instruction. Reviewed both
language explanations against the source controls: current paired-person write
authority, displayed baseline checks, history readable to the person, unchanged
Forget restrictions for new execution, and no arbitrary command input. Register
the private human control handler with the persistent-bots inventory. Source HTTP
and client tests do not establish new-control Cloud carriage or server UI.

The source-owned development-server continuation adds four native MCP tools
through the existing endpoint, independent Host process ownership, exact local
rules and current source/Forget checks. Reviewed the persistent-bots explanation
and the new tool descriptions in English and Taiwan Traditional Chinese. The
copy names source-user files/network authority outside the agent sandbox,
complete command/directory review, no model-supplied identity or human approval,
unknown cleanup and the remaining Cloud/UI/ports/crash-recovery gaps. Native
rule scope and the related loading/memory/replay explanations retain their
existing meaning. Source rule visibility follows API 92 while older sources keep
their native rule behavior. The inventory includes the private command handler,
its native descriptor and the per-definition reconciliation boundary. A local
synthetic process test is not an installed vendor or Cloud acceptance receipt.
Website snapshot publication follows the reviewed product tree after integration;
refreshing the source/copy receipt is not a deployment.

Codex full tasks retain the workspace profile but now send requested sandbox
exceptions to native automatic review. This restores the request path needed
for an explicitly authorized browser launch; it does not add a permission
switch or remove the sandbox. The bilingual machine-workflow and CodeGraph
explanations were reviewed: their frozen instruction text, selection rules and
context-provision authority are unchanged. Only the dispatch source receipt
changes; automatic review is an execution decision, not a new workflow prompt.
The English and Taiwan Traditional Chinese tool-preflight guides also describe
this request path, replacing their obsolete `never` explanation. They still
require exercising the actual installed tool and reporting refusals, rather
than treating an approval setting as proof of browser availability.

The Bot dialogue alignment explains explicit multi-message delivery and the
last-text fallback in English and Taiwan Traditional Chinese. Its fixed prompt
now matches delivery: after any explicit message, final text remains in trace.
Native narration, supplied thoughts and per-tool status/output/diffs retain
separate step identities. Text is persisted as deltas and joined only for a
requested step revision, under the existing read budget and source authority.
The supplied Codex adapter's existing terminal-output metadata is appended
in order to the owning tool, rather than dropped. No new execution permission,
memory source or model call is added by reading these traces. The public website snapshot must follow this reviewed product tree.

The interrupted-Bot update retains allowlisted failure diagnostics through the
native reply, durable turn and bounded chat read view. Review of the persistent
Bot, input, fallback, routine and group entries in English and Taiwan Traditional
Chinese found no change to prompt text, memory selection, execution authority or
automatic replay rules. Context deadlines remain unknown effects; displaying a
cause does not prove an action was not applied. The existing bilingual
explanations remain accurate, and only the three changed implementation source
receipts are refreshed.

The Bot exchange restart fix implements the existing English and Taiwan
Traditional Chinese review card's promise to preserve replies through a
restart. During a planned shutdown the source keeps unstarted reply turns
and replies waiting for queue space. Startup restores the saved feature state
and starts recovery after write access is published. The exchange can show
the request outcome and reply delivery while the sender's unknown turn still
blocks execution. The pending-integration count follows the broker's settled
landing through a retryable projection that does not start a review. The
quoted prompts and the original request's replay policy are unchanged.

Bot restart recovery keeps eligible unstarted chat messages in their original
order. When native cleanup and the original history are verified, the source
may queue one restricted continuation ahead of them. The Bot receives a host
notice asking it to inspect the prior progress. The old execution receipt
keeps its unknown outcome even after the continuation releases the queue.
An unconfirmed Send now delivery retains its own uncertainty. A dispatched
result can stay visible while its review waits for the originating conversation
to recover. Pending review and restart status are rendered in the reader's
language, with model instructions and raw diagnostics in details.
A person can send another instruction in the same chat when restart recovery
needs review. The source acknowledges the interrupted turn in the same
transaction as the new message, keeps its original execution receipt and
processes unstarted messages in their original order.
That message runs ahead of the automatic work already waiting in the queue:
work reports and other Bots' requests and replies. It never moves ahead of
the person's earlier messages, a verified continuation, held edits or a
running turn. Its prompt first asks the Bot to inspect what the interrupted
turn finished and states how many automatic items are still queued; those
items are delivered afterwards, not dropped. A work review admitted after a
declined continuation is released the same way as work queued before it.

The person's own message is admitted behind any unknown turn, whatever its
cause, origin or count, and behind a stale active-turn pointer; only an
automatic restart continuation still queued or running keeps its source
unacknowledged ahead of the message. A running row no worker owns is recorded
unknown (`worker_lost`) and is never resent. When a verified restart declines
its continuation (too old, an unconfirmed tool or dispatch, a changed binding,
or a continuation that ended without succeeding) and messages wait behind the
interrupted turn, the source settles that turn as `restart_not_repeated` with
its execution receipt still unknown, writes a visible notice that it was not
repeated, and starts the first queued prompt with a host header asking the Bot
to inspect what finished and not repeat a write, send, dispatch or other
external action. A dispatched task's review now waits in the queue behind the
interrupted origin instead of holding the outbox; it runs only after the origin
is resumed, acknowledged or settled. These change no memory selection,
execution authority or replay rule: no earlier prompt is sent again.

The Bot network card separately describes authenticated cross-source work,
captured execution fences, untrusted peer input and the dedicated assistant
principal boundary. Its English and Taiwan Chinese copies distinguish source
construction from the private Cloud API, relay and assistant gateway that still
need integration. A saved preference is never account or assistant consent.
Its source-composition explanation now also names live machine Bot authorization,
independent socket negotiation and local synchronization of already approved
consent with a pinned signed pair. Source transport ACKs do not assert native
execution or Agent observation; production lifecycle attachment and real private
bridge acceptance remain separately pending.

Bot memory gained one source on 2026-11-24: machine-wide shared memory from Bots settings.
The rows that apply to the Bot's Project are prefixed to the Bot's memory context as
`<shared_memory>` (`internal/app/projectassistant/runtime.go`, `SharedMemory` option), so they
travel through the existing fresh-session opening, `<memory_update>` and compaction redelivery
paths; no new provider sink was added. A Bot's own "use shared memory" switch removes it. The
context card does not yet list this source separately; that card is pending.

The selected Bot extension card now covers execution-owned folder skill snapshots,
selected stdio and HTTP MCP transports, dedicated encrypted connector credentials,
per-call and publication revocation, and durable unknown-cleanup reservations.
Its English and Taiwan Chinese explanations distinguish local construction evidence
from installed provider, production Cloud and remote Bot acceptance. Independent
native Project/global configuration remains separately governed. The source receipt
includes the materializer, stdio owner, connector vault and dedicated secret input.

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
and evidence mapping. Poteto version 2 also reviews comments changed in the diff
and considers formal verification for high-impact guarantees. Neither option
grants additional authorization. Fresh roots
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
startup default. The person chooses a repository and tool, reviews setup
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


The editable starter Playbook inventory also includes Drive Board Items to Done.
Only its title is localized; selection sends its English launch message unchanged in
every interface language. Seeding or displaying it injects nothing.

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

The Gemini Host's saved model and effort choices, their reapplication after a
load, and the saved last offer change configuration writes only. They add no
model input, so the Gemini context explanations in both languages are unchanged.

The Gemini Host's shared model offer for disconnected conversations only changes
which choices the info sheet lists. It adds no model input, so the Gemini context
explanations in both languages are unchanged.

The Session info sheet now lists model and reasoning choices before the CodeGraph
section and moves the native connection state to the end. The CodeGraph guidance
it shows and the preference it saves are unchanged in both languages.

A Gemini turn whose worker dies because the host is already closing is now
marked for the existing restart continuation instead of recorded as a failed
turn, and the daemon stops the owned hosts before the Cloud context on a signal.
The continuation's text, condition and single delivery are unchanged, so the
`gemini-restart-continue` explanations in both languages still apply.

Both daemon stop paths now share one order that stops the owned hosts before the
Cloud context. It adds no model input; the `gemini-restart-continue` explanations
in both languages still apply.

The Gemini Host's native-log reader now also projects turns a `schedule` timer
starts and reads `manage_task` status results. It only reads the CLI's own log
for display and sends nothing to the model, so no transparency explanation
changes.

The native-log reader also looks under `$HOME/.gemini/<app-data-dir>`, where AGY
1.5 writes its conversation logs. It still only reads for display and sends
nothing to the model.

The mobile Session header previews the rightmost available entrance. Pressing any
part of that button lists every available entrance vertically; only choosing an
entrance executes its action. The arrow stays close to the preview. Its expand/collapse
labels are UI copy only. The reviewed English and Traditional Chinese `session-rotation`
explanations, handoff trigger and original excerpt remain applicable; the shared
`next-strings.ts` source receipt follows this UI change.

The voice-work entry also covers explicitly awaited discovery continuations:
original requests, recorded results and real tool contracts can be sent to the
selected CLI for up to four planning rounds per request. GET and startup still
never call a model. Both language explanations name the additional trigger and
quota boundary. Versioned question approval, effect admission, capability-scoped
claims and durable cross-browser narration are deterministic execution boundaries,
not authority granted to the planning model.

Persistent Bots have a separate conditional entry for Claude Code and Codex.
The default-hidden module records configuration without opening a provider.
Only an explicitly enabled, authorized send whose native readiness is verified
may expand captured standing instructions and provenance-checked stored
context into ACP text blocks. A selected quote is a separate checked block;
forget revisions reject old or unknown-provenance quoted turns and unsafe
opaque resume. Queued turns capture instructions at their next idle start.

The reviewed sources include the runtime and native prompt producer. The
scripted peer in testdata is registered by the conservative producer scan as
verification code, not as an external provider or evidence of native loading.
This inventory establishes no native read-only or memory-isolation proof.
English and Taiwan Traditional Chinese describe the same conditional scope.
The public website snapshot must be imported and deployed after the reviewed
product commit lands; no website publication is performed by this change.

Persistent Bot workers reuse the broker's child briefing and result protocol.
The conditional child card in English and Taiwan Traditional Chinese now names
this trigger and its exact additional instruction: the worker is neither a human
nor a terminal Root, returns results and landing facts to the source Bot chat,
and cannot approve, claim or finish Board items for a person. The reviewed
excerpt contains the actual branch in ChildBrief. Authenticated source policy,
process-local capabilities and the broker enforce admission; the instruction
alone does not establish authorization or successful execution. Native provider
admission, production host wiring and public website publication remain separate
acceptance work. The website snapshot must follow this revision when published.

A fresh persistent-Bot native Session also receives a structured handoff from
source-owned SQLite facts: the exact identity and captured instructions, current
filtered memory, unfinished-work references and a history bookmark. The runtime
builds it within the final memory revision fence, and the ACP producer verifies
that opening against the exact prompt turn/context before writing. No transcript
excerpt or opaque provider summary is silently substituted for filtered memory.
Unfinished references grant no replay authority. The existing conditional card,
source excerpt and both language versions describe this addition; native
readiness and the eventual public website snapshot remain separate acceptance.


The first installed Claude Bot route adds explicit local artifact setup and
OS controls to the existing lazy production Host. Setup starts no native
adapter and establishes neither login nor model access. Its session settings
disable native transcript persistence, prompt suggestions and progress
summaries; a new process reconstructs the app's filtered context. Review of
the persistent-Bot card in English and Traditional Chinese distinguishes
those controls from live-provider evidence. The unrelated Antigravity launch
context is unchanged; its source link follows the CLI entry-point movement.
The public website snapshot still follows approval and publication of the
reviewed product tree.

## Browser layout and address controls (2026-11-15)

The new English and Taiwan Traditional Chinese reload hint and address-navigation
label in `next-strings.ts` are browser UI copy. They do not change the localized
handoff request or any provider instructions. The `session-rotation` explanation,
original excerpt, loading condition and effect were reviewed in both languages
and remain accurate. The source receipt follows these two browser labels;
website snapshot publication remains a separate step.


The persistent Bot output card documents the execution-bound `send_file`,
`publish_artifact` and `list_artifacts` tools, immutable local snapshots,
paired chunk reads and the isolated HTML viewer. Its English and Taiwan
Traditional Chinese explanations distinguish explicit publication from
automatic Project upload and from general input-file uploads. The existing
Bot context explanation now follows the agent's four permission choices and
revocable Claude standing rules. Source receipts cover the actual output
producers and viewer; they do not establish a deployed website or a live
provider result.


Desktop tool preparation identifies Recordly as an independent application with
no Skill entry. Both language cards describe host and permission checks, local
recording/import and file export, and the difference between task/Project file
locations and machine-wide installation. The prompt grants neither OS permissions
nor cloud uploads. The reviewed excerpt includes the conditional desktop path;
listing a source does not establish installation or a successful recording.
The public website snapshot must follow this reviewed product revision.

## Bot routine schedules in development builds

The conditional `persistent-bot-routines` entry records a new timer-triggered prompt source. The owned MCP endpoint can save, list, pause, delete or explicitly test a schedule for its current Bot. Due work uses the existing actor but a fresh isolated native history slot, and the next ordinary chat loads its remembered main history with a new fence. Enabling a schedule authorizes future model turns using that native agent account; saving is not an immediate test. Tagged interval, Unix-millisecond once and standard five-field cron triggers retain their IANA zones and unchanged deadlines; up to 20 OR triggers share one coalesced pending wake. Extended cron modifiers, webhook/event triggers and automatic replay remain unsupported in this slice. Interrupted routines are excluded from the ordinary chat restart notice. Both language explanations distinguish scripted A/B/A proof from installed-provider qualification, and state that current Codex without resume refuses routines. Public Release Bots remain hidden. The website still requires its own synchronized snapshot and deployment acceptance after this source lands.

## Native Bot protocol volume and diagnostic boundaries

The ACP reader, ordered notification queue, write path and text capture no longer impose local notification-count or one-mebibyte transport ceilings. They follow the referenced Codync transport behavior while retaining prompt/load ordering, cancellation, source fencing and unknown-effect receipts. Native output-stop reasons remain distinct from local pressure. The runtime keeps a typed capacity boundary when one is available, without copying raw provider errors, changing model instructions or turning uncertainty into permission to replay. The persistent Bot context, input and routine explanations in both languages were reviewed and remain accurate; these changes affect protocol delivery and diagnostics, not the instructions they describe. The refreshed source receipts are not website deployment acceptance.

The default-off tool-library suggestion switch adds conditional guidance to new
Session and child executions. Its own card explains local search of recommended
and saved sources, the explicit proposal and agreement boundary, and unchanged
existing/resumed conversations. It grants no tool installation, API spending or
workflow authority. The CLI search reads metadata only.


Routine progress now has its own conditional entry. The host supplies a
JSON-quoted prior note as untrusted data to an isolated routine prompt and
instructions for saving the whole updated stopping point. Both language cards
describe the exact trigger, deterministic content/token checks, separate routine
storage, atomic clearing on memory forget and stale-execution refusal. The
existing schedule card no longer claims that the supplied Codex adapter reports
no resume support; installed native routine A/B/A remains separately unqualified.
The public website snapshot must import this reviewed product revision after it
lands. A refreshed source receipt alone is not website acceptance.

## Session decisions in voice calls (2026-11-16)

The voice-work entry now explains person-owned waits, unresolved Notes, explicit
answers expressed by meaning, and the distinction between replying to a Session
and performing its filesystem operation. Both language explanations preserve the
managed exact-spec confirmation boundary. The original excerpts include the
updated planner and shared decision guidance. Fleet question reads retain machine,
conversation and execution identity; every answer requires the earlier question
fingerprint. A blocked send returns a read/answer route without retrying its write.
Synthetic regressions cover changed identities, missing menus and changed questions;
model comprehension and audio acceptance remain separate. The public website
snapshot must import this reviewed product revision after it lands.


## GPT-Live speaking pace (2026-11-16)

The conditional `voice-live-pacing` entry records the existing four speed settings as
localized speech instructions. Startup includes the selected pace; an in-call change
also sends a trusted session-wide `session.instructions.append` to the speaking model.
The latest instruction explicitly supersedes prior pace directions and preserves
content, answer length, speaking language, accent and interruption behavior. The
English and Taiwan Traditional Chinese explanations distinguish saved settings and
sent instructions from audible compliance and exact playback multipliers. This
OpenAI voice path is independent of the native-agent filters. Synthetic protocol
checks require no provider call; listening acceptance remains separate. The public
website snapshot must follow this reviewed product revision after it lands.

## Automatic briefing from the Session list (2026-11-16)

The conditional `voice-list-briefing` entry records the list-only opening trigger.
Both voice engines read current Session summaries, with person-owned waits and
unresolved Notes first, then present one or two Sessions per batch. The runtime
retains one prepared next batch and yields to speech or an explicit screen switch.
Realtime playback evidence and GPT-Live transcript quiet-window evidence remain
distinct; neither synthetic output proves listening comprehension. Recap mode and
cache policy still govern any native assistant fork. The owning machine receives the
fork flag and uses the Session's native assistant account for that recap. Fleet
reports use current execution identities and continue past Sessions that disappear.
The English and Taiwan Traditional Chinese speech guidance distinguishes an unreadable
transcript from a read with no new progress. No extra organizer, persistent
input, answer or action approval is created by the implicit opening. The separate
durable task allowance remains three. Both language explanations and the original
reporting guidance reflect the two-Session bound. The public website snapshot must
follow this reviewed product revision after it lands. Voice message-board reads also
exclude archived boards and describe an unassigned Lead without inventing an actor.

## Voice questions selected in the call window (2026-11-16)

The voice-question-menu entry records the new explicit UI trigger and both provider paths.
Opening the menu reads only directory metadata. Clicking a complete sentence adds user input
and application routing context; it never approves a mutation. Recent-reply explanation shares
the existing reader, carries the actual question and checks conversation identity before and
after content reads. The English and Traditional Chinese descriptions distinguish submission,
model behavior, speech and playback. They also distinguish this menu from the proposed separate
question-focused analysis service, which is not implemented. Source receipts and local fixture
acceptance do not establish website, Cloud deployment or live-call acceptance.


## Focused voice questions and the call menu (2026-11-16)

A focused question now reaches a shared machine endpoint from either voice engine. It provides
the actual user question and bounded recent originals to the existing small OpenAI text model,
with no tools. The response carries an answer, literal source quotes, unknowns and whether older
material is needed. Quote validation is structural evidence, not a semantic-quality guarantee.
The original-message context stays in bounded machine memory, is checked against the current
conversation and newest-page revision before reuse, and is reclaimed on a later request after
expiry. Opening summaries and their five-minute freshness policy do not invoke this path.
The revised bilingual voice-question-menu entry covers this provider input, its usage estimate,
its demand-only trigger and its failure behavior. A new route fails on an older machine instead
of accidentally starting an unrelated paid recap. No live paid call was used for local tests.

## Results for explicit voice reads (2026-11-20)

Focused review questions can include the reviewer's recorded completion summary when the
notice and the dispatch owner match the selected conversation. The small text model treats
both the review and the main Session replies as source data. If analysis is unavailable or its
quotes fail validation, supported clients receive bounded source excerpts with the failure stage
and returned usage. Rephrasing the same read for a Session within one spoken turn reuses the
request. GPT-Live hands explicit answers and recaps to the speaking model and discards a pending
opening update when the person starts a question. The bilingual explanation distinguishes this
handoff from observable speech. No live provider call is required to test these paths.

## Voice menu receipts and usage estimates (2026-11-17)

The voice-work, list-briefing, live-pacing and question-menu explanations were
reviewed in English and Taiwan Traditional Chinese. These changes do not add or
alter model instructions: a completed rich-row receipt is consumed directly,
interruption cancels the pending opening read, and native menu navigation uses
arrow keys with selection confirmation. Conversation, execution, machine and
question-fingerprint checks remain in force. Usage snapshots, cache-write pricing
and the bounded final-usage receiver record costs rather than adding provider
work. Focused-analysis usage is recorded independently by the machine to avoid
counting it twice in daily estimates. The refreshed source receipts establish
reviewed sources, not listening quality or deployment acceptance. The public
website snapshot must import the delivered revision after it lands.

## App sign-in and browser pairing (2026-11-17)

The app sign-in, setup completion and pairing labels changed in
`next-strings.ts`. They are interface copy, not provider instructions. The
`session-rotation` card's condition, original excerpt and effects were reviewed
in English and Taiwan Traditional Chinese and remain accurate. No handoff
request, injection sink or assistant launch behavior changed. The source
receipt follows this copy update; website snapshot publication is separate.

## Voice continuation and local hangup (2026-11-17)

The bilingual list-briefing entry now separates per-item presentation progress
from exact spoken-reference evidence. Live's transcript quiet window remains an
estimate, not playback completion. The new conditional call-ending entry records
the English and Taiwan Traditional Chinese instructions, current-question yes
handling, local ending deadline and provider-free hangup cue. Neither mechanism
grants terminal writes or background task approval. Both source excerpts and
language copies were reviewed; the website must import this delivered revision.

## Session report phrasing and call controls (2026-11-19)

The bilingual briefing and recap instructions report progress and blockers. They mention an
owner decision when the evidence shows one and introduce each new Project by name. A direct
farewell in the transcript closes the call locally. A typed delivery receipt and a confirmed
Session switch trigger distinct Web Audio cues on the call's audio context.

## Voice playback and natural report controls (2026-11-19)

The client recognizes natural variations of closing and continuing a report. Realtime report
progress follows completed audio for the prepared batch. Reference evidence used to resolve a
later action still comes from the spoken transcript and current Session identity. The reviewed
English and Traditional Chinese guidance and source receipts reflect these boundaries.

## Managed task resources (2026-11-17)

Managed-resource instructions now distinguish retained deliverables from temporary
binaries and raw execution logs. The English and Traditional Chinese child
explanations and original briefing excerpt reflect retention and positive safety
checks; they do not grant a new deletion authority or tool permission. The
resource CLI registration and Root artifact directory do not add a provider
injection trigger. The callback guide describes the shared inspection/cleanup
contract and its future Bot boundary; reading it does not execute cleanup.

The 2026-11-17 portability correction keeps the same child storage policy in both
languages. Both Chinese tags share the reviewed compact wording; launch-size
checks now choose their language explicitly instead of inheriting the host locale.
The original briefing excerpt and source receipts follow this wording correction.

Bot API-key identities have a separate conditional entry, `persistent-bot-api-key`.
It describes dedicated Secret Handoff input, protected-file encryption, explicit
provider environment allowlists and the separation from ordinary model context.
The fallback entry now names ordered Agent/login identities, including another
login of the same Agent. Synthetic local proof does not establish provider,
production Cloud or installed-native acceptance.

Heavy callbacks now reuse a registered Project-scoped Go build cache and describe
its source-owned consumer fence, idle capacity trimming and lack of a cache
retention delay in both guide languages. Ordinary scratch keeps 24 hours and
non-artifact raw logs seven days. Review of the English and Taiwan Traditional
Chinese core-guide cards confirms their loading trigger and per-section scope
remain accurate: this guide change does not inject a new startup instruction,
change native permissions, create a Bot or turn a future Bot into a machine
operator. The reviewed source receipt follows both guide files; the website
snapshot must import this reviewed product tree after landing.

The Bot-network source verifier selects the pinned signing key by the actual
sender machine, independently of the original pair offer direction. Review of
the English and Taiwan Traditional Chinese Bot-network explanations confirms
that this corrects signature verification without broadening grants, scopes or
injected instructions. Both native source orientations have focused local
evidence; private Cloud wiring and destination acceptance remain pending.

The Bot-network explanation in both languages still applies when the source
verifies assistant consent before target lookup and requires the current
target generation for newly submitted messages. The same admitted request
can recover its receipt and correlated reply after the target binding changes.
The source receipts cover those changes without changing the recipient prompt,
native permission choices or the scope of external assistant consent.

The owned Bot main-chat reaction tool and its current unsupported room boundary are documented in [Message reactions](project-assistants-reactions.md). It accepts only an emoji; Host captures the actual running actor and answered message.

## Older library artifact titles

The English and Taiwan Traditional Chinese output explanations still describe
metadata-only renaming, complete content verification, retained unknown intents
and unchanged Bot instructions. A verified artifact read now refreshes the
matching older library card's logical title within its Bot, Project and
conversation scope. Delayed history cannot overwrite an observed newer title;
publication identity, bytes and dates remain intact. This adds no model call,
permission or automatic command replay. Closing the viewer returns focus to
the originating card. The source inventory includes the library projection
and title merge; the public website snapshot must follow the delivered tree.


The Bot information panel now keeps one explicit settings draft across Details
and Bot settings. Instructions, Agent/model, selected-login and fallback values
keep their existing source validation and next-turn semantics. Secondary controls
open on request; changing the layout does not add model calls, prompt text,
memory sources, execution permissions or automatic save/replay. The existing
English and Taiwan Traditional Chinese Bot explanations were reviewed against
those unchanged boundaries. Conversation compaction remains an unqualified
capability gap rather than a placeholder control.

The ACP client now retains optional live command and usage reports through
installed-session isolation. These readings require the same native session
and execution fence; unknown metadata, cancellation and connection failure
never advertise an actionable capability. Reviewing the persistent-Bot and
input explanations in English and Taiwan Traditional Chinese found no new
prompt text, memory input, execution permission or automatic replay. This
internal read port does not execute compaction or add a policy control.

The Host now transfers that cached reading only from an existing live actor,
through the owned-session lifetime. Human and background authority, complete
execution/native identity, current binding and memory/instruction revisions
qualify it; closing seals the reading before native cleanup. This adds no
provider initialization, prompt text, memory input, native write, tool or
automatic trigger. Review of the existing English and Taiwan Traditional
Chinese Bot explanations confirms those boundaries remain accurate. Both
Host and runtime read sources are included in the reviewed inventory. No
HTTP/UI command or idle maintenance policy is introduced by this read path.

The separate native maintenance port uses the adapter's literal `/compact`
convention without ordinary chat context. It advertises and consumes typed
compaction facts, checks the captured session fence and accepts one attempt
per idle client lifetime. Uncertain replies never become automatic retries.
It remains disconnected from Host commands and UI until durable intent and
recovery compatibility exist. The English and Taiwan Traditional Chinese Bot
explanations still add no automatic trigger, memory input or policy control;
the source inventory now includes this explicit maintenance producer.

## Named Bot topic memory source foundation

The persistent-Bot entry was reviewed in English and Taiwan Traditional
Chinese for the new title/summary indexes and explicit `read_memory` detail
argument. Full note bodies are absent from assembled context and index
responses. Actor and full Project ownership, memory revisions, forget
tombstones and opaque-resume refusal stay in force. The Project shared-memory
view remains read-only and outside model context. Controlled Codex note
write/recall/Forget qualification is recorded in the memory/settings document;
Claude memory qualification remains blocked by its observed native usage
refusal. Production observation and installed-binary recovery remain separate.
The source receipt covers the schema-12 protection, compact index and detail paths;
website publication follows the delivered product revision.

The single-chat presentation update changes the identity opener, draft toolbar visibility,
text bubbles and transient message controls. Review of the English and Taiwan Traditional
Chinese Bot-review card found no change to its recipient prompt, source reply prompt,
exchange ownership, source authority or replay rules. The existing bilingual explanation
remains accurate; the ChatRows implementation receipt follows the reviewed presentation.
Opening Details dismisses a palette without discarding an unresolved reaction request.
The website snapshot must follow this reviewed source when it is next published.

## Direct voice drafts and execution tones (2026-11-18)

The voice-work explanation now covers the direct Live and unmanaged Realtime
message admission gate as well as the existing durable path. Both languages
separate the original request from approval of assistant-composed text, require
full recipient/content read-back, bind fresh consent to the exact draft and
recipient identity, and describe one-use approval and unknown-send handling.
The excerpt quotes the application's review result. Live local media activity
and quiet are conservative evidence; the explanation does not claim listener
completion from transcript gaps or actual phone acceptance from synthetic tests.
Execution tones now follow running call-owned tasks and admitted action requests;
queries, saving, planning, queueing and draft review remain silent. Website
snapshot publication is still separate from refreshing these source receipts.


## Revisioned native topic-note writes

The persistent-Bot entry was reviewed in English and Taiwan Traditional
Chinese for the explicit revisioned note object accepted by edit_memory.
read_memory returns an index and its revision from one snapshot. Literal
request digests preserve exact retries after an acknowledgement is lost,
including after a later forget; replay cannot recreate the forgotten note.
The current-turn Host, full Project and peer-request restrictions remain in
force. This increment retains the existing core/archive schema 12 data shape
and API 89. It does not include the separately isolated additive topic store
or change metadata. Scripted tool tests are separate from installed-model
selection, clean-conversation recall, updater qualification and production
acceptance. The note editor is now part of this source construction: it keeps
full bodies out of the index, binds edits to the displayed revision, and keeps
the literal request after an uncertain acknowledgement. It does not change the
memory authority or enable Project memory writes. Low-frequency policy,
short-fact and bulk-forget controls are disclosed when needed. Both language
versions were checked against these same boundaries. Its real-parent browser
fixture is independent of installed-provider and production visual acceptance.


## Whole-call voice observations (2026-11-18)

The voice-work entry now describes one call-owned operation ledger shared by the
persistent history and expanded diagnostic tool list. Both language explanations
cover automatic opening, read-ahead, per-Session reads, stable IDs, cancellation,
cache reuse, unconfirmed results after hangup and the distinction between additive operation duration and elapsed
coverage. The ledger is observational: it does not enter provider instructions,
execute tools, replay requests or grant consent. Its private journal metadata
uses existing flushes. Reload persistence and cross-device history are not
claimed. The reviewed sources include the ledger and its real modal component;
website snapshot publication remains separate from source receipt freshness.

The anchored message-action update changes transient menu placement and the display of
pending and receipt-confirmed reactions. Review of the Bot-review explanation in English
and Taiwan Traditional Chinese found no change to peer prompts, exchange ownership,
memory selection, native permission or replay authority. Unknown reaction outcomes still
retain their original request. The ChatRows source receipt follows the reviewed renderer;
the existing bilingual instruction explanation remains applicable.

## Cache admission target correction (2026-11-18)

The callback instructions now distinguish the individual cache eviction target
from aggregate storage admission limits. Review of the English and Taiwan
Traditional Chinese text confirms that an active or unsealed cache postpones
eviction without independently blocking an affordable compile. Source-backed
sealing, protected outputs and future Bot authority are unchanged. No new
injection trigger, provider permission or cleanup path is introduced.

The callback guide now assigns optional per-cache sealing and trimming to
periodic maintenance. Review of both language versions confirms that measured
aggregate budgets and the reserve still govern compile admission; no permission,
retention period, output protection or Bot authority changes. The existing
instruction explanations remain accurate. The reviewed source receipts follow
the guide wording; they do not establish website deployment acceptance.

## Offline Bot archive recovery

The persistent-Bot explanation in English and Taiwan Traditional Chinese now
includes the local `archive restore` and read-only `archive export` commands.
The operator explicitly names the expected machine, Bot and complete Project;
the archive hash is integrity evidence, not a signature or a paired grant.
The restore validates and reconstructs the archive before publishing a new
isolated database. Neither command starts a native provider, resumes imported
turns or replaces the running daemon's data. This introduces no model prompt
or live recovery policy. The new command and refreshed parent-command source
receipts do not establish actual-binary qualification or destination acceptance.


The generated English and Taiwan Traditional Chinese route inventories now
report API 92. Only that generated level line changes; generated routes are not
one of the compiled guide's 21 freshness-pinned translation parts. The current
and fallback translation tests still verify those independent guide pins.


The Bot server title and details now share a source/grant/Bot/Project-bound
observation driven by the existing visible-chat refresh. Both language
explanations distinguish selected-chat observation from a Host background
stream, preserve stale and uncertain outcomes, and keep installed/Cloud
acceptance separate. The controller, hook and Chat mount are included in the
reviewed source receipts; the native Bot instructions themselves are unchanged.

The assisted Cloud pairing briefing uses existing remote access and checks the
target's Cloud machine ID before sending its one-time offer. An already
configured InstaBox CLI can wake an auto-paused target. The browser can also
use the account's explicit InstaBox binding to wake the target before it makes
an offer. Both language explanations and the original briefing excerpt were
reviewed. A wake response alone does not verify an offer or its fingerprints.

The InstaBox installation Session receives a prepared first message after the
person chooses the setup action. Its instructions keep the verified provider
terminal through registration and first device pairing. The pairing helper can
also use a signed-in provider dashboard terminal, with hidden input for the
one-time offer and fingerprint evidence from the verified Cloud machine.
The provider terminal and the person's Cloud pairing page have distinct roles.
The reviewed inventory includes the setup entry, both language explanations,
and the original instructions.

A pairing helper reports the target machine's acceptance and observed fingerprints.
The originating browser owns final confirmation after opening the handover and
checking the fingerprints. The reviewed briefing and both languages distinguish
these two outcomes.

The English and Taiwan Traditional Chinese Schedule Webhook wake setting explains
the account owner's opt-in and possible InstaBox runtime costs. It is console
copy. The existing assisted-pairing, setup and Session rotation instruction
excerpts still describe their respective flows.

The Bot review panel resolves participant profiles by their recorded Project
and reads exchange notices through the existing paginated chat view. The
English and Taiwan Traditional Chinese Bot review and turn context explanations
still describe the same prompts, tools, memory access and execution authority.

Closing a Session now dismisses its detail and hides the row while the terminal
receipt is pending. The Cloud writer captures the selected execution before the
detail changes and uses the closeability version from the confirmation. The
English and Taiwan Traditional Chinese Session rotation and CodeGraph entries
still describe the same handoff request, safety check and message guidance.

The Bots memory inspector reads Project memory previews on demand, one page
at a time. Review of the English and Taiwan Traditional Chinese persistent-Bot
memory explanations confirmed that Project files remain a read-only display
and do not enter the Bot's model context. The source receipts follow the
reviewed read projection and UI.

The Bot roster leaves incoming Bot exchange notices out of the recipient's
unread count. The English and Taiwan Traditional Chinese Lead explanations
still describe the same handoff, execution authority and prompts. The reviewed
source receipt follows the roster projection.

Starting in an explicitly registered Project resolves the saved ID before
preparing the launch context. The English and Taiwan Traditional Chinese
language, Project memory, role, workflow and CodeGraph explanations still
describe when their context is selected and how it reaches the assistant.
The reviewed start-route receipt follows the Project lookup.

Project starts give a known tmux pane an early inventory and screen read.
The English and Taiwan Traditional Chinese launch-context explanations still
describe the same language, memory, role, workflow and integration inputs.
The reviewed start-route receipt follows the observation timing change.

Project launches in tmux can use the pane's ready screen to advance discovery.
The English and Taiwan Traditional Chinese explanations still describe the
language, role, workflow, Project memory and integration inputs at launch.
The reviewed start-route receipt follows the change to discovery timing.

Queue edits pause a never-started message at its original position until one of
the explicit human actions resolves the hold. The source commits the replacement
text, turn state and receipt together. The context indicator uses only the
provider's current token report or the last successful reading for the same
binding and memory revision. The group history projects existing Broker states
under the room turn's current member. The English and Taiwan Traditional Chinese
explanations and source inventory cover these paths.

The background-work sheet sends a localized status question as ordinary Session
input when the person presses Ask Session. The input uses the composer queue
rules and the existing conversation receipt. The English and Taiwan Traditional
Chinese CodeGraph, tool preparation and handoff entries still describe their
own activation and input rules. Refresh the receipts for the shared sources.

A message sent to a just-launched tmux Session waits briefly for its assistant.
The English and Taiwan Traditional Chinese explanations still describe the
language, role, workflow, Project memory and integration inputs at launch.
The reviewed start-route receipt follows the record of launched panes.

A Session started in tmux may run in a login shell Clawdline started earlier
in the same Project directory (the `warm_shells` Workflow setting, on unless
turned off). The shell reads the same startup files, and the launch line it
is given is unchanged, so the English and Taiwan Traditional Chinese
launch-context explanations still describe every input at launch. The
reviewed start-route and settings receipts follow the new switch.

Bot send recovery keeps an uncertain user message in the transcript and
looks up that command's admission receipt. The browser retains its pending
attachments for an explicit retry or draft restoration. An edited recovery
and the original compete for the same source admission. Review of the
English and Taiwan Traditional Chinese Bot context and routine explanations
found the same instruction inputs, provider scopes, and execution rules.

A Bot whose previous Agent process could not be confirmed stopped refuses a
new message as not sent (`pa_capacity_full` / `owned_cleanup`) instead of as
an unknown effect, logs the cleanup cause, and the chat says resending is
safe. Review of the English and Taiwan Traditional Chinese Bot context
explanations found the same instruction inputs, provider scopes and execution
rules; only the source receipts change.

The Cloud Bot roster can ask its paired source for the existing list before
the API version and command inventory arrive. The English and Taiwan
Traditional Chinese Bot explanations still describe the same send authority
and instruction inputs. The source receipt follows the read path.

Submitting a Bot message now keeps its attachment checks and send running
when the chat closes. On return, the browser can retry an unconfirmed send
once under the original command and source grant. The source deduplicates
admission by key. Review of the English and Taiwan Traditional Chinese
persistent Bot and interrupted-turn cards found the same instruction inputs,
source authority, and turn-review prompt. The source receipt follows the
browser send path.
