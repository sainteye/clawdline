# Persistent Bots: getting started

A Bot is a long-lived Claude assistant bound to one Project. You chat with
it in the console; it remembers what you tell it, works in the Project with
Claude Code's own tools, and can hand larger changes to an isolated worker
through Clawdline's Broker. The worker's result comes back to the same chat.

Bots are off by default.

## What you need

- macOS.
- For a Claude Code Bot: Claude Code 2.1.323 or newer, signed in as you
  normally use it. For a Codex Bot: Codex 0.166.0 or newer, signed in, with
  its memories feature off (the default; `codex features list`).
- Node 22 or newer with npm.

Clawdline finds both programs itself, including installs from nvm, mise,
asdf, fnm, Volta or Homebrew. You do not place any files. On the first
message, Clawdline installs the ACP adapter it supplies
(`@agentclientprotocol/claude-agent-acp` or `@agentclientprotocol/codex-acp`) with your npm and checks it
against a fixed digest. This takes a few seconds, and later messages reuse
it.

## Turn Bots on

In the Clawdline app on this machine, open **Settings** and turn on
**Persistent Bots** (on the tab with the dispatch settings). The Bots orb
appears in the Session list. A paired browser or phone can read the setting
but cannot change it, like this machine's other settings.

Bots is not in public releases yet: the switch is shown only on a dev build
(`v<x.y.z>-dev.<commit>`), or where Bots is already on so it can be turned
off. The terminal command below still works on any build.

From a terminal, the same switch is:

```sh
clawdline project-assistants enable
```

`clawdline project-assistants status` shows the current state.
`clawdline project-assistants disable` turns Bots off and keeps saved Bots
and conversations. `clawdline project-assistants setup-claude` runs the
first-use setup ahead of time and prints which Claude Code version it found;
`setup-codex` does the same for Codex.

## Use a Bot

1. Click the Bots orb in the Session list, then **+**. You can drag the
   orb anywhere over the list, or onto the quota bar at the bottom, where it
   becomes a small icon that rings and counts new replies.
2. Give it a name, choose the Project and write its standing instructions.
   Creating a Bot does not call a model.
3. Send a message. Up to four pictures can go with it: press **+**, paste
   them or drop them on the message box. The Bot sees them as images. (Pictures
   work from this machine's own console; not yet through Clawdline Cloud.)
   The Bot answers in the chat. While it works, the chat shows what it is
   doing (the tool it is using, or that it is writing) and how long the turn
   has run. A message sent while it is still working waits in line and is
   shown as pending; **×** on a pending message takes it back before it is
   sent, leaving the current reply and the other pending messages alone, and
   **↳** sends it now, into the reply in progress, as in Codync. If that reply
   had just ended, the message keeps its place and goes next. (Send now works
   from this machine's own console; not yet through Clawdline Cloud.)
4. Ask it to dispatch work, for example "add a file and commit it". It
   starts an isolated worker in that Project's worktree, and a work card
   follows the work from accepted to running to done. The worker's summary
   arrives in the chat.
5. **Stop** ends the current reply and any pending ones. Your ordinary
   Sessions are not touched.

A Bot keeps its own memory, as in Codync: when you tell it something lasting
or ask it to remember, change or forget something, it does so with its
memory tools, one short fact per entry. **Memory** in the chat lists every
entry, yours and the Bot's, and forgets the ones you choose. Adding or
changing an entry keeps the Bot's conversation going, and its next reply
starts from the whole memory as it is now; forgetting starts a fresh
conversation that carries only what is still remembered. A Bot keeps at most
256 entries.

**Permissions**, in the Bot's settings and when you create it, decides how
much the agent does on its own before it asks, as in Codync. It lists the
agent's modes, its Auto mode first, then **Approve automatically**:

| Setting | What happens |
| --- | --- |
| **Auto** (Claude Code), **Auto review** (Codex) | The agent decides what's safe and does it, and asks you about the rest. New Bots start here. |
| The agent's other modes: **Manual**, **Accept edits**, **Plan** (Claude Code); **Read-only**, **Workspace access**, **Full access** (Codex) | The agent works in that mode; anything it asks about appears as a permission card. |
| **Approve automatically** | Each request is allowed for you with the agent's allow-once option. |

With any setting your agent's own settings still apply. On a model without
Claude Code's Auto mode, Claude Code works in Accept edits instead. A mode
the agent does not offer stops the turn with a reason rather than run it in
another. When the agent asks, a permission card appears in the chat and you
can allow that action once. If a worker's terminal shows a question, such as whether to trust a
folder, the work card tells you to answer it in that Session.

## If something is missing

- *This Bot needs Claude Code 2.1.323 or newer and Node 22 or newer*:
  install or update them, sign in to Claude Code, then send again.
- A reply whose result is unknown is never sent again on its own. Your next
  message marks it as failed and continues.

Technical details are in [project-assistants-native-entry.md](project-assistants-native-entry.md)
and [project-assistants-server.md](project-assistants-server.md).
