# Board, session to-dos and Verify

After this page you can put a piece of work on a project's Board, hand it to a Claude Code or Codex
session, and follow it through implementation, verification, merge and deployment; keep a
session's own to-dos; and keep a list of changes waiting to be verified on **Verify**.

## Availability

All of this is local and free. The Board is off until you turn it on.

Labels below follow the English interface; the console also supports other languages.

## Turn the Board on

1. Open **Settings**.
2. Find **Enable Project Board** and press the button so it reads **Enabled**.

**Check:** the block says **Saved**, and **Board** in the menu opens a board with
**＋ Create item**. Turning it
off again keeps the history.

## Follow work on a desktop

The desktop Board places **Unassigned**, **In progress**, and **Needs attention**
beside each other. Implementation, verification, merge and deployment appear on the
card, so short phase changes do not move it between columns. A question or blocker
keeps its phase visible in **Needs attention**. Press a card to open its details.

Turn on **Show Agent proposals** or **Show Plans** to reveal their columns on the
left. **Show completed** reveals completed items on the right. All three start off;
a hidden column leaves no empty space. Each column keeps a fixed width, and the
Board scrolls horizontally inside its own region. The heading and filters stay in place.
The phone Board keeps its existing layout and controls.

## Put work on the Board

1. Open **Board** and press **＋ Create item**.
2. In **Create Board item**, choose the Project and the kind (**Type**):
   - **Feature** and **Issue** go to **Unassigned** and can be assigned to a session.
   - **Epic** is assignable like Feature and Issue; **Refactor** and **Plan** stay in **Planning**.
3. Give it a title (**Title**), description (**Description**) and **Acceptance criteria (Markdown)**. You can dictate the description and add
   up to six reference pictures. If the description has two or more top-level list items, they
   become the item's steps when it is assigned.
4. Press **Create**.

## Assign it to a session

On the card, either choose an existing session (**Choose existing Session**) and press **Assign**, or choose
Claude or Codex and press **Start a new … Session** to open a new one. **Go to Session** takes you to it, and
**Remind Session** reminds it of the item.

From then on the session moves the item itself, and each move needs evidence:

In the console's **Settings → Planning and verification gates** block, planning is on by default and independent
verification is off. The macOS app also shows the same pair in **Clawdline Next → Settings… →
Dispatch work to other sessions**.
The two values are captured on the item's first successful assignment in each cycle; changing the
switches later affects future cycles. With planning on, Feature and Epic need observable acceptance
criteria before implementation. An Epic needs a plan and independent review; a Feature needs those
only when its **Needs independent review** switch is on. An Issue is exempt. Planning off also lets
an Epic skip forced planning. With independent verification on, the card shows the latest result, and
detail shows
bounded evidence or why a claim could not be verified. `NEEDS_WORK`, stale evidence, and technical
failure never count as PASS. Escalations show whose decision it is; only a **waiting for you**
decision exposes person controls. AI, person, and technical overrides are labeled as overrides.
When evidence detail fills, use **Verification detail capacity and export** to download and verify the exact export, confirm
that it is saved, then clear eligible old detail. The latest state and lifetime counts remain.

| Phase | On the card | The agent must show |
| --- | --- | --- |
| implementing | Implementation | — |
| verifying | Verification | — |
| merging | Merge into Git | how it was verified |
| deploying | Deploy | a verified landing of the commit |
| done | Done | every step completed |

Each card carries a **Token bill**: what the work cost so far
([usage.md](usage.md)).

**Epics and who made a card.** A card split out of an Epic names it under its summary as
**↑ Epic〈title〉**, with **done** or **cancelled** when the Epic is closed; pressing it opens the
Epic's detail, and each row under **Child items** in an Epic's detail opens that child the same way.
A card a session made rather than you carries an **Agent-made** badge and a dashed top edge:
either a session created it from one of your messages (**Created by a Session from your HH:MM message**), or the
Epic's owning session split it out of the Epic (**Split out by the Epic's owner Session**). Your own
items carry no badge.

**Proposals.** A session can propose an item. **Agent proposal** uses plain language in a compact review
row: what the session proposes, why it should be done, its Project and source. Press
**Explain** to reveal **What will change** and **What you will see when it is done**. Press **Accept and create** or **Reject**; nothing is accepted for you.

**Questions.** When the owning session needs an answer, the question and its answer buttons appear
inside that Board item's card under **Needs your decision**. The item title and description stay beside the
question, and the card says which answer becomes the default at the deadline. A new question cannot
be created without an open item owned by that session.

## Session to-dos

Open a session and use the **Session to-dos** fold: it lists the Board items that session owns, the
ones it recently finished, and direct to-dos. Press **+** to add one. Press an empty checkbox to
complete a direct to-do, or its checked checkbox to restore it as unfinished.

A direct to-do is a pull queue for future work. Adding one records your intent without sending a
conversation message or interrupting the current turn. The Session can pick it up when it next
checks its work. An idle Session cannot poll its to-dos by itself: if you need it to act now, use
**Send** in the conversation instead. Keep larger, independently tracked work on the Board.

A session adds its own to-dos only when you ask it to, with:

```sh
clawdline todo add "Write the migration" "Update the README"
clawdline todo list
clawdline todo done <to-do id>
```

## Verify: changes waiting to be verified

Some changes can only be judged later: lower a setting and see a week later whether it helped.
**Verify** (headed **Waiting to be verified**) keeps each such change with
**Why**, **What counts as it holding**, **Data**,
**Notes** and **Verdicts**.

- Mark each criterion **Mark passed** or **Mark failed**, add notes with **Add note**,
  and finish with **Accept** or **Reject**; a reason is required.
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
