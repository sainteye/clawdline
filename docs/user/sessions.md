# Watch, answer, start, stop and close sessions

After this page you can tell at a glance which Claude Code or Codex session needs you, read its
transcript, answer its question or send it more work, attach a picture, start a new session in a
project, stop the turn it is on, and close it safely.

## Before you start

- The daemon is running and the console is open ([install.md](install.md)).
- To type into sessions, sign in and pair the browser with the target machine. This includes
  devices previously issued read-only keys. Cloud also needs `clawdline cloud commands on` on
  that machine ([remote-access.md](remote-access.md)).
- Sessions are found in tmux on macOS and Linux, and in iTerm2 on macOS. Windows cannot list
  sessions yet ([platforms.md](platforms.md)).

Labels below follow the English interface; the console also supports other languages.

## Sessions and access

In **Settings → Sessions and access**, both switches start enabled. Select the ⓘ button beside
one to expand its On, Off, and scope explanation. These are settings of the selected machine,
including when changed from a paired Cloud browser with Send permission.

- **Use full permission by default for dispatch**: off changes future dispatches that omit a
  permission mode to the assistant’s own permission settings, without adding full-permission arguments.
  An explicit task mode takes precedence; existing tasks keep their mode.
- **Automatically discover other Sessions**: off keeps Sessions opened by Clawdline and stops
  adding externally opened conversations. It takes effect on the next inventory refresh, without
  closing conversations, deleting retained records, or changing pairing or Cloud command permission.

## Read the list

Open **Sessions**. Each row is one session, marked with its project's pixel icon,
its directory and its terminal.

| On the row | Meaning |
| --- | --- |
| 🙋 **Waiting for your answer**, row highlighted | The session asked a question or needs permission. These rows move to the top |
| A spinner | Working |
| No state word | Idle |
| Conversation not started yet | The conversation has not started yet |
| A note that the state could not be identified | Clawdline read the screen but could not tell the state. It says so instead of guessing idle |

A second badge says where the work stands: **can take new work**, **moving on its own**, **a decision is owed**, **Delivered** or **Landed, verified by Clawdline**. A session's own claim is marked **self-reported**. The header counts working, waiting and unreadable sessions.

Type in the filter box, or press `/`, to narrow the list by task, project or terminal. Esc clears
it. A child session dispatched by another one is shown under its parent, marked └.

## Open a session

Click a row. The detail shows the real transcript, read from what Claude Code or Codex wrote. Press
`r` to reverse its order; **Settings** → **Transcript** sets the order for every
session.

The **⋯** menu (**Session actions**) holds the rest:

- **Session info**: title and directory, **Edit session title**, context
  use, token use, **Switch model** and, when the assistant reports them, plan limits.
- **Live screen**: the terminal as it is right now.
- **My messages**, **Snippets**, **Git** (changes, commit, push — commit and
  push ask first).
- **Stop current work (Esc)** and **Close session**, below.

## Answer and send

- **When the session asks a question**, a card above the message box lists its options. Tap one:
  the choice goes to the session as key presses, as if you had pressed it in the terminal.
- **To send text**, type in the message box and press **Send**, or Return. Shift-Return
  starts a new line. On a touch screen, Return makes a new line and you send with the button.
- **Pictures:** press **+** (**Attach a picture**), paste one anywhere on the page, or drag
  it onto the session. Tap the thumbnail to open **Mark what to change** and circle
  things with **Red pen** before sending.
