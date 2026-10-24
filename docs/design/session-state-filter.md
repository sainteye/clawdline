# Session state filter in the header

The person selected **C** on 2026-10-21, with one amendment: the header keeps its existing
counts and colors while showing all Sessions; only an applied filter names its state in orange.
The implementation uses All / Attention / Working / Idle, applies a single choice immediately,
and keeps Refresh in the dialog footer. Local and all-machine views share this control. A reload
starts at All. The original three-direction comparison is retained below as design history.

The interactive comparison is `docs/design/session-state-filter.html`. The script-free historical
Cloud reading copy is `docs/design/session-state-filter-preview.html`.

## Filtered empty results

Show one compact card with a state-specific sentence and a Show all Sessions button. The fleet
list must not also render its generic no-match sentence. Keep the schedule section independent
below it. Both local and fleet views share the same result card; an incomplete fleet reading
keeps its uncertainty sentence instead of claiming that no Session needs attention.

## Dashboard inspection keeps the current scope

From All machines, choosing a machine for the wordmark dashboard opens a pinned usage read.
It does not choose the console's machine, repoint its active reader, change the URL, or remount
the fleet list. Closing the dashboard preserves the list's search, state filter and group state.
The dashboard names the inspected machine. An explicit Open Clawdfather press is a separate
navigation action; its destination still belongs to the inspected machine.

## What changes for the person

- The header's count area (`Counts` in `web/console/src/App.tsx`) stops opening the machine
  dashboard and opens a "which Sessions are shown" filter instead. The dashboard moves to the
  `clawdline` wordmark; the mascot stays the drawer handle. That move is handled separately by the
  root and is only drawn here.
- The row under the fleet toolbar (`.cloud-all-status-filters` in
  `web/console/src/cloud/FleetSessionList.tsx`: Refresh / 需要注意 / 工作中 / 靜置) is removed. Its
  status choices move into the filter; refresh gets a new home that each design places differently.

## Sources read, and what they say

| Source | Fact the design depends on |
|---|---|
| `web/console/src/App.tsx` `Counts` (~L1178) | Counts total / working / waiting / not-started / shells / unreadable from `SessionRow.state`; on narrow screens `legacy/responsive.css` hides the quiet parts; clicking calls `setMachine(true)` (dashboard). |
| `web/console/src/cloud/fleet-filters.ts` | `FleetFilter = "all" \| "attention" \| "working" \| "idle"`, single value. `attention` = `waitingForReply \|\| needsAttention`. `working` = (`state === "working"` or shells running) **and** `freshness === "current"`. `idle` = `state === "idle"`, no shells, current. |
| `FleetSessionList.tsx` ~L108, L300–L318, L376–L395 | `statusFilter` is component state, not persisted. Chips toggle between one value and `all`. With a filter set, machine groups with no matching rows are hidden. Refresh button has `idle/loading/done/failed` feedback and lives in the same row as the chips. |
| `web/console/src/session/SessionToolbar.tsx`, `Sessions.tsx` | The single-machine list has only a text filter; there is no state filter there today. |
| `web/console/src/cloud/all-machine-sessions.ts` | `freshness: "current" \| "stale" \| "unknown"`. |

Semantic caveats the implementation must keep:

1. **The categories overlap.** `attention` is orthogonal to `working`/`idle`: a Session can be
   both. Per-option counts therefore do not sum to the total; the modal says so.
2. **The state filters do not partition every row.** Non-current rows fail `working` and
   `idle`, but may still match `attention`, whose predicate does not require current freshness.
   A current row with an unknown state and no attention signals can also match none of these
   filters. The mock adds 狀態未更新 as a fourth option; that alone does not prove every real
   row will be covered when every box is selected. Keep an explicit show-all mode, and decide
   how unmatched rows should be represented during implementation. The additional category is
   a proposal awaiting approval.
3. **Local and fleet states are not the same vocabulary.** The local header counts
   `state === "waiting"`; the fleet `attention` reads `waitingForReply`/`needsAttention`. The
   design does not assume they are equivalent. Using the filter on the single-machine view needs
   an explicit definition per category on local `SessionRow` data before it ships.
4. **Counts are reliable only for settled readings.** In the all-machines view a machine still
   loading contributes nothing yet; the modal names the machine and says "還在讀取，數字可能偏少" rather than showing a
   confident total (toggle "模擬一台機器還在讀取" in the HTML).

## The three directions

All three keep controls ≥ 44 px, use the product's dark tokens (`legacy/tokens.css`) with the
orange `--accent`, and behave the same on one machine and on all machines; only the subtitle,
the counts and machine grouping change.

**A — Counts become the filter button; tick, then apply (recommended).**
The header still reads like today (on a phone the waiting count is moved first, so truncation drops
the less urgent part). The modal is a checklist with counts: bottom sheet on a phone, centred
dialog on desktop. Multi-select, union. The primary button states the outcome ("顯示 6 個";
"顯示全部" when nothing is ticked); 取消, ✕, Esc and the backdrop discard the draft. "全部顯示"
clears the ticks. While a filter is active the header shows the selected names and the shown
count with an orange outline. Refresh becomes a 44 px icon button at the end of the search row.

**B — Instant tiles, refresh inside the panel.**
Counts stay as quiet text with a filter button that carries a badge (number of selected states).
A panel drops from the header with four large tiles that filter immediately; the list updates
behind a light scrim. No apply, no cancel: 完成, ✕ and the backdrop only close. The first row of
the panel is Refresh with "last updated", next to the numbers it refreshes.

**C — Single-select view switcher.**
The header names the current view ("全部 9" / "需要注意 2"). A radio menu applies and closes on
tap. This is exactly today's `FleetFilter` behaviour, so it is the smallest change. It cannot show
two states at once, and 狀態未更新 stays reachable only through 全部. Refresh is at the foot of the
menu, plus pull-to-refresh on a phone.

**Why A.** The person asked "which states", which reads as several. A keeps the counts that the
header exists for, makes the consequence of a choice visible before it is made (the result count
on the button), and gives refresh a permanent one-tap place that does not depend on opening the
filter. B is faster for frequent toggling but has no way back from an accidental tap except
clearing; C is safest to build but drops multi-select.

**Empty result.** All three show "沒有符合「…」的 Session" in the list with a 全部顯示 button that
clears the filter. A zero-count option stays selectable; it is not disabled.

## Original implementation proposals (historical)

- The unselected A/B proposal would have changed `FleetFilter` to a set (`ReadonlySet<"attention" | "working" | "idle" | "stale">`), empty
  meaning all; `matchesFleetFilter` returns true for an empty set and ORs the members otherwise.
- The filter state must move up from `FleetSessionList` to where the header can read it (the
  header is in `App.tsx`), or the header entry must dispatch to the list. Counts per option should
  come from the same projected rows the list filters, so the button's "顯示 N 個" matches the list.
- Product copy changes need English and zh-Hant in the same change (`docs/localization.md`).
- The existing refresh feedback states (`loading/done/failed`) carry over unchanged.

## Original design questions (historical)

1. Which direction: A, B, C, or a mix (e.g. A's checklist with B's refresh-in-panel).
2. Multi-select with union semantics, with "show all" equal to clearing the selection — yes or no.
3. Add the fourth option 狀態未更新 (freshness ≠ current), keeping show-all as an explicit mode
   and separately handling any current rows that match none of the three existing filters.
4. Should the single-machine view get the same filter now, and if so how is 需要注意 defined on
   local `SessionRow` data (`state === "waiting"` alone, or also the attention signals)?
5. Should the selection persist across reloads, or reset to all each time the page opens?
