# Bots settings: memory shared by every Bot, and what else belongs there (2026-11-23)

Status: design proposal, nothing implemented. Base revision `f210e0c65`. Codync compared at
`9dae3e2bec9b91a4a42f85a943eb3983f95d0503` (2026-11-23, release 2.53.0).

## The request

The person asked for a **Settings** entry above the Bots list where they can write memory that
applies to every Bot. For example: "ordinary coding Sessions use Codex, design work uses Claude
Code, image generation uses Codex, Gemini or Grok". They also want to choose which Projects each
memory applies to, with all Projects as the default. They asked what else such a page should hold,
with Codync as the reference.

The example is mostly a **dispatch rule**: it says which agent should run a piece of work. That
changes the answer. Today the program decides this and the model cannot (§1.4), so writing the
rule as prose for the model to read would not change anything.

## 1. Where things are today

### 1.1 Every Bots-related setting

| Setting | UI | Stored | Scope |
|---|---|---|---|
| Show dispatched clones (顯示派出的分身) | roster options menu, `web/console/src/project-assistants/BotRoster.tsx:235` | localStorage `clawdline.bots.show-dispatched`, `dispatched-work-preference.ts:5-14` | per viewer |
| Sort mode (drag sorting) | same menu, `BotRoster.tsx:235` | `roster-state.ts:14` | per viewer |
| Roster order (default / unread / machine) | filter row, `BotRoster.tsx:240-243` | localStorage via `getRosterState`, `Bots.tsx:370` | per viewer |
| Machine filter | `BotRoster.tsx:245` | view state | per viewer |
| Tower / list view | toolbar, `BotRoster.tsx:231` | view state | per viewer |
| Pin, section | row context menu, `BotRoster.tsx:186-196` | `Bot.SidebarPinned`, `SidebarSection`, `internal/domain/projectassistant/types.go:138-139` (localStorage fallback `BotRoster.tsx:111-117`) | per Bot |
| Hide, set as lead | row context menu, `Bots.tsx:330-346` | `Bot.Hidden` `types.go:137`, `Bot.Lead` `types.go:140` | per Bot (one lead per machine) |
| Preferred lead | none (automatic) | localStorage `["clawdline.defaultLead",account,reader]`, `Bots.tsx:85-87` | per viewer |
| Instructions, agent and model, login profile, fallbacks, name and appearance, notify, background threads (1–6), permissions and mode, team management, extensions, network, standing rules | `BotSettings.tsx:148-181` | `pa.Bot`, `types.go:118-154` | per Bot |
| Take over delivered work (`work_review`) | Bot settings page | `Bot` field `work_review` ("on"/"off", empty = on), commit `dbbf52a84` | per Bot. **Not in this base revision**: it is on `integrate/20261123-batch` and has not reached `main` yet |
| `continue_on_completion` | none; a `dispatch_work` argument, `internal/adapters/projectassistantacp/mcp.go:778-791` | per call | per dispatch |
| Lead creation | automatic, `internal/adapters/projectassistanthost/lead.go:13-30`; agent order Claude Code → Codex, `internal/transport/http/project_assistants_lead.go:12-19` | `LeadSetupNeeded`, `internal/adapters/projectassistantstore/lead.go:16` | per machine |
| Bots on/off | Settings window, `web/console/src/pages/settings/window/SettingsWindow.tsx:1178-1179` | config.json `project_assistants_enabled`, route `/v1/settings/project-assistants` (`internal/transport/http/project_assistants_host.go:535-572`, local owner only) | per machine |
| Dispatch policy prose | Settings window, `SettingsWindow.tsx:343`, `DispatchPolicyPane.tsx` | `dispatch-policy.local.md`, `internal/transport/http/dispatch_policy_settings.go:19-60` (local only) | per machine |
| Bot completion sound | Notifications page, `web/console/src/pages/notifications.tsx:198` | localStorage `clawdline.bots.completion-sound`, `completion-sound.ts:5` | per viewer |

