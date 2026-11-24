# Over-defensive write checks — audit, 2026-11-24

Scope: optimistic-concurrency and "stale/unknown" checks that refuse a person's save, toggle or
edit even though nothing the person edited has changed. Investigation only; no product code was
changed. Base commit `87ee68023`.

Already being fixed elsewhere and not repeated here: the Bot settings form
(`web/console/src/project-assistants/BotSettings.tsx:99` → `internal/domain/projectassistant/policy.go:213`,
error `pa_stale_version`). Several findings below share its root cause and should be fixed with it.

## The shared root cause

Two tokens are used as edit guards but advance on almost any activity:

- **`Bot.Version`** (project assistants). Every runtime mutation goes through
  `Engine.prepareMutation`, which sets `m.ExpectedVersion = bot.Version; bot.Version++`
  (`internal/app/projectassistant/runtime.go:271-272`), and the store then writes
  `a.Bot.Version = m.ExpectedVersion + 1` (`internal/adapters/projectassistantstore/repository.go:459`).
  `r.mutate` is called for turn start/update, binding updates, permission answers, deliveries and
  stops (`runtime.go:528,1278,1404,1859,1867,1983,2080,2185,2208,2292,2322`), plus fallback,
  routines, handoff cards, queue edits and send-now. Other bumps: Bot memory writes
  (`projectassistantstore/memory.go:212`), background reports (`background_tasks.go:353`),
  lead re-designation of the previous lead (`roster.go:246`), Bot-to-Bot exchanges
  (`collaboration.go:185`), work bridge (`app/projectassistant/workbridge.go:69`), lifecycle
  (`lifecycle.go:252`), routine runs (`service_routines.go:119,387`). In practice it moves several
  times per chat turn.
- **Work v2 item `version`**. Every `PutItem` runs `version=version+1`
  (`internal/adapters/store/work_v2.go:770-773`), and `PutItem` is called by agent step
  check-offs, documents, phase changes, gates and Epic-child creation (details in finding 1).

