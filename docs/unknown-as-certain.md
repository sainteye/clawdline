# Seventeen places that still draw "I do not know" as a fact

Measured 2026-09-21 against `web/console/src` (excluding `legacy/js/**` and
`public/strings/**`, which are byte-locked copies) and `internal/`. It
continues `first-run-audit.md` §U4, which named the family and gave it a
whole-class acceptance; four of those are fixed and are not repeated here.

**The guard for this family reports zero.** `web/console/src/refusals/scan.ts`
finds no unanswered refusal in the tree today, and every finding below is a
shape it cannot see: a `catch` that only sets a boolean, a reject that returns
an empty array, a literal `0`, or a reason already dropped on the Go side
before TypeScript could have it. A green guard is not the same as a finished
family — which is the same lesson as the rest of this list, applied to the
list's own instrument.

| # | Where | What a person is shown | Why it is wrong |
|---|---|---|---|
| 1 | `App.tsx:736`, `:884` | `沒有 session` | The header counts `fleet.snapshot?.sessions ?? []`, so a first load and a stopped daemon both read as "none". `Dashboard.tsx:336` was fixed to `fleet.loaded ? … : "—"`; the header was not. |
| 2 | `cloud/CloudGate.tsx:285-288` | `這個帳號還沒有任何機器回報。` | `.machines()` rejects into `setMachines([])` and the reason is dropped. `unauthorized`, an undecryptable envelope and a relay timeout all become "you have no machines", sending the reader to set up a machine when the answer is to pair or sign in again. |
| 3 | `pages/work/Board.tsx:82`, `:93` | `讀不到看板。` above, `0` and `這一區目前沒有東西。` below | `board === null` and a board that really is empty take the same path, so one screen says both. |
| 4 | `cloud/relay-reader.ts:592-599` | `已連上` | `/v1/health` answers `ok` whenever the relay socket is up. That boolean is the browser's line to Cloud, not Cloud's line to the machine: a `stale` machine reads as connected while its list never arrives. |
| 5 | `legacy/devices-bridge.ts:190-191` | `在線`, `已與這個瀏覽器配對` | This machine's own card hard-codes `freshness: "current"` and `pairing: "local"` and never reads health, so it says "online" after the daemon has stopped. |
| 6 | `session/ScreenPanel.tsx:127`, `:195` | `讀不到那個畫面` | Both rejects keep only `setFailed(true)`. `session_not_found`, `forbidden`, `cloud_not_carried` and a dropped network are four different next steps flattened into one sentence. |
| 7 | `web/core/src/refusal.ts:46-53` | `…（unexpected_error）` | `isRefusal` accepts only the flat `{error: string}` shape. The daemon's gate, actions, focus and dispatch answer the nested `{"error":{"code":…}}`, so every named refusal from them is thrown as a transport error and described as `unexpected_error`. `session/sender.ts:88-102` already reads both. |
| 8 | `pages/now/shared.ts:24-25` | `這一塊讀不到。（unexpected_error）` | Downstream of 7: a daemon that is not running is "offline", not "unexpected". The file's own comment says the formatter separates them; it does not. |
| 9 | `pages/schedules.tsx:650` → `:559` | `連不到 Clawdline。它還在那台 Mac 上跑著嗎？` | The catch keeps a boolean, so a 500 and a 403 both accuse the daemon of being gone — and Create stays pressable on a list that could not be read. |
| 10 | `session/Transcript.tsx:328` | `送不出去（machine_offline）` + `再試一次` | The card builds its own sentence instead of `failureSentence`, so the actionable words written for `machine_offline`, `forbidden`, `cloud_read_only` and `rate_limited` are never used, and Retry is offered for codes that can never succeed. `Composer.tsx:411` was fixed; the card was not. |
| 11 | `adapters/terminal/tmux.go:72-74` | `找不到在跑 Claude Code 的分頁`, claimed authoritative | A failed `LookPath` appends a note and returns with `Complete` still true. Started from Finder, where launchd's PATH has no `/opt/homebrew/bin`, a machine with tmux running reports an empty inventory as complete. `capabilities.go:57-61` already has the true sentence. |
| 12 | `terminal/tmux.go:103`, `iterm_darwin.go:143`, `:155` | `在等這台機器的清單`, forever | A whole source failing writes its reason into `inv.Notes`, and `/v1/sessions`' `Scan` has no field to carry it. Nothing in the non-legacy console reads `scan.sources` or `gaps` either. Automation being denied to iTerm2 is a Settings panel the reader is never told about. |
| 13 | `adapters/analytics/worktrees.go:135-136` | `這個專案底下沒有任何 worktree 完成過 Feature。` | `featureRows: 0` and an empty list are literals. This daemon does not record feature attribution at all, so "not measured" is drawn as "measured, and none". |
| 14 | `adapters/analytics/query.go:1150-1151` | `reviewReceipts: {status: "complete", read: 0}` | The same literal, nested, and worse: it claims to be complete. Nothing was scanned — `classifier.configured` is false next to it. |
| 15 | `cloud/CloudGate.tsx:576` → `:1089` | `Clawdline Cloud 沒有回應（offline）` | A `fetch` TypeError has no code and is given `offline`, which names Cloud as the subject. Nothing in the tree reads `navigator.onLine`, so a phone with no signal blames the server. |
| 16 | `cloud/CloudGate.tsx:1215` | `有一台機器的資料在這裡讀不出來（unknown_sender）。` | The machine's id is not kept with the code, so with five machines listed the sentence names none of them and asks for nothing. |
| 17 | `pages/work/shared.ts:81` | `…store_unavailable（store_unavailable）` | The fallback passes the code into a sentence that already appends it. `pages/now/shared.ts:14-18` carries a comment about exactly this; the board did not follow it. |

Two more are in `legacy/js/**` and cannot be touched while that directory is
compared byte for byte: the ledger drawing `usage_analytics_busy` as a fault
with no retry (`view/ledger.js:386-387`, whose daemon side is correct) and a
device card's `0 Session 清單` (`view/devices.js:46-47`).

`CloudGate.tsx:1138-1141` is fixed and struck from the audit's list: unread and
unknown session counts now say which they are.