So Bots settings live in five places: the roster menu, the roster filter row, the row context menu,
the Bot's own settings page and two Settings panes. Only the per-Bot page has a clear home.

### 1.2 Memory layers

| Layer | Stored | Injected | Bounds (`internal/domain/capacity`) |
|---|---|---|---|
| Bot memory (the person's and the Bot's entries, `docs/bots.md:119-126`) | daemon SQLite, `internal/adapters/projectassistantstore/memory.go:68` | a fresh native session gets it in the handoff opening; a resumed session gets `<memory_update>` with the whole memory after a change (`internal/adapters/projectassistantacp/client.go:586-611`, `:1109-1113`); `<context_restored>` after compaction (`compaction.go:89`); context snapshot from `Memory.WithCurrent` (`internal/app/projectassistant/runtime.go:1409-1473`) | `bots.memory_entries` 256 (`register_projectassistant.go:282`), `bots.memory_search_results` 30 (`:285`), `bots.memory_preview` 128 chars (`register_bot_memory_settings.go:30`) |
| Project shared memory | `<state dir>/memory/<repo key>/<name>.md`, `docs/project-memory.md:9-13` | the index is added once at launch of ordinary Sessions and dispatched children (Codex `-c developer_instructions`, Claude Code `--append-system-prompt`, `docs/project-memory.md:164-182`). **Not given to Bots**: `docs/project-assistants-memory-settings.md:57-58`; shown read-only, `internal/app/projectassistant/memory_view.go:111-127` | `memory.entries` 1152, `memory.entry_bytes` 64 KiB, `memory.description_bytes` 512, `memory.groups` 16 and others in `register_memory.go:12-72`; launch memory over 9 KiB is refused |
| The person's preferences | no separate layer; they are entries in each Bot's memory (`types.go:22`) | as Bot memory | as Bot memory |
| Bot instructions | `Bot.Instructions`, `types.go:150` | system prompt, "Standing instructions from the user:", `internal/adapters/projectassistantacp/session_configuration.go:187-188`; take effect on the next idle turn | `bots.instruction_bytes` 16 KiB (`register_projectassistant.go:186`) |

Nothing today is written once and read by every Bot. A preference that should hold for all Bots has
to be repeated in each Bot's memory.

### 1.3 Machine-level settings that could be reused

- `nextconfig.File` (config.json) with a schema list of keys, for example
  `tool_library_suggestions_enabled` at `internal/adapters/nextconfig/nextconfig.go:671`, served by
  `/v1/settings/integrations` (`internal/transport/http/integration_settings.go:12-44`), with Cloud
  decoding at `internal/app/cloudops/zz_decode_generated.go:1911`. This works well for one boolean.
  It does not suit a list of rows that each have a Project scope and a version.
- The `DispatchPolicy` struct (`internal/adapters/projectassistanthost/host.go:24-31`) has an
  `Assistant` field, but production fills it from a constant (§1.4). The `/v1/settings/dispatch-policy`
  route is unrelated; it edits house-rules prose.
- The Bot store's SQLite, which already holds versioned per-Bot memory, is the natural owner of a
  per-machine shared list.

### 1.4 How `dispatch_work` picks the child's agent today

- The tool schema (`internal/adapters/projectassistantacp/mcp.go:991-1003`) and its decoder
  (`:765-774`) have no agent argument.
- The policy is a constant, `internal/transport/http/project_assistants_host.go:624-628`:
  `Assistant: "claude", PermissionMode: "auto"`. It is copied through `Host.current` (`host.go:375-399`)
  and into the draft (`internal/app/orchestrator/project_assistant_actor.go:368`).
- `docs/project-assistants-broker.md:16-21` says these values may not come from model arguments.
- **Result: every Bot dispatch runs Claude Code**, whatever agent the Bot itself uses. The CLI is
  different: `clawdline dispatch --assistant` accepts claude, codex or antigravity and defaults to
  the calling agent (`cmd/clawdline/dispatch.go:108`).