A dedicated settings token already exists for Bots: `Bot.SettingsVersion` ("changes on
configuration writes independently of chat activity", `internal/domain/projectassistant/types.go:144`),
maintained in `repository.go:450-456`. It is used today only by team management
(`projectassistantstore/team.go:31,56`). Caveat for anyone adopting it: it bumps on *any* Bot
field difference, including `BackgroundThreads` and the roster fields `SidebarPinned`,
`SidebarSection`, `Lead` (`roster.go:245`), so it is a better token than `Version` but still
broader than "the fields in this form".

## Evidence run

A temporary store test (not committed; source in the task artifacts as
`zz_overdefense_probe_test.go.txt`, output `probe-network-memory.txt`) ran in an empty
`CLAWDLINE_NEXT_DIR`:

```
bot.Version when the panel loaded: 1
bot.Version after one Bot memory write: 2 (memory revision 2)
network toggle, network settings revision unchanged (0), panel's bot version: err=pa_stale_version: network_bot_version
same toggle with only the bot version refreshed: revision=1 err=<nil>
person memory note with revision captured before the Bot's write: err=pa_stale_version: memory
--- PASS
```

So one Bot-initiated memory write is enough to make both an unrelated network-settings toggle
and a person's memory note fail, and the same toggle succeeds once only the Bot version is
refreshed.

## Findings, by user impact

Items 1 and 2 were fixed separately and are on `main`; the status of items 3–10 is noted under each heading.

Severity: **high** = routine background activity blocks an ordinary save; **medium** = happens
when the person edits while something specific runs; **low** = narrow window or rare action.

### 1. Board (Work v2) item edits fail with 409 while an agent works on the item — high, confidence high

- Check: `internal/app/work_v2.go:740` (`Edit`: title, description, acceptance, condition,
  review switch) `if !VersionHolds(c.ExpectedVersion, prev.Version) { return store.ErrConflict }`.
  The same strict check guards images (`:605`, `:655`), convert (`:913`), assign (`:1101`),
  unassign (`:1419`), cancel (`:1967`), complete (`:2020`), reopen (`:2071`). The person route keeps
  the strict number (`internal/transport/http/work_v2.go:1060-1063`); only agent routes may send
  `AnyVersion` (`work_v2.go:2330-2341`).
- Unrelated bumps of the same token: step check-off/cancel/reopen bumps the item after the step
  (`work_v2.go:2306-2312`), add step (`:2228`), attach/delete document
  (`work_v2_documents.go:145,199`), phase advance/finish (`:1754`), retract completion (`:2175`),
  Epic child creation touches the Epic with only `UpdatedAt` changed (`epic_children.go:112`),
  assignment lifecycle (`:1345,1395,1489`, `session_items.go:160`), handoff
  (`orchestrator/handoff_board.go:203`), verification gates (`work_v2_gates.go:136,183,463,559,644`),
  gate-detail purge (`store/work_v2_gates.go:1229`, only `UpdatedAt`).
- What the person sees: HTTP 409 `version_conflict`, "The item changed; reread it before writing."
  (`app/work_v2.go:183-184`); the console shows its conflict word
  (`web/console/src/pages/work/shared.ts:33`, `WorkV2.tsx:1271`). No retry or merge on either side
  (`transport/http/work_v2.go:1072-1075`).
- Frequency: every time the agent checks off a step, attaches a document, changes phase, or the
  verifier reports — i.e. continuously while an item is being worked on.
- Fix: `Edit` already applies nil-pointer field patches (`work_v2.go:747-831`). Compare only the
  patched fields against a base (title/description/acceptance by value or by the existing
  `acceptance_version`/`acceptance_digest` columns), or introduce a `content_version` bumped only
  by `Edit`/`Convert`. Keep a phase/owner token for cancel, complete, reopen, assign, unassign —
  those decisions do depend on phase and owner. Same token problem, minor: the read-only persona
  suggestion pre-check `transport/http/work_v2_persona_suggestion.go:46`.

### 2. Bot model picker greys out Save as soon as the Bot does anything — high, confidence high

- Check (client): `web/console/src/project-assistants/ChatModel.tsx:34` freezes
  `baseline = useRef(bot).current`; `:47` `stale = baseline.version !== bot.version` against the
  live prop; `:48` `blocked = stale || …`, which disables the select and Save.
  Server: `Chat.tsx:584` `saveModel` sends `expected_version: original.version` and is passed to the
  dialog at `Chat.tsx:1709`; it calls `editBot(...)` without the `original` argument, so the
  existing rebase in `editBot` (`Chat.tsx:551-553`: if editable fields are unchanged, take
  `fresh.version`) never runs, and `policy.go:213` refuses.
- Unrelated bumps: every `Bot.Version` bump above (any turn while the dialog is open).
- What the person sees: Save disabled with 「設定已在其他地方變更。表單會保留。請關閉並重新開啟設定，查看最新版本。」
  (`projectAssistants.fallbackSaveStale`).
- Frequency: whenever the model is changed while the Bot is replying or a routine runs — the
  common moment to change a model.
- Fix: compare `sameEditableSettings(baseline, bot)` instead of versions; pass `original` to
  `editBot` so the existing rebase applies; send only `runtime_settings` once the server can merge
  by field.

### 3. Network settings toggles require the whole Bot version although they have their own revision — medium-high, confidence high (reproduced)

**Status (2026-11-25): fixed.** The store no longer compares `ExpectedBotVersion` (`projectassistantstore/network.go`); the settings revision and the conversation fence still guard the write, and older clients may keep sending the field. Test: `TestNetworkSettingsKeepAccountScopeCASAndHistoricalBytes` (a toggle with an older Bot version saves).

- Check: `internal/adapters/projectassistantstore/network.go:70`
  `if a.Bot.Version != write.ExpectedBotVersion { … "network_bot_version" }`, fed from
  `app/projectassistant/network_settings.go:132`. The settings also carry a dedicated
  `ExpectedRevision` that already protects them.
- Client: `NetworkSettingsPanel.tsx:18` sends `expected_version: view.bot_version` from the read made
  when the panel mounted; the view is replaced only after a successful save.
- Unrelated bumps: every `Bot.Version` bump; reproduced with one Bot memory write (evidence run).
- What the person sees: 「來源無法確認這次偏好設定。請重新整理後再試。」 (`projectAssistants.networkSaveFailed`).
- Frequency: any toggle after the Bot has done anything since the panel opened.
- Fix: drop the `ExpectedBotVersion` comparison and rely on the settings `revision` plus the
  existing fence/authority checks; if a Bot-level guard is wanted, use `SettingsVersion`.

### 4. Memory note edits and Forget are blocked by the Bot's own memory writes — medium-high, confidence high (reproduced)

**Status (2026-11-25): fixed server-side.** `rebaseMemoryRevision` in `projectassistantstore/memory.go` lets a note save or Forget read at an older index revision proceed when none of the notes it names was rewritten since; a rewrite of the same note, or a future revision, still answers `pa_stale_version`. Test: `TestMemoryEditsCompareTheEditedNoteNotTheIndex`. Not done: an overwrite choice in `MemoryPanel` for the remaining real conflict, whose message still says to refresh and reopen.

- Check: the whole project memory index revision, not the note's:
  `domain/projectassistant/policy.go:655,678`, `app/projectassistant/topic_memory.go:47`,
  `projectassistantstore/memory.go:100,136`.
- Client: `MemoryPanel.tsx:151,180` capture `expected_revision: value.view.memory.revision` when the
  editor opens; sent at `Chat.tsx:1218-1219`; Forget at `Chat.tsx:1235`. The captured revision is
  never advanced, so retrying fails identically; getting past it means cancelling and reopening,
  which loses the draft.
- Unrelated bumps: Bot memory tools (`app/projectassistant/memory_tools.go:156,220`), topic memory
  from turns (`topic_memory.go:82,95`), compaction (`memory_compaction.go:58`); each also bumps
  `Bot.Version` (`memory.go:212`). Reproduced: one Bot memory write → person note
  `pa_stale_version: memory`.
- What the person sees: 「這則筆記已變更，請重新整理記憶後再開啟。」 (`projectAssistants.memoryDetailStale`).
- Frequency: editing a note while the Bot is in a conversation that saves memory.
- Fix: check the edited entry's own revision (`MemoryEntry.Revision`, already validated at
  `policy.go:681`) instead of the index revision; for Forget, refuse only if a selected source
  changed. Auto-retry with a fresh index revision when the entry is unchanged.

