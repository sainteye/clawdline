# Your first Session

A Session is a Claude Code or Codex conversation you can watch and answer from Clawdline. This walkthrough starts with the installed release and ends when you can see your first conversation.

## Install and open

On macOS 13 or later, or Linux, install Clawdline with the command on the [project home page](../../README.md#install). The installer opens the console in your browser. If you already installed it, open Clawdline from the menu bar on macOS or run `clawdline open` on the machine.

**Success:** the console opens to the Session list. If the page does not open, run `clawdline doctor` on that machine and follow the action it prints. The [installation guide](install.md) covers service setup, updates, removal, and source builds.

## Start a conversation

1. In a terminal on the same machine, run `tmux new -s work`, then run `claude` or `codex` inside that terminal. On macOS, an iTerm2 Session also appears without tmux.
2. In Clawdline, open **Sessions**. Look for the new row under its project.
3. Select the row to read the conversation. Send a short message in the box and press **Send**.

**Success:** your message appears in the conversation, and the Session continues working. An Agent question appears as a choice card or an attention note; answer it in the Session. [Working with Sessions](sessions.md).

**If the row is missing:** check that the assistant is running inside tmux (or iTerm2 on macOS), then refresh the list. **If the row says its state is unknown:** open its live screen; Clawdline waits for readable evidence instead of guessing. [Recover from a problem](recovery.md).

## Home and Settings on macOS

The application menu's **Home / Settings** (**Shift–Command–H**) brings the main Clawdline
console forward. If the Cloud tab was in front, this command returns to the local console.
It is a navigation shortcut; it does not change a setting.

Use **Settings** in the website console to change settings for the selected machine. Shared
machine settings are also available from Clawdline Cloud, so select the intended machine before
editing them. Browser-local preferences apply only to that browser.

Older macOS builds also showed a separate **Settings…** (**Command–,**) application menu item
for a native app settings window. That entry has been retired in current builds; settings are
operated through the website. A screenshot showing both entries may be from an older build.
If the visible menu differs, check the running version before assuming the two entries are two
current settings systems.

## Continue from another device

When this works locally, you can pair a phone or another computer. Cloud is optional and must be enabled on the machine. [See another machine](cloud-first-look.md).

[Technical reference for Session discovery and actions](../session-states.md) · [Agent operation guide for this build](../agent-contract.md)