- <a id="pictures-from-the-agent"></a>**Pictures from the Agent:** an Agent shows you a picture — a
  screenshot, a design, a chart — by running `clawdline image send <picture>` in its Session. The
  picture appears under the reply that sent it; open its thumbnail to inspect it. An Agent that has
  loaded the Clawdline skill knows the command; asking for "the screenshot" or "send me the picture" is enough.
  The picture is stored on the Session's machine for 24 hours; an expired one stays visible as an
  expired tile. A paired Cloud browser needs read permission. If an Agent says it sent one and you
  see nothing, see [Troubleshooting](troubleshooting.md#sessions).
  [How a picture finds its reply](../messages.md#pictures-a-session-shows-the-person).
- **Snippets** are text you send often. Create them with **New snippet**, for this project
  or every project. Pressing one only fills the box; you still send it.
- **Playbooks** (called Workflows before 2026-10-23) hold a complete launch message. Open the
  Playbook tab, select one, then press **Start Playbook**. The shipped **Zero-Based Review** example reviews the current
  Project and is available in every Project. Its unedited text follows the interface language;
  you can edit or delete it like another Playbook. A deleted example stays deleted after restart.
- **Skills:** type `/` (or `$` for Codex) as the whole message to list the session's skills and
  commands. Choosing one writes it into the box and does not send.

**Check:** your message appears in the transcript, and the row goes back to working.

## Attention notes from Agents

An Agent can leave a concrete request for you above a Session conversation: something to read,
answer or do. Open the **Attention** control in the Session header when it has a red dot.
The card explains the action and why the Agent needs you. With multiple suggested replies,
tapping one sends that message to the Session and resolves the note after the send is accepted.
With one suggestion or none, the button fills the message box; you can edit it and press
**Send** yourself. You can also mark a note **Move to handled**, which clears it without
sending or approving anything. The Agent can continue unrelated work while the note waits.

These notes are separate from a terminal permission prompt and from a Board decision with a
deadline. A note alone does not send a push notification; if the Agent is blocked, its Session
still needs a waiting state and an attention notification. [Human intervention notes](../human-interventions.md)

## Dictate

Press the microphone (**Dictate a message**), speak, and press it again
(**Stop and transcribe**) to turn it into text in the box. Transcription runs on the machine, not in a
cloud service, so the machine needs whisper.cpp's `whisper-cli` and one `ggml-*.bin` model, for
example in `~/.cache/whisper`. Browsers only allow the microphone on a secure page: `127.0.0.1`,
the tunnel's HTTPS address, or the hosted console.

## Start a session

1. Press **+** (**Start a session**) in the list header.
2. Choose where under **Where should it start?**. The list holds folders where Claude
   Code or Codex has run, folders of live sessions, and folders you added with
   `clawdline project add` ([projects.md](projects.md)).
3. Choose **Claude Code** or **Codex** under **Start with**, or resume a past
   conversation under **Pick up an earlier one**.

Codex history loads twenty provider records at a time. Choose **Load older sessions** to
continue; a page containing only excluded background conversations still offers the next page.
If a read fails, **Retry** keeps the sessions already loaded and retries the same page. A read
failure does not mean a conversation was deleted. Resuming checks the selected conversation
directly rather than listing the project's entire history again. Provider subprocesses retain
their two-process concurrency cap, eight-second per-response timeout and twenty-second total
deadline; retries happen only when requested.

**Check:** the sheet says **Started. Waiting for it to appear…** and the new
session shows up in the list. With several machines on a Cloud account, **Machine** chooses
where it runs.

You can also press the microphone in the header (**Say what to start**) and describe the
task; Clawdline drafts the project, assistant, model and first message, and nothing starts until you
press **Start**.

**Settings** → **Defaults for new sessions** offers the models available to Codex and Claude Code
as separate machine-wide pickers. The Claude Code choices follow that machine's installed `/model`
catalog and account cache, including its versioned legacy choices, instead of a Clawdline-owned list.
Choose **Assistant default** to leave that assistant in charge. The setting applies only when Clawdline
opens a new session without another model choice; a model chosen for one session still
wins, a child agent may explicitly choose any admitted model for its own task, and resuming an
existing conversation does not change its model.
The same two fields appear in Clawdline Cloud on a phone. The phone can read them through its paired
machine; saving a change also requires that machine's Cloud command switch and this browser's local
operation grant. See [Let paired devices act](remote-access.md#let-paired-devices-act).
The same setting is available without a browser as
`clawdline setting set codex_default_model gpt-6-sol` (or
`claude_default_model claude-opus-5-5`); use `default` as the value to hand the choice back to the
assistant.

To change the model of an existing Codex session, open its info card and press any other model, or
send `/model` alone in its composer. Pressing a model closes the info card and opens the menu Codex
draws above the composer; choose the model there. Codex does not switch models through
`/model <name>`, so the info card sends `/model` for every Codex model press. Claude Code's info
card continues to send its model choice directly.
After choosing a Codex model, its reasoning-level menu is also shown as choices in the console.
Choose the reasoning level there; a model change is not finished merely by opening its first menu.

## Stop the current turn

**⋯** → **Stop current work (Esc)** sends Esc to the session's terminal. The session stops the turn it
is on once it reads the key; the conversation stays open. It works from a phone as well.

## Close a session

**⋯** → **Close session**, or on a phone, swipe the row left and press **Close Session**. The dialog
first says whether closing is safe:

- **Safe to close**: the button reads **Close Session**. The assistant quits, then its
  terminal tab closes.
- An idle Session with complete identity evidence and no open Board item, to-do, child work, or
  self-declared debt is **Safe to close** without an extra attestation.
- **N obligations remain**: the session still owes a delivery, a landing or a
  to-do. **Review session** takes you back to look; **Close anyway** closes it after
  a second, explicit press.
- **Closeability unknown**: Clawdline cannot tell, and does not close it.

Swiping never closes anything by itself; it opens the same dialog.

From a terminal, `clawdline interrupt <session-id>` and `clawdline close <session-id>` press Esc or
close the terminal directly, **without** these checks. Use them for testing, not as the normal way
to close a session.

## Troubleshooting

- A session is missing, or you cannot type into one: [troubleshooting.md](troubleshooting.md).
- Dictation says whisper is not installed or has no model: install whisper.cpp and put a model in
  `~/.cache/whisper`.

## Deeper

- [keyboard-shortcuts.md](keyboard-shortcuts.md)
- [architecture.md](../architecture.md) — how sessions are discovered and read.
- [english-on-a-chinese-screen.md](../english-on-a-chinese-screen.md) — the English still left on
  the Chinese screen.
