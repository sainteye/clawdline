# Use the Gemini personal preview

[繁體中文（台灣）](gemini.zh-TW.md)

Clawdline calls this assistant **Gemini** in the console. This preview runs the
Antigravity ACP CLI on the selected machine. Gemini CLI and Antigravity are
separate products; installing or signing into one does not establish that the
other is ready.

The preview appears for connected users when the machine owner has enabled
it. It uses an already signed-in personal Google account, with **AI Credits off**.
Keep those settings when using the preview. Free-account quota can still run out;
Clawdline does not switch to an API key or a paid fallback.

## Start and send

1. On the Session list, choose the machine, then press **Start a session**.
2. Choose **Gemini**, then the Project.
3. Type a message and press **Send**. Your message appears first; the assistant's
   text and tool activity arrive as that same native turn runs.

A send being accepted does not mean the model has completed its answer. Read the
conversation and current state before sending again if the connection drops. A
lost acknowledgement does not automatically resend your message.

If Gemini is absent, check that this machine has the personal preview enabled.
If startup reports a sign-in problem, sign into the selected machine's
Antigravity profile, then start or resume again. An empty conversation has no
assistant reply until you send a message.

## Read history and continue

Open the existing Session to read its saved conversation. Scroll upward and use
**Load earlier messages** when available to read older messages and tool results.
Repeated identical messages remain separate turns.

To return to a past native conversation, select **Pick up an earlier one** in
the start chooser and choose the exact conversation under its original Project.
Resuming keeps that conversation's identity; it does not create a replacement or
send a new message. Older native history may need to load before it is shown.

A daemon restart stops the native processes but preserves saved conversations.
When the machine is online again, resume the saved conversation and send an
explicit new message. A turn interrupted during restart is not automatically
repeated. Saved history remains readable while sign-in needs attention, provided
the owning daemon is online. An offline machine cannot supply fresh history or
accept a new message from the phone.

## Answer a tool permission request

When the native CLI asks for permission, the Session shows its question and
offered choices. Select the choice for the tool shown. An approval applies to the
scope named by that native choice, such as once or always; Clawdline does not
change a denial into an approval.

Clawdline does not impose a 45-second limit on a model turn or on waiting for
your permission choice. The question remains active until answered or the
underlying turn/process is cancelled. A short timeout still protects setup,
settings and pipe writes.

If the request changes or expires, refresh the conversation and read the current
question. Closing the conversation or cancellation does not approve its pending
tool. A device with read-only access can read the conversation but cannot answer
a permission request.

A native workspace restriction can stop a tool before any permission menu is
offered. In that case, read the failed tool and the assistant's explanation;
there is no pending approval for the console to answer.

## Models, reasoning and quota

Open **About this session** to see the native model choices and, when separately
offered by the CLI, **Reasoning effort**. Model and reasoning are separate
settings. An older native response that only supplies model variants keeps those
variants as model choices rather than inventing an effort control.

Quota is the native account reading, with its observation age and reset time
when available. Missing data is unknown, not zero usage. Context occupancy is
the current context reading, not a bill or a currency estimate.

## Roles and shared Project memory

On a machine that advertises role support, choose a role in **Open a session**.
The daemon uses the same immutable role, handbook and enabled-skill snapshot as
its other assistants, together with shared Project memory and the selected
language. Opening a blank conversation sends no model input; its first message
carries these instructions separately from the original user text.

A resumed conversation retains its original snapshot, even if the role settings
have changed. It does not inject a new snapshot or resend an interrupted message.
An older machine that does not support this path keeps its role picker hidden.

The native CLI currently flattens embedded instructions into its replayed user
text. Clawdline restores the original input only when that first replayed turn
matches its saved context and original text exactly. Unknown replay formats are
preserved rather than deleting text by guesswork.

Native tools use their own Clawdline Session identity. CLI dispatch can start a
Gemini child with the existing role squad, worktree, acceptance and result
workflow. Native model and permission choices must be acknowledged by the CLI.
Worker completion notices are recorded as read only after their own normal turn
completes; cancelled or limited turns do not create that receipt.

## Reading tool activity

Read and search activity shows the supplied filename and line range. Consecutive
activities can be expanded as a group, and each tool opens its received result.
Large results are read from saved history only when expanded. Closing a group
does not delete its history.

Tool calls use the same compact rows and fold controls as Claude Code and Codex.
Long commands stay on one line; open a row to read the full command and result.

Delegated activities show the roles requested by Gemini. Activities with an explicit
native subagent identity are grouped separately; activities without one are not
assigned to a subagent by guesswork. A completed delegation call does not mean
every child has finished.

Some native interface versions return an empty text result even when the CLI's
own log contains output. Clawdline distinguishes that from an older record with
no saved result. It does not reread a source file and present its current contents
as an earlier tool result. Tool completion follows native notifications, which
can arrive after the assistant's reply.

## Preview boundaries

This preview does not provide native terminal
attachment or turn-only cancellation. The owned native process implementation
supports macOS and Linux; Windows is not enabled by this path.

For connection problems, see [Recovery](recovery.md). For another machine or a
phone, see [Clawdline Cloud](cloud-first-look.md).

## Automatic names

**Settings → Auto-name new sessions** also applies to Gemini. Choose Claude Code,
Codex or Auto there. After a native turn completes successfully, an unnamed
conversation uses the same scrubbed first-request/latest-context naming flow as
**Smart name** in Session Info. The chosen naming assistant uses its account quota; Auto can try the other
assistant if the first fails, for at most two small model turns. Off remains off; enabling Gemini does not enable
this preference or spend naming quota by itself.

The resulting title is saved locally with the exact conversation and survives
a daemon restart. Manual names and Board/task names keep priority. A failed or
interrupted automatic naming attempt is not retried on every turn or restart;
use **Smart name** explicitly to try again. Naming never blocks the next Gemini
message. Turning the setting off while naming runs prevents title publication.

There is no Clawdline limit of two Gemini conversations. Each explicitly opened
conversation uses its own native process; more conversations can use more local
memory and CPU. Opening remains serialized, and native/provider errors still
apply. When a startup request names a model, it is selected from that native
conversation's offered models before the Session is published. Unsupported
choices fail without publishing another Clawdline Session; the native CLI may
still retain the blank conversation created before selection. In an owned Gemini Session,
`/model <native-model-id>` and `/effort <offered-value>` also use the existing
metadata controls; they are not model prompts.

## Pictures sent by the assistant

Pictures published with `clawdline image send` use the same image tiles and
original-image reader as other assistants. An exact image marker in an older
Gemini reply is also resolved; a marker inside a fenced code example stays text.
The stored native reply is preserved, and expired or unknown images keep their
existing unavailable state.

A Session-targeted image send binds to the first subsequent assistant row in
that exact owned history generation. Paging and an ordinary daemon restart
keep the same row. No marker is required in the assistant's prose. A replaced
history generation cannot reuse the old row number. Images sent with the older
marker-only API still need that marker in the reply.
