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
not relabeled from current settings. Graphify remains separate, with its launch
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


The Graphify lookup launch instruction is unchanged by explicit local index
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