**Recommendation: the rule must actually drive dispatch.** A sentence in memory that says "use
Codex for coding" cannot work today, because the model has no argument through which to act on it.
Adding a free `assistant` argument would contradict the broker rule and leave the choice to model
judgement. Split the decision instead:

- The model chooses **what kind of work** this is, from the kinds the person defined. Only the model
  can judge that "redesign the empty state" is design work.
- The program maps the kind to an **ordered agent list** from the person's table, skips agents that
  are not installed or not signed in, and records the resolved agent in the task.
- Without a matching rule, the default rule applies. Without any rules, today's behavior applies
  (Claude Code).

The free-text shared memory still exists for things that cannot become rules ("answer with the
conclusion first").

## 2. Codync

Links are `https://github.com/leepokai/Codync/blob/9dae3e2bec9b91a4a42f85a943eb3983f95d0503/` + path,
written below as `C/<path>`.

### 2.1 Codync's settings

**Global, desktop Settings** (sidebar groups "Personal" and "This Mac", `C/apps/desktop/src/renderer/views/settings/SettingsView.tsx#L32-L43`)

- Account sign-in (`SettingsView.tsx#L152-L181`)
- Theme: System, Light or Dark (`#L183-L185`, `C/apps/desktop/src/shared/appearance.ts#L1-L7`)
- Text size 11–18 pt (`#L186-L192`)
- Usage icons, compact sidebar, show agent logos (`#L193-L199`, `C/apps/desktop/src/renderer/lib/prefs.ts#L68-L79`)
- App language (`#L201-L215`)
- Show thinking details (`#L216-L220`)
- System: Dock, notifications (Bot done, needs you, error; default off), island, voice shortcut, open at login (`#L221-L258`)
- Privacy: share usage analytics (`#L265-L287`, `C/host/src/analytics.rs#L110`)
- Hidden bots with Unhide (`C/apps/desktop/src/renderer/views/settings/HiddenBotsSection.tsx#L16-L24`)
- Usage limits per computer, choose and arrange (`SettingsView.tsx#L289-L312`, `C/apps/desktop/src/renderer/views/settings/UsageLimits.tsx#L37-L50`)
- Updates (`SettingsView.tsx#L314-L360`)
- Computers and devices: name, relay, pairing, device list, cloud assistants, move active bots to top (`C/apps/desktop/src/renderer/views/settings/ComputersView.tsx#L195-L294`)
- Network rules between computers and bots (`C/apps/desktop/src/renderer/views/settings/RuleSheet.tsx#L14-L80`)
- Computer access walkthrough (`C/apps/desktop/src/renderer/views/computer-access/ComputerAccessView.tsx#L25-L44`)
- Voice chat (`C/apps/desktop/src/renderer/views/settings/VoiceChatSettingsView.tsx#L51-L113`)
- No global instructions and no global default model. A new Bot's agent is chosen in code: Claude if installed, otherwise the first that runs (`C/host/src/agent/new_bot.rs#L51-L59`).

**Per Bot** (`C/apps/desktop/src/renderer/views/bots/BotEditorView.tsx`)

- Standing instructions (`#L138-L142`)
- Agent and model, fast mode, login profile (`#L144-L157`)
- Fallbacks, and whether the Bot may change them (`#L161-L170`)
- Advisor mode: the chat plans, and threads run on a separate agent (`#L172-L184`)
- Summarize long chats: Auto, Early, Near the limit, Custom (`C/apps/desktop/src/renderer/views/bots/CompactionRow.tsx#L22-L34`, `C/host/crates/memory/src/compaction/threshold.rs#L21-L32`)
- Avatar and name (`#L200-L217`)
- Workspace folder (`#L219-L247`)
- Permissions (`C/apps/desktop/src/renderer/views/bots/ApprovalRow.tsx#L16-L22`, `C/apps/desktop/src/shared/approvals.ts#L26-L58`)
- Threads at once 1–6, default 3 (`#L250-L254`)
- Worktree per thread (`#L257-L264`)
- Notifications (`#L268`), use the computer (`#L269-L284`), manage the team (`#L286-L293`)
- Network switches (`C/apps/desktop/src/renderer/views/bots/NetworkSwitches.tsx#L13-L28`)
- Connectors and skills (`#L297-L314`)
- Memory view: "Use what your bots share" per Bot, and a switch for the harness's own memory (`C/apps/desktop/src/renderer/views/bots/MemoryView.tsx#L56-L66`)
- Routines: schedule or webhook, timeout (`C/apps/desktop/src/renderer/views/routines/RoutineEditorView.tsx#L162-L199`)

**Per Project**: none. Codync has no Project entity. A project is a Bot's folder, and the only thing
that depends on it is the default of "Use what your bots share": on for managed folders, off for
project folders (`C/host/src/chat/memory/mod.rs#L25-L29`, `C/docs/design/bot-memory.md#L195-L202`).

**Shared memory ("yours")**: Markdown `MEMORY.md` files plus SQLite version records in
`<data>/shared-memory/`. It holds facts, not instructions, and is capped at 400 tokens
(`C/host/src/chat/memory/files.rs#L22-L28`). It goes into the instructions' memory opening
(`C/host/src/chat/context.rs#L199-L262`). A mid-session change sends `<memory_update>`
(`C/host/src/chat/memory/mod.rs#L120-L140`). It cannot be limited to some projects. It is edited
by telling a Bot, not in a form.

### 2.2 Comparison

| | Items |
|---|---|
| **Both** | standing instructions; agent and model; login profile; fallbacks; name and avatar; workspace (ours read-only); per-Bot notifications; background threads 1–6, default 3; permissions; team management; extensions/connectors; network; routines; pin, section, hide, lead; a memory view; `<memory_update>` on change |
| **Codync only** | shared memory with a per-Bot "use shared memory" switch; Summarize long chats; advisor mode; fast mode; worktree per thread; use the computer; a hidden-Bots list with Unhide; show agent logos, show thinking details, compact sidebar; global done/needs-you/error notifications; usage limits; voice chat; switch for the harness's own memory |
| **Clawdline only** | Projects as first-class scope; Project shared memory; show dispatched clones; take over delivered work (`work_review`, on its way to `main`); Tower view; unread and per-machine order; completion sound; dispatch policy prose; Bots on/off per machine; standing rules through `.claude/settings.local.json` |

"Codync only" is based on this page's search: `BotSettings.tsx` has no compaction, fast mode,
worktree or unhide control, and the console source has no unhide action. Some may exist elsewhere in
another form. Verify each one before building on it.

### 2.3 Worth adding, ordered by value to the person

There is no usage data for these Bots surfaces, so frequency is reasoned from the request and the
code, not measured. Items whose only value is matching Codync were left out: voice, updates,
analytics, text size and usage limits already have homes elsewhere or no demand.

1. **Agent choice for dispatched work** (the request itself; Codync's nearest is advisor mode). Every
   Bot dispatch now runs Claude Code. A rule table removes the need to correct the choice by hand,
   or to re-dispatch from a terminal with `--assistant`, on every non-Claude task.
2. **Shared memory, with a per-Bot "use shared memory" switch** (Codync has it). It saves writing
   the same preference into every Bot. The switch is the escape hatch for a Bot that must not see it.
3. **Hidden Bots list with Unhide** (Codync has it). Hiding is one menu click, but there is no
   visible way back. That is a dead end rather than a time cost; it is rare but blocking.
4. **Summarize long chats per Bot** (Codync has it). Long Bot chats are a current performance topic
   (recent commits load the newest messages first), but this is a per-Bot setting. It belongs on
   `BotSettings`, not on this page; listed as a follow-up.

## 3. Design

All four directions are drawn at desktop width and at 390 px. Pictures are in the task artifacts as
`bots-settings-A.png` … `bots-settings-D.png`; the source is `bots-settings-mock.html` (static HTML,
fake Project names). Each picture shows the desktop screen, then (A–C) the 390 px roster with the
entry point, then the 390 px settings screen.

Every direction contains the same content:

- **Shared memory**: rows of text, each with edit, delete and a Project chip set. The scope picker
  is a checkbox popover, not a native `select`. Its first option is "All Projects (including future
  ones)", which is the default, and the other options list each Project and the personal workspace.
- **Dispatch rules**: work kind → ordered agents, with the same Project scope.
- **List**: show dispatched clones (moved here from the roster menu), hidden Bots.
- **Notifications and sound**: completion sound (moved from the Notifications page); a link to per-Bot
  notify and take-over switches, which stay per Bot.
- **This machine**: links to the Bots on/off switch and dispatch-policy prose, which stay in Settings.

What moves and what stays:

- **Moves in.** Show dispatched clones and the completion sound are both settings about Bots that
  are now hidden on unrelated surfaces. They stay per viewer, as today.
- **Stays, linked.** Bots on/off and dispatch-policy prose are local-owner machine settings that
  Cloud must not write (`project_assistants_host.go:567-572`, `dispatch_policy_settings.go:19`).
  Moving them would mix permission levels on one page.
- **Stays.** Order, machine filter and tower view change what the list shows right now. They are view
  controls, not settings, so they stay on the list. Per-Bot fields (notify, `work_review`,
  instructions) stay on the Bot page. This page explains which scope it covers.

### Direction A: gear on the roster toolbar, one scrolling page

A gear button sits beside "+" in the roster toolbar. The settings open in the chat pane on desktop
and as a full screen on the phone, with all groups on one scrolling page.

- Good: one tap; everything visible; matches Codync's single-form Bot editor; least new structure.
- Weak: one more toolbar button; the page grows as groups are added.

### Direction B: "Bots settings…" in the existing options menu, tabbed page

The option sits in the existing ⋯ "清單選項" menu, and the page has a tab for each group.

- Good: no new toolbar button; tabs stay short.
- Weak: two taps and a hidden entry; tabs scroll sideways at 390 px; it hides the fact that there
  are only four small groups.

### Direction C: gear, category rail on desktop, list → detail on the phone

- Good: scales to many groups; the phone list shows a summary per group ("2 entries · all Projects").
- Weak: heavier than the content needs today; one more step on the phone.

### Direction D: settings panel expanded above the roster

The gear expands shared memory inline above the search box, and the other groups collapse into one
row.

- Good: no navigation; the memory stays next to the Bots it affects.
- Weak: pushes the roster down; it does not fit dispatch rules well; it competes with the list on
  the phone.

**Recommendation: A**, with C's rule rows for dispatch. There are four groups with two to four rows
each, so a single page is enough. It is the closest to the existing per-Bot settings form and to
Codync. Move to C only if the page later grows past about six groups.

### 3.1 Data model

Both lists live in the Bot store's SQLite on each machine, the same authority as Bot memory.

```
bot_shared_memory(id, text, scope_all bool, scope_projects []ProjectRef,
                  version, updated_at, updated_by)
bot_dispatch_rules(id, kind, hint, agents []Agent (ordered), is_default bool,
                   scope_all bool, scope_projects []ProjectRef, version, updated_at)
Bot.use_shared_memory  "on" | "off" | ""   (empty = on, the same convention as notify and work_review)
```

- **Scope.** `scope_all = true` means every Project, including Projects added later; it is the
  default. Otherwise `scope_projects` lists Projects, and the personal workspace counts as one.
  A row whose Projects have all been removed stays and is shown as "no Projects". It is not silently
  widened to all Projects.
- **Order.** Rule matching is most specific first: a rule scoped to the Bot's Project beats an
  all-Projects rule for the same kind.
- **Precedence.** Bot instructions > the Bot's own memory > shared memory. The opening says so
  explicitly. Project shared memory is not given to Bots today (§1.2), so it does not conflict with
  this list. A dispatched child still gets Project memory as it does today. For dispatch, a matching
  rule decides the agent. The model cannot name an agent, only a kind.
- **Injection.** Shared memory rows that apply to the Bot's Project form one labelled section of the
  existing memory opening, placed before the Bot's own memory. A change marks every affected Bot's
  memory as changed, so each one's next turn carries the existing `<memory_update>`. No new injection
  path is needed, but the transparency page needs one new card. Dispatch rules are not injected as
  prose. `dispatch_work` gains a `kind` argument whose enum is generated from the rules that apply,
  plus a one-line description of each kind in the tool text.
- **Dispatch resolution.** It takes the first agent in the rule's list that is installed and signed
  in. If none qualifies, the dispatch fails with a typed error that names the kind and the missing
  agents, rather than silently running Claude. With no rules, it uses Claude Code, as today. The
  resolved agent and the rule ID go into the task record, so the result explains itself. The CLI
  accepts claude, codex and antigravity (Gemini). Grok is a Bot agent (`types.go:76`), but it is not
  a dispatch assistant, so the picker only offers dispatchable agents. The pictures show
  "Codex → Gemini → Grok" because that is the person's example. Until Grok can be dispatched, it
  would appear in the picker as unavailable.
- **Capacity**, to register in `internal/domain/capacity`, with values to confirm:
  - `bots.shared_memory_entries` 20
  - `bots.shared_memory_entry_bytes` 1 KiB
  - `bots.shared_memory_total_bytes` 4 KiB: keeps the opening small, about 10× Codync's 400-token cap
  - `bots.dispatch_rules` 12
  - `bots.dispatch_rule_agents` 4
- **Cloud and phone.** New `project-assistants.shared-settings.get` and `.put` words, using whole-list
  writes with `expected_version`, the same pattern as the per-Bot PATCH. Phones may read and write,
  as they can edit Bot memory today. The per-viewer toggles stay in browser storage on each device.
- **Several machines.** Each machine has its own lists, matching one lead per machine and Bots that
  belong to one machine. With more than one machine, the page shows the machine picker already used
  in the roster. Copying a list to another machine is a Cloud feature and is not part of this design.

### 3.2 Does this need an on/off switch?

The rule here is that new behavior needs a switch whose default keeps today's behavior. Both lists
start empty:

- An empty shared-memory list injects nothing.
- An empty rule table dispatches Claude Code exactly as today.

The person turns the feature on by writing the first row, and turns it off by deleting rows, so a
separate global switch would add a second, redundant state. **Recommendation: no global switch.**
Keep the per-Bot "use shared memory" switch (default on), because it is the only way to exclude one
Bot without narrowing every row's Projects. The `kind` argument does not change dispatch when no
rules exist.

### 3.3 Details to confirm

1. The entry point is a gear in the roster toolbar beside "+" (direction A), not inside the ⋯ menu.
2. The page is one scrolling page with five groups: shared memory, dispatch rules, list,
   notifications and sound, this machine.
3. The example is split in two: the agent choice becomes dispatch rule rows, and only true
   preferences stay as shared memory text.
4. In dispatch rules, the model picks the work kind, the program picks the agent, and the Bot can
   never name an agent directly.
5. When none of a rule's agents is installed and signed in, the dispatch fails visibly instead of
   falling back to Claude Code.
6. The Project scope defaults to "All Projects, including future ones". Choosing Projects uses a
   checkbox popover. A row whose Projects have all been removed shows "no Projects" and does not
   apply anywhere.
7. Precedence is Bot instructions > the Bot's own memory > shared memory, and the memory opening
   states it.
8. "Show dispatched clones" and "completion sound" move onto this page and stay per device.
   "Bots on/off" and "dispatch policy prose" stay in Settings, linked from this page.
9. There is no global switch. Each Bot gets a "use shared memory" switch, default on.
10. The limits are 20 shared entries, 1 KiB each and 4 KiB in total, 12 rules and 4 agents per
   rule, all per machine and registered in `capacity`.

### 3.4 Not in this design

- Summarize long chats belongs on each Bot's settings page.
- A hidden-Bots list could live under "List" here, but whether unhide already exists somewhere was
  not proven. Check before drawing it.
- Copying settings between machines is a Cloud feature.
