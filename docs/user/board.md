# Board, session to-dos, Now and Verify

After this page you can put a piece of work on a project's Board, hand it to a Claude Code or Codex
session, and follow it through implementation, verification, merge and deployment; keep a
session's own to-dos; see what is in flight on **Now**; and keep a list of changes waiting to be
verified on **Verify**.

## Availability

All of this is local and free. The Board is off until you turn it on.

The console's interface is in Traditional Chinese, the only language it ships so far. Labels
below are given as they appear, with their meaning in parentheses.

## Turn the Board on

1. Open **設定** (Settings).
2. Find **啟用看板系統** (enable Project Board) and press the button so it reads **已啟用**
   (enabled).

**Check:** **看板** (Board) appears in the menu. Turning it off again keeps the history.

## Put work on the Board

1. Open **看板** and press **＋ 建立項目** (create item).
2. In **建立看板項目**, choose the Project and the kind (**類型**):
   - **Feature** and **Issue** go to **待指派** (unassigned) and can be assigned to a session.
   - **Epic**, **Refactor** and **Plan** stay in **規劃區** (planning) and are not assigned.
3. Give it a title (**標題**) and a description (**描述**). You can dictate the description and add
   up to six reference pictures. If the description has two or more top-level list items, they
   become the item's steps when it is assigned.
4. Press **建立**.

## Assign it to a session

On the card, either choose an existing session (**選擇既有 Session**) and press **指派**, or choose
Claude or Codex and press **開新 … Session** to open a new one. **前往 Session** takes you to it, and
**提醒 Session** reminds it of the item.

From then on the session moves the item itself, and each move needs evidence:

| Phase | On the card | The agent must show |
| --- | --- | --- |
| implementing | 實作 | — |
| verifying | 驗證 | — |
| merging | Merge 回 Git | how it was verified |
| deploying | 部署 | a verified landing of the commit |
| done | 已完成 | every step completed |

Each card carries a **Token 帳單** (token bill): what the work cost so far
([usage.md](usage.md)).

**Proposals.** A session can propose an item. It waits under **Agent 提案** until you press
**接受並建立** (accept) or **拒絕** (reject); nothing is accepted for you.

## Session to-dos

Open a session and use the **Session 待辦** (session to-dos) fold: it lists the Board items that session owns, the
ones it recently finished, and direct to-dos. Press **+** to add one.

A session adds its own to-dos only when you ask it to, with:

```sh
clawdline todo add "Write the migration" "Update the README"
clawdline todo list
clawdline todo done <to-do id>
```

## Now: where things stand

**現在** (Now; its heading is **現在是什麼狀況**, where things stand) collects what is in flight on
this machine: **正在做** (in progress: dispatched tasks that have not finished) and **交了還沒記帳**
(delivered, not recorded: finished work whose landing is not yet recorded; see
[clawdfather-and-dispatch.md](clawdfather-and-dispatch.md)). A source that could not be read says
so; it is never drawn as zero. Press **重新讀一次** to read again.

## Verify: changes waiting to be verified

Some changes can only be judged later: lower a setting and see a week later whether it helped.
**驗收** (Verify; its heading is **等待驗收**, waiting to be verified) keeps each such change with
**為什麼** (why), **怎樣算成立** (what counts as it holding), **資料** (the data it is judged by),
**紀錄** (notes) and **結論** (the verdict).

- Mark each criterion **標為通過** (passed) or **標為未通過** (failed), add notes with **加入紀錄**,
  and finish with **驗收通過** (accept) or **不通過** (reject); a reason is required.
- From a terminal or a session:

  ```sh
  clawdline verify add --title "Earlier compaction" --due 7d \
    --criterion "cost per task is lower" --why "compaction window lowered"
  clawdline verify list
  clawdline verify note <id> "first week looks cheaper"
  clawdline verify done <id> --accepted "held for two weeks"
  ```

  `--due` takes `7d`, `36h`, a date, or a date and time.

## Deeper

- [work-system-v2.md](../work-system-v2.md) — kinds, phases, authority and evidence.
- [board-redesign.md](../board-redesign.md) — board, backlog and session to-do as three structures.
- [verifications.md](../verifications.md) — the verification record and its CLI.