### 5. Routine editor fails to save after the routine fires — medium, confidence high

**Status (2026-11-25): fixed client-side.** The editor's save sends the live definition's version while its name, instruction, triggers, timeout and enabled switch still equal the opened ones (`routine-edit-base.ts`, used by `Routines.tsx`); an edit made elsewhere keeps the opened revision and is still refused. Test: `routine-edit-base.test.ts`. Not done: a server-side field base, and an overwrite choice for a real conflict.

- Check: `app/projectassistant/service_routines.go:345` `if d.Version != q.ExpectedRevision`.
- Client: `Routines.tsx:309` `setEditing(d)` captures the definition; save sends
  `expected_revision: editing?.version ?? 0` (`:381`); `load()` refreshes `page` but never `editing`.
- Unrelated bumps: every scheduled fire `routines.go:290` (`d.Version++` in the tick), follow-up
  cleanup `runtime.go:1847`, lifecycle `lifecycle.go:138`. These change run state, not the name,
  instruction, triggers or timeout the person edits.
- What the person sees: a generic unavailable message with `(pa_stale_version)`; the editor has no
  refresh; closing loses the draft.
- Frequency: editing a frequently scheduled routine (e.g. a lead check-in) across a fire.
- Fix: separate the definition's edit revision from run-state progress (or compare only edited
  fields server-side); in the client, rebase when the edited fields of the fresh definition still
  equal the captured ones.

