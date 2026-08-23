# Versions

<!-- Generated from Sources/Compat.swift by tools/build-compatibility.py. Do not edit. -->

Clawdline reads things the assistants it drives were never obliged to keep still: a
transcript file, a spinner drawn on a terminal, a process name, a clipboard convention,
the shape of a dialog. That is a reasonable way to build this and an unreasonable thing
to leave unwritten, because **each of those changing looks exactly like Clawdline being
broken.** This page is what it was run against, and what you would see if that stopped
being true.

## The short version

- Built and used against Claude Code **2.1.235**.
- And against Codex **0.149.0**.
- The oldest that everything here works with is **1.0.93**, and only one feature cares.
- Nothing refuses to run on an older one. What you lose is the one feature whose floor
  you are under, and the second table below says which.
- A **newer** Claude Code is the normal state of the world and is not warned about.

## Tested against

| Clawdline | Claude Code | Codex | |
|---|---|---|---|
| 0.6.0 | 2.1.235 | 0.149.0 | Answering a session from a phone, which adds two dependencies of a different kind. The hook contract — five events written into ~/.claude/settings.json — replaces reading the screen when it is installed. And answering a multiple-choice question sends the single byte its picker reads, so if that picker stops taking a bare digit the phone can still see the question and can no longer answer it. This is also the first release that can see Codex, which adds five shapes of its own — a rollout file, a live line, a dialog, a process name and the word that ends a session. |
| 0.5.0 | 2.1.234 | not applicable | Reads every session's screen to say which is working, which has stopped and which is waiting — so it depends on the shape of the spinner line and of the box Claude Code asks a question in. |
| 0.4.0 | 2.1.233 | not applicable | Images go over as [Image #3] rather than as paths, which adds the clipboard-on-Ctrl-V dependency. |
| 0.3.0 | not recorded | not applicable | Transcript reading, the spinner line, and the project footer. |
| 0.2.0 | not recorded | not applicable | Mascot packs and the picker. |
| 0.1.0 | not recorded | not applicable | First release. |

The two version columns are what somebody actually had installed while using that
release — not a supported range. A range nobody tried is how a compatibility table starts
saying things that are not true. "Not applicable" is a release that predates this being
able to see Codex at all, which is a different thing from nobody having written it down.

**A newer Claude Code is the normal state of the world.** It updates itself and this does
not, so nothing warns about it. Older than 2.1.235 does get a line in the menu bar, because
then a missing feature really is missing rather than broken here.

## What it depends on, and how you would know

| Whose | What | Where | Works since | If it changes |
|---|---|---|---|---|
| Claude Code | The session transcript: one JSONL file per session, under ~/.claude/projects/ | `Transcript.swift` | not known to have a floor | ⌘J shows nothing, or stops partway through a conversation |
| Claude Code | The spinner line Claude Code draws while it works, scraped off the screen | `Activity.swift` | not known to have a floor | The bar never says what a session is doing, even while it is doing it |
| Claude Code | The process being called `claude` | `Assistant.swift, Tmux.swift` | not known to have a floor | No sessions found at all, and nowhere to send a prompt |
| Claude Code | The tab title, and the status glyph Claude Code puts in front of it | `Transcript.swift` | not known to have a floor | The wrong conversation in ⌘J, or a stray glyph in the name |
| Claude Code | Reading an image off the system pasteboard on Ctrl-V, as [Image #N] | `Targets.swift` | 1.0.93 | A dropped image arrives as nothing, and the prompt points at a picture that is not there |
| Codex | The rollout: one JSONL file per session, under ~/.codex/sessions/YYYY/MM/DD/ | `Codex.swift` | not known to have a floor | A Codex session's transcript is empty, and the pane falls back to the terminal capture |
| Codex | A session holding its own rollout open, and saying `thread_source` in its first line | `Codex.swift` | not known to have a floor | Two Codex sessions in one directory show each other's conversation, or a session shows one of its subagents' |
| Codex | Its live line — a bullet, a word, and a clock: • Working (10s • esc to interrupt) | `Activity.swift` | not known to have a floor | A Codex session never looks busy, even while it is |
| Codex | The process being called `codex`, natively or behind the published Node shim | `Assistant.swift` | not known to have a floor | No Codex sessions in the list at all, and nowhere to send a prompt |
| Codex | Its dialogs: numbered rows under a caret in column zero, with the composer taken away while one is up | `SessionState.swift` | not known to have a floor | A Codex session that is waiting for an answer looks idle, and cannot be answered from a phone |
| Codex | `/quit` ending a session, and a bare digit answering a dialog | `Assistant.swift, Targets.swift` | not known to have a floor | "End" closes the tab on a session that is still running; a menu answer does nothing |

"Not known to have a floor" is not a shrug. These have looked the same for a long time,
nobody has gone back to find the version they started in, and putting a number there
that nobody checked would make the whole column mean "probably".

## Claude Code has its own dictation now

`/voice` — hold space, and it is good. Where it differs is the whole reason to reach for
this one instead:

- It **streams your audio to Anthropic's servers**; its docs say "audio is not processed
  locally".
- It needs a **Claude.ai account** — not an API key, Bedrock, Vertex or Foundry — and is
  unavailable under an organisation's HIPAA compliance setting.
- It transcribes **one language at a time**.
- **As of 2026-08-17 it does not support Chinese at all.** Twenty languages, Japanese
  and Korean among them, and no variety of Chinese in the list; no `language` value
  changed that. Checked against 2.1.233, which answered `"Chinese" is not a supported
  dictation language; using English`. Dated rather than hedged: if it changes, this
  line becomes history instead of becoming wrong.

Clawdline's second pass never leaves the machine and is built for the sentence with two
languages in it. See [whisper.md](whisper.md).

## claude-bestiary

No version of it is pinned, on purpose. Clawdline reads the **files**, and
[docs/project-status.md](project-status.md) is the contract for them —
[claude-bestiary](https://github.com/sainteye/claude-bestiary) is one producer, and a cron job
or a git hook that writes the same shapes is another. Naming a version of it here would
say something untrue about everything else that writes them.

A missing or unreadable status file is a normal state rather than an error, so there is
nothing to warn about: the footer simply has less to say. That is the whole compatibility
story, and it is short because the coupling is a documented file format rather than a
program's internals.
