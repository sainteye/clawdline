# Bot permissions: clear copy and an overview of every Bot (design, 2026-11-24)

Status: design only. No product code changes in this record. Base commit `f49bf1dfc`.
The Bots settings modal this builds on is task 2c62d733 (branch head `ff848ed99`, not merged).

## Why

A person asked why their Bots almost never ask for approval. The source answers it, but the
settings page does not: one dropdown mixes two unrelated switches, and its one-line hint does not say
what will actually happen. The person asked for two things:

1. The permission copy in a Bot's settings must be at least as clear as the source explanation, with
   a short sentence next to every option.
2. An overview of every Bot's permissions.

The person approved this direction: split the dropdown into **agent mode** and **Clawdline
auto-allow**, add one **combined effect** line, and put the overview in Bots settings as a table with
risk marks, filters and inline adjustment (cards on the phone).

## 1. What the code does today

### 1.1 Two independent settings, one dropdown

A Bot stores two fields (`internal/domain/projectassistant/types.go:131-134`):

- `Mode`: the agent's own mode. Empty or `auto` means the agent's Auto mode
  (`modes.go:5-7`, `modes.go:52-61`). New Bots start there.
- `PermissionPolicy`: what Clawdline does when the agent asks (`types.go:101-109`).

The console merges both into one list (`web/console/src/project-assistants/approvals.ts:21-29`):
every agent mode, then "Approve automatically". Choosing a mode sets `permission_policy: "ask"`;
choosing "Approve automatically" sets `permission_policy: "approve"` **and drops the mode**, so the
mode falls back to Auto (`approvals.ts:33-38`, applied at `policy.go:224-231`). The console can
therefore produce only `(any mode, ask)` and `(Auto, approve)`, plus a legacy `auto_read` that is
shown only when a Bot already has it. The API and the Lead's `manage_bots` tool can store
`approve` together with any mode (`internal/transport/http/project_assistants_lead_control.go:203-208`
keeps the current mode when only the policy changes).

### 1.2 Agent modes, as the pinned adapters define them

| Agent | Mode id (stored) | Native id | What it means | Source |
|---|---|---|---|---|
| Claude Code | `auto` | `auto` | Claude Code decides whether each action is safe; it stops for permission only when it judges an action risky. If the selected model does not support Auto, the adapter switches to Accept edits and Clawdline accepts that (`client.go:395-421`). | `modes.go:22-28`; claude-agent-acp 0.106.0 `dist/session-mode.js` `AUTO_MODE_FALLBACK = "acceptEdits"` |
| Claude Code | `default` | `default` | Manual: always asks before making changes. | `modes.go:25` |
| Claude Code | `acceptEdits` | `acceptEdits` | File edits are accepted automatically; other actions (commands, network) ask. | `modes.go:26` |
| Claude Code | `plan` | `plan` | Plans before changing anything; acting needs permission. | `modes.go:27` |
| Claude Code | `bypassPermissions` | — | Not offered: the adapter hides it unless the person's settings allow it. | `modes.go:18-19` |
| Codex | `auto` | `agent` | "Auto review": workspace-write sandbox, network off, approval `on-request` with the `auto_review` reviewer, so it asks only for actions the reviewer finds potentially unsafe. | `modes.go:43`, `modes.go:55-56`; codex-acp 2.6.0 `dist/index.js:41828-41843` |
| Codex | `read-only` | `read-only` | Read-only sandbox, approval `on-request`, reviewer `user`: editing files and network need approval. | `modes.go:44`; `dist/index.js:41799-41811` |
| Codex | `workspace-write` | `workspace-write` | Edits inside the workspace; asks before writing outside it or using the network. | `modes.go:45`; `dist/index.js:41812-41827` |
| Codex | `agent-full-access` | `agent-full-access` | Sandbox `danger-full-access`, approval `never`: Codex never asks, has network and every file on the computer. | `modes.go:46`; `dist/index.js:41844-41853` |
| Gemini | `auto` | `auto` | Gemini decides; where its Auto mode is not offered (no native terminal sandbox) Clawdline switches the session to Manual. | `modes.go:31`; `client.go:403-409` |
| Gemini | `default`, `accept-edits`, `plan` | same | Manual, accept workspace edits, plan. | `modes.go:32-34` |
| Grok | `auto`, `default` | same | Grok decides / asks before actions that need approval; passed as `--permission-mode` at launch. | `modes.go:38-39`; `installation_grok.go:66` |

Gemini and Grok semantics come from the descriptions in `modes.go`; their CLIs were not inspected
for this record.

### 1.3 Clawdline's permission policy