### 6. Session to-do text edits fail once the Session reads its to-dos — medium, confidence high

**Status (2026-11-25): fixed.** Marking the queue read no longer runs `version=version+1` (`adapters/store/work_v2.go`). Tests: `TestASessionReadingItsTodosDoesNotBlockTheirEdit`; `TestPersonEditsDirectTodoAndMustSendTheRevision` now expects the read to leave the version unchanged.

- Check: `app/work_v2_todos.go:134` `if prev.Version != expectedVersion { return store.ErrConflict }`
  (also `:189`, `:316`, `:555`).
- Unrelated bump: the agent listing to-dos marks them read
  `UPDATE session_direct_todos SET read_at=?,version=version+1 … AND read_at IS NULL`
  (`adapters/store/work_v2.go:2295-2296`), called with mark-read from the agent GET
  (`transport/http/work_v2_agent.go:390`).
- What the person sees: 409 `version_conflict` on a text edit.
- Frequency: once per to-do, the first time the Session polls after it was written.
- Fix: do not bump `version` for `read_at`, or exclude read-marking from the edit token.

### 7. Lead setup stays locked after one stale refusal — medium, confidence medium

**Status (2026-11-25): fixed client-side, not browser-verified.** The choice passes the Bot as `original` to `editBot`, so the existing field rebase applies; a stale refusal no longer disables the choices, 「重新檢查」 clears it, and the new message `leadSetupStale` (English and Taiwan Chinese) says to choose again. No component test or screenshot was made.

- Client: `LeadSetup.tsx:88` passes `choosing = … || error === "pa_stale_version" || …` to every agent
  step, so after one refusal the choices are disabled and 「再檢查一次」 (`:90`) does not clear the
  error; only a remount does. The request uses `leadSetupBot.version` (`Chat.tsx:1712`), i.e.
  `Bot.Version`.
- What the person sees: 「設定已在其他地方變更。表單會保留。請關閉並重新開啟設定，查看最新版本。」
- Frequency: first-run setup while the lead Bot is already producing its greeting/turn.
- Fix: rebase as in `editBot` (unchanged editable fields → fresh version) and let 「再檢查一次」 clear
  the stale error. Not reproduced in a browser.

### 8. Resolving a Note fails if any device has marked it read — low-medium, confidence high

**Status (2026-11-25): fixed.** A read sets `read_at` once through `MarkHumanInterventionRead` without advancing the version and never conflicts; resolve, reopen and withdraw still race on the version. Tests: `TestHumanInterventionReadDoesNotBlockResolveFromOlderCopy`; the route test `TestManagerRootCanPostToAnotherSessionWithoutSendingAReply` now resolves from the pre-read copy and checks that a reopen against an unseen resolution conflicts.

- Check: `app/human_interventions.go:240` `if prev.Version != version { return store.ErrConflict }`.
- Unrelated bump: `read` writes through `PutHumanIntervention` (`human_interventions.go:247-267`),
  which runs `version=version+1` even when `ReadAt` was already set
  (`adapters/store/human_interventions.go:172`); reads come from other devices/relay
  (`web/console/src/cloud/relay-writer.ts:943`).
- What the person sees: 409 on resolve/withdraw from a copy loaded before the read.
- Fix: make `read` not advance the version (or skip the write when `ReadAt` is set); keep the
  check for resolve vs. reopen vs. withdraw racing each other.

### 9. Login-profile and connector-credential actions compare the whole Bot version — low, confidence high

**Status (2026-11-25): fixed.** Both `login_profiles.go` and `extensions.go` no longer compare `Bot.Version`; fence, authority epoch, project, agent and connector-selection checks remain. Test: `TestLoginProfileActionSurvivesBotActivity` (connector credentials share the change but have no new test).

