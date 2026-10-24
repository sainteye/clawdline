# Use the Gemini personal preview

[繁體中文（台灣）](gemini.zh-TW.md)

Clawdline calls this assistant **Gemini** in the console. This preview runs the
Antigravity ACP CLI on the selected machine. Gemini CLI and Antigravity are
separate products; installing or signing into one does not establish that the
other is ready.

The preview appears only for an administrator when the machine owner has enabled
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

If the request changes or expires, refresh the conversation and read the current
question. Closing the conversation or timing out does not approve its pending
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

Role-based dispatch and native tool access to the Clawdline Session identity are
still being integrated and are not established by this role-picker change.

## Preview boundaries

This preview does not provide native terminal
attachment or turn-only cancellation. The owned native process implementation
supports macOS and Linux; Windows is not enabled by this path.

For connection problems, see [Recovery](recovery.md). For another machine or a
phone, see [Clawdline Cloud](cloud-first-look.md).