When the agent sends a permission request, Clawdline shows a card. Then
(`internal/app/projectassistant/runtime.go:1991-2003`):

- `ask`: the card waits for the person.
- `approve`: Clawdline immediately answers with the agent's **allow-once** option. The person never
  needs to see the card. Rules the agent itself enforces (for example a Claude Code deny rule) still
  apply. If the agent offers no allow-once option, the card waits.
- `auto_read`: answers only options marked read-only. **In practice no native option is ever marked
  read-only**: `ClassifyNativePermission` returns `ReadOnly=false` for every inspected adapter
  version and every payload (`internal/adapters/projectassistantacp/permission_policy.go:12-32`,
  `docs/project-assistants-native-permissions.md:38-46`). So `auto_read` behaves exactly like `ask`
  today, and its label "Allow verified reads" promises something that never happens.

### 1.4 Bots run in the background

Bots run through ACP. There is no native AskUserQuestion menu. When a Bot needs the person, it either
asks in chat or calls `ask_person`, which posts a decision card
(`internal/adapters/projectassistantacp/mcp.go:637`; peer requests are told to do so in
`internal/app/projectassistant/collaboration.go:249`).

### 1.5 Every path that lets a Bot act without a fresh approval

This is the complete list. The combined effect must account for all of it.

| # | Path | Who it applies to | Effect | Source |
|---|---|---|---|---|
| P1 | Agent mode itself | every Bot | Auto modes decide alone; Codex Full access never asks; Accept edits / Workspace access skip edit questions | §1.2 |
| P2 | `approve` policy on native permission cards | Bots set to Approve automatically | every native request allowed once, silently | `runtime.go:1991-2003` |
| P3 | `approve` policy on host-owned "source" cards | same | also auto-answers Clawdline's own source cards that are not marked human-only, such as starting a saved development server | `runtime_source_permissions.go:99-104`, `:222-235`; `devservers.go:101-108`, `:204` |
| P4 | The agent's standing "Always allow" rules | every Bot | Claude Code keeps them in the Project's `.claude/settings.local.json` `permissions.allow`; they are listed and revocable in settings. The agent's other settings (for example the person's own Claude Code or Codex configuration) also still apply | `projectassistantacp/rules.go:13-24`; `service.go:835-860`; `types.go:106-107` |
| P5 | Saved source command rules (development servers) | Bots with a saved rule | a matching start does not ask again | `devservers.go:95-108` (`servers.Approval{Rules: ...}`) |
| P6 | Dispatch inside the Bot's own repository Project | every Bot on a repository Project | no card at all; the child always runs **Claude Code with `--permission-mode auto`** | `project_assistants_host.go:172`, `:626-629`; `host.go:400`; `dispatch.go:1060-1062` |
| P7 | Dispatch into another Project | Lead, on a turn the person started in its chat | no card | `host.go:384` |
| P8 | Dispatch into another Project | other Bots | a human-only card that `approve` cannot answer; "Always" saves a visible, revocable Bot-to-Project rule, after which no card is shown | `host.go:387-397`; `dispatch_rules.go:15-17`, `:178-238`; `runtime_source_permissions.go:106-113` |
| P9 | Lead `manage_bots` | Lead, on a direct person turn | can create, edit (including another Bot's `permission_policy` and `mode`), stop and delete Bots with no second card | `project_assistants_lead_control.go:21-25`, `:183-208`; `docs/project-assistants-owned-mcp.md:233-251` |
| P10 | Team plans by a non-Lead manager | Bots with team management | always a human-only card | `runtime_source_permissions.go:115-132`; `host.go:230-244` |
| P11 | Inherited ceiling for managed Bots | Bots created by a manager | inherit the creator's policy and mode; cannot be raised | `team_management_policy.go:128-167` |

Not a Bot path but often confused with it: a root's `clawdline dispatch` uses the dispatch
permission setting. There, Codex `full` means workspace-write with network and the `auto_review`
reviewer, not Codex's own Full access, and Claude `full` means `bypassPermissions`
(`dispatch.go:1060-1088`, `draft.go:293-308`). Bot dispatch never uses this setting (P6).

**Finding on the unmerged dispatch-rules branch (by reading, not run).** On `ff848ed99` a work-kind
rule can pick Codex or Gemini, but the policy still sends `PermissionMode: "auto"`
(`project_assistants_host.go` on that branch). `draft.go:301-308` accepts `auto` only for Claude
and refuses other agents with "permission_mode must be one of: ask, edits, full". The branch's tests
use a fixture with `PermissionMode: "edits"` (`project_assistant_actor_test.go:31`), so they do
not cover this. A rule that resolves to Codex is expected to be refused at dispatch. The overview
must not advertise a dispatch agent until this is fixed; the fix belongs to that task, not to this
design.

## 2. The fact table (the single source of the copy)

Columns: the Clawdline policy. Cells: the combined effect and risk. "Asks" means a permission card
waits for the person. Human-only cards (P8, P10) always ask, whatever the row says.

| Agent · mode | `ask` | `approve` |
|---|---|---|
| Claude · Auto | **Asks only for actions Claude judges risky** (low). On a model without Auto: as Accept edits | **Never asks** (high: full trust) |
| Claude · Manual | **Asks before every change** (low) | **Never asks** (high) |
| Claude · Accept edits | **Edits without asking; asks for commands and other actions** (medium) | **Never asks** (high) |
| Claude · Plan | **Plans only; asks before acting** (low) | **Never asks** (high) |
| Codex · Auto review | **Asks only when the reviewer finds an action potentially unsafe**; no network by default (low) | **Never asks** (high) |
| Codex · Read-only | **Asks to edit files or use the network** (low) | **Never asks** (high) |
| Codex · Workspace access | **Asks only to write outside the Project or use the network** (medium) | **Never asks** (high) |
| Codex · Full access | **Never asks** — Codex itself requests nothing; the Clawdline setting changes nothing (high) | **Never asks** (high) |
| Gemini · Auto | **Gemini decides; falls back to Manual where Auto is unavailable** (low) | **Never asks** (high) |
| Gemini · Manual / Accept edits / Plan | as the Claude rows of the same name (low / medium / low) | **Never asks** (high) |
| Grok · Auto | **Asks only for actions Grok judges risky** (low) | **Never asks** (high) |
| Grok · Manual | **Asks before actions that need approval** (low) | **Never asks** (high) |

`auto_read` is displayed as the `ask` column with a note "Behaves like Ask me: no request is
currently verified as read-only" until a classifier returns `ReadOnly=true` for some payload.

Every cell also carries two fixed footnotes, shown once under the effect line rather than repeated:
"Dispatching to another Project and team changes always ask you" (P8, P10), and "Rules saved with
Always allow are listed under Saved rules" (P4, P5). The Lead additionally shows "As Lead, it can
dispatch to other Projects and change other Bots when you ask it directly" (P7, P9).

### How the UI reads it

The table lives in one place and the console never writes its own copy:

- Go owns the data: extend `AgentMode` (`modes.go:10-14`) with `Effect` keys per policy, a risk
  level (`low | medium | high`) and the fallback note, and add `PermissionEffect(agent, mode, policy)
  (effectKey, risk)`. A table test iterates `AgentModes(a) × {ask, approve, auto_read}` for every
  agent and fails on a missing cell, so a new adapter mode cannot ship without copy.
- `go run ./tools/contract-gen` already generates Go and TypeScript together; generate the table into
  the contract so `approvals.ts` drops its hand-written `AGENT_MODES` (`approvals.ts:6-12`) and reads
  the generated one. The console renders `w[effectKey]` from the catalogs (English and Traditional
  Chinese in the same change, per AGENTS.md).
- The overview reads the same function per row, so a row and the settings page can never disagree.

## 3. Directions

All four use the same data and the same copy. Pictures use fake Bots (Nova, Pixel, Quill, Ledger,
Orbit, Moss) and fake Projects. Every chevron, radio and warning mark is SVG; there is no native
`<select>` and no character arrow. Static source: task artifact `mock.html` (`?d=A|B|C|D`); phone
pictures render it in a 390 px frame (`phone.html`).

### A. Two columns + combined effect (the approved direction)

Settings: left column lists the agent's modes as radio rows with a one-line description each; right
column has two rows, **Ask me** and **Allow automatically**. Under both, a combined-effect band shows
the fact-table sentence and a risk chip. A second, pink band previews "If changed to Allow
automatically: Never asks" so the risk is visible before it is chosen. On the phone the columns stack.

Overview: a table (Bot, Project, Agent, Mode, Auto-allow, Combined effect, Risk, Adjust). High-risk
rows are tinted. **Adjust** opens a popover with the same two controls, a live "after the change"
effect and a button that names the one Bot it changes. Phone: cards with the same fields.

- Strong: exactly matches the person's approved shape; each switch is explained where it is chosen;
  the preview band makes the high-risk combination hard to pick by accident.
- Weak: tallest of the four on the phone; the preview band repeats information when already on
  Approve (then it is hidden).

### B. Matrix picker

Settings: rows are modes, columns are Ask me / Allow automatically; each cell is the combined effect
itself, and the person picks a cell. High-risk cells carry a muted left mark.
Overview: mode and auto-allow are editable directly in the table cells.

- Strong: every outcome is visible at once; nobody has to imagine combinations.
- Weak: the Approve column reads "Never asks" four times; on the phone the grid becomes long;
  in-cell editing in the overview makes accidental changes easy.

### C. Effect first

Settings: a summary at the top, "Asks you / Does not ask / Always asks you", then two compact custom
dropdowns that update the summary live.
Overview: grouped by outcome ("Never asks you (2)", "Asks you (4)"), compact rows.

- Strong: answers the person's actual question ("when will it ask me?") first; shortest on the phone.
- Weak: option descriptions live inside the dropdown menus, so the choices are less scannable;
  grouping hides the per-column filter.

### D. Presets + advanced

Settings: four presets (Careful = Manual + Ask, Default = Auto + Ask, Edits freely = Accept edits +
Ask, Full trust = Auto + Approve), the combined effect, and an "Advanced" disclosure with the two
columns.
Overview: outcome counts at the top, a table, and batch selection with "Change to Ask me".

- Strong: quickest for most people; batch "back to Ask me" is a strong safety action.
- Weak: presets differ per agent (Codex has no Accept edits), adding a second vocabulary to learn;
  batch editing is a behavior change and needs the guard in §5.

### Recommendation

**A for the settings page, with the overview of A plus the outcome filter chips from C.** It is the
direction the person approved, it explains each switch at the point of choice, and the combined
effect is never more than one line away. Batch editing (D) is left out of the first version; the
only batch action worth adding later is "Change selected to Ask me", which can only lower risk.

## 4. Items to confirm

1. The setting is split into **Agent mode** and **Clawdline auto-allow** with options Ask me /
   Allow automatically; each option has the one-line description from the fact table.
2. A **Combined effect** line always shows the fact-table sentence and a risk chip (low / medium /
   high). High is labelled "Full trust" with a muted red mark; medium and low use neutral and green.
3. When the current choice is not Approve, a second band previews what Allow automatically would do.
4. `auto_read` is shown as "Behaves like Ask me" for Bots that still have it and is no longer
    offered to new choices; the Bot keeps its stored value until the person changes it.
5. The combined-effect footnotes state the always-ask paths (other-Project dispatch, team changes)
    and link to Saved rules; the Lead row also states its direct powers (P7, P9).
6. The overview is a tab in the Bots settings modal: columns Bot, Project, Agent, Mode, Auto-allow,
    Combined effect, Risk; filters All / Never asks you / Auto-allow / per agent / Project.
7. Inline adjustment edits one Bot at a time in a popover that shows the after-change effect and a
    button naming the Bot; saving uses the Bot's `expected_version`.
8. On the phone (390 px) the overview is a card list with the same fields and the same Adjust popover
    shown inline.
9. The copy comes only from the generated fact table; a test fails when any agent × mode × policy
    cell lacks copy, in English and Traditional Chinese.
10. Dispatch is shown as fact, not as a setting: "Dispatched Sessions run Claude Code in Auto mode".
    It changes when the dispatch-rules task lands and its Codex/Gemini permission refusal is fixed.

## 5. Behavior switches

This change is copy and layout. It keeps every stored value and every enforcement path. Two things do
change and are called out for the person:

- **New reachable combinations.** Two independent columns let the console store `approve` with a
  mode other than Auto (for example Manual + Allow automatically), which the API already accepts but
  the console could not produce. The fact table covers these cells, so the effect line stays true.
  If the person prefers not to widen what the console can produce, the right column can be disabled
  for non-Auto modes; recommendation: allow it, because the effect line makes it explicit.
- **Inline adjustment from the overview** writes Bot settings from a second surface. Guards:
  one Bot per save; the popover shows the before/after effect and names the Bot on the button; the
  save sends the row's `expected_version` so a stale row is refused rather than overwriting; raising
  a Bot to high risk requires the same explicit choice as on the settings page (no one-click toggle
  in a table cell). Batch editing is not in the first version; if added, it offers only "Change to
  Ask me", lists the affected Bots before saving, and reports per-Bot results.

No feature flag is needed for the copy. Inline adjustment can ship behind the modal's existing
availability, since the modal itself is not yet merged.

## 6. Not done here

- No product code, catalog or contract changes.
- Gemini and Grok mode semantics were taken from `modes.go`, not from their CLIs.
- The dispatch-rules permission refusal (§1.5) was found by reading and is not reproduced.
