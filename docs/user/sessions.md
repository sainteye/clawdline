# Watch, answer, start, stop and close sessions

After this page you can tell at a glance which Claude Code or Codex session needs you, read its
transcript, answer its question or send it more work, attach a picture, start a new session in a
project, stop the turn it is on, and close it safely.

## Before you start

- The daemon is running and the console is open ([install.md](install.md)).
- To type into sessions, the browser needs write permission: `clawdline open --send` on this
  machine, or a paired device that is allowed to act ([remote-access.md](remote-access.md)).
- Sessions are found in tmux on macOS and Linux, and in iTerm2 on macOS. Windows cannot list
  sessions yet ([platforms.md](platforms.md)).

The console's interface is in Traditional Chinese, the only language it ships so far. Labels below
are given as they appear, with their meaning in parentheses.

## Read the list

Open **Session 清單** (sessions). Each row is one session, marked with its project's pixel icon,
its directory and its terminal.

| On the row | Meaning |
| --- | --- |
| 🙋 **在等你回答** (waiting for you), row highlighted | The session asked a question or needs permission. These rows move to the top |
| A spinner | Working |
| No state word | Idle |
| 對話還沒開始 | The conversation has not started yet |
| A note that the state could not be identified | Clawdline read the screen but could not tell the state. It says so instead of guessing idle |

A second badge says where the work stands: **可接新工作** (can take new work), **自行推進中**
(moving on its own), **欠一個決定** (a decision is owed), **已交付** (delivered) or **Clawdline
已驗證落地** (landed, verified by Clawdline). A session's own claim is marked **自述**
(self-reported). The header counts working, waiting and unreadable sessions.

Type in the filter box, or press `/`, to narrow the list by task, project or terminal. Esc clears
it. A child session dispatched by another one is shown under its parent, marked └.

## Open a session

Click a row. The detail shows the real transcript, read from what Claude Code or Codex wrote. Press
`r` to reverse its order; **設定** (Settings) → **對話記錄** (transcript) sets the order for every
session.

The **⋯** menu (**Session 快捷動作**) holds the rest:

- **Session 資訊** (session info): title and directory, **編輯 session 標題** (rename), context
  use, token use, **切換模型** (switch model) and, when the assistant reports them, plan limits.
- **即時畫面** (live screen): the terminal as it is right now.
- **我傳出的訊息** (my messages), **常用句** (snippets), **Git** (changes, commit, push — commit and
  push ask first).
- **停止目前的工作（Esc）** and **關閉 session**, below.

## Answer and send

- **When the session asks a question**, a card above the message box lists its options. Tap one:
  the choice goes to the session as key presses, as if you had pressed it in the terminal.
- **To send text**, type in the message box and press **送出** (send), or Return. Shift-Return
  starts a new line. On a touch screen, Return makes a new line and you send with the button.
- **Pictures:** press **+** (**附一張圖**, attach a picture), paste one anywhere on the page, or drag
  it onto the session. Tap the thumbnail to open **標記要修改的地方** (mark what to change) and circle
  things with **紅筆** (red pen) before sending.
- **Snippets** (**常用句**) are text you send often. Create them with **新增常用句**, for this project
  or every project. Pressing one only fills the box; you still send it.
- **Skills:** type `/` (or `$` for Codex) as the whole message to list the session's skills and
  commands. Choosing one writes it into the box and does not send.

**Check:** your message appears in the transcript, and the row goes back to working.

## Dictate

Press the microphone (**用說的寫一則訊息**, dictate a message), speak, and press it again
(**停下來，轉成文字**) to turn it into text in the box. Transcription runs on the machine, not in a
cloud service, so the machine needs whisper.cpp's `whisper-cli` and one `ggml-*.bin` model, for
example in `~/.cache/whisper`. Browsers only allow the microphone on a secure page: `127.0.0.1`,
the tunnel's HTTPS address, or the hosted console.

## Start a session

1. Press **+** (**開一個 session**, start a session) in the list header.
2. Choose where under **要在哪裡開？** (where should it start). The list holds folders where Claude
   Code or Codex has run, folders of live sessions, and folders you added with
   `clawdline project add` ([projects.md](projects.md)).
3. Choose **Claude Code** or **Codex** under **要用哪一個開？** (start with), or resume a past
   conversation under **接續之前的** (pick up an earlier one).

**Check:** the sheet says **開好了。等它出現⋯** (started, waiting for it to appear) and the new
session shows up in the list. With several machines on a Cloud account, **機器** (machine) chooses
where it runs.

You can also press the microphone in the header (**說要開什麼**, say what to start) and describe the
task; Clawdline drafts the project, assistant, model and first message, and nothing starts until you
press **開始** (start).

## Stop the current turn

**⋯** → **停止目前的工作（Esc）** sends Esc to the session's terminal. The session stops the turn it
is on once it reads the key; the conversation stays open. It works from a phone as well.

## Close a session

**⋯** → **關閉 session**, or on a phone, swipe the row left and press **關閉 Session**. The dialog
first says whether closing is safe:

- **可以安全關閉** (safe to close): the button reads **關閉 Session**. The assistant quits, then its
  terminal tab closes.
- **安全關閉** (close safely): only the session's own confirmation is missing, and nothing on the
  Board or its to-dos is open.
- **還有 N 項未了結** (N obligations remain): the session still owes a delivery, a landing or a
  to-do. **回到 session 檢查** takes you back to look; **仍要關閉** (close anyway) closes it after
  a second, explicit press.
- **無法判斷能否關閉** (closeability unknown): Clawdline cannot tell, and does not close it.

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
