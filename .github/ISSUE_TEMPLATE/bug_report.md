---
name: Something is broken
about: Something the app does, or fails to do
labels: bug
---

**What happened, and what you expected instead**

**Which terminal**, and whether Claude Code is running inside tmux:

**Version** — the tag you installed, or the build you made:

**macOS version**:

**Anything in the log?** `~/.config/clawdline/clawdline.log` — the last twenty lines are usually
enough. Please skim it before pasting: it can contain paths and window titles.

<!--
If the bar cannot find your session, these two say more than anything else:

    ps -eo pid,tty,command | grep -i claude
    tmux list-panes -a -F '#{pane_id} #{pane_tty} #{pane_current_command}'
-->