- Checks: `app/projectassistant/login_profiles.go:106`, `extensions.go:100`
  `if current.Version != q.ExpectedVersion`.
- Client re-reads the Bot right before sending (`LoginProfiles.tsx:26`, `ConnectorCredential.tsx:19`),
  so the window is small, but a turn committing in that window still refuses the action. These are
  already fenced by authority epoch, source checks and the conversation fence (`login_profiles.go:110-115`,
  `extensions.go:103-109`).
- Fix: drop the `Bot.Version` comparison or use `SettingsVersion`; keep the fence and authority.

### 10. Squad entities share one global catalog version — low, confidence medium

**Status (2026-11-25): not done.** Still holds: `saveSquadEntitiesTx` compares the singleton catalog version. A per-entity check needs each entity's base active version on the wire (a contract change); with no background writer and an existing client retry, it was left for a later change.

- `adapters/store/squad.go:399` compares the singleton `squad_catalog_state.version`, bumped by any
  entity save or package adoption (`squad.go:446-447`, `squad_package_adopt.go:272`). Saving skill A
  makes an open draft of skill B conflict. Person-only, no background writer; the client has a
  retry path (`pages/Squad.tsx:372`). Fix: per-entity version.

## Checks judged legitimate — keep them

- Deleting a Bot while it is working: `service.go:981` `delete_busy`
  (「這個 Bot 還在工作，請先停止再刪除。」). Destructive; the refusal is the point.
- Board v1 placement: `app/board_work.go:316`; the only other writer is `applyRules` moving the same
  placement (`:207`), and the refusal returns the current view.
- Project files and ops documents: token is the SHA-256 of the content
  (`adapters/projectfiles/files.go:521`); only content changes move it. Draft is kept
  (`ProjectFiles.tsx:71`). Small UX gap only: 「重新讀取」 is disabled while dirty (`:184`).
- Board settings revision (`adapters/board/settings.go:153`): person-only writers.
- Groups: version changes only on edit, leave, delete (`domain/projectassistant/groups.go:254,287,301`).
- Artifact titles: per-artifact `TitleVersion` (`artifact_titles.go:99`).
- Message reactions (`reactions.go:59`) and tower floors (`tower-floors.ts:60,68`): per-item tokens,
  and the client re-reads and recovers.
- Fences and generations (`StaleGeneration`, binding/generation checks in `repository.go:461-468`):
  they stop writes into a replaced conversation/session, which is a real boundary.
- Idempotency receipts (`store/receipts.go`), `pa_outcome_unknown` holds that refuse to re-send an
  effect whose result is unknown, and permission/authority epochs: replay and authorization guards,
  not concurrency over-reach.
- Team management `SettingsVersion` checks (`team.go:31,56`): a manager plan approved against
  specific settings must not apply to different ones.
- Work v2 cancel/complete/reopen/assign/unassign conflicts after a phase or owner change (see 1).

## Categories with nothing found

- Cursor/frontier expiry (`pa_cursor_expired`, `ports.go:262`): only refuses an event resume and makes
  the client reload a snapshot; no write is refused by it.
- Background refresh overwriting a draft: none found. The observed failure is the reverse — live
  props advance while the captured base stays frozen (findings 2, 4, 5, 7).
- Cloud/relay and transcript revisions (`adapters/cloud/bot_network_api.go:276`,
  `bot_routine_hooks.go:142`, `pairing.go:154`, `transport/http/transcript.go`): machine protocol or
  read-delta checks, not person saves.

## Suggested order of work

1. With the Bot settings fix already in progress: stop using `Bot.Version` as an edit guard
   anywhere a person edits (findings 2, 3, 7, 9). Either field-compare server-side or use a
   settings-only token; if `SettingsVersion` is chosen, first exclude `BackgroundThreads` and the
   roster fields from what bumps it.
2. Work v2 `Edit` field-level base (finding 1) — the highest-traffic case.
3. Entry-level memory revision and routine edit revision (4, 5).
4. Stop bumping versions for read markers (6, 8).
