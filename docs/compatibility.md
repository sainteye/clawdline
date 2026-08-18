# Versions

<!-- Generated from Sources/Compat.swift by tools/build-compatibility.py. Do not edit. -->

Clawdline reads things Claude Code was never obliged to keep still: a transcript file, a
spinner drawn on a terminal, a process name, a clipboard convention. That is a reasonable
way to build this and an unreasonable thing to leave unwritten, because **each of those
changing looks exactly like Clawdline being broken.** This page is what it was run
against, and what you would see if that stopped being true.

## The short version

- Built and used against Claude Code **2.1.234**.
- The oldest that everything here works with is **1.0.93**, and only one feature cares.
- Nothing refuses to run on an older one. What you lose is the one feature whose floor
  you are under, and the second table below says which.
- A **newer** Claude Code is the normal state of the world and is not warned about.

## Tested against

| Clawdline | Claude Code | |
|---|---|---|
| 0.5.0 | 2.1.234 | Reads every session's screen to say which is working, which has stopped and which is waiting — so it depends on the shape of the spinner line and of the box Claude Code asks a question in. |
| 0.4.0 | 2.1.233 | Images go over as [Image #3] rather than as paths, which adds the clipboard-on-Ctrl-V dependency. |
| 0.3.0 | not recorded | Transcript reading, the spinner line, and the project footer. |
| 0.2.0 | not recorded | Mascot packs and the picker. |
| 0.1.0 | not recorded | First release. |

The Claude Code column is the version somebody actually had installed while using that
release — not a supported range. A range nobody tried is how a compatibility table starts
saying things that are not true.

**A newer Claude Code is the normal state of the world.** It updates itself and this does
not, so nothing warns about it. Older than 2.1.234 does get a line in the menu bar, because
then a missing feature really is missing rather than broken here.

## What it depends on, and how you would know

| What | Where | Works since | If it changes |
|---|---|---|---|
| The session transcript: one JSONL file per session, under ~/.claude/projects/ | `Transcript.swift` | not known to have a floor | ⌘J shows nothing, or stops partway through a conversation |
| The spinner line Claude Code draws while it works, scraped off the screen | `Activity.swift` | not known to have a floor | The bar never says what a session is doing, even while it is doing it |
| The process being called `claude` | `ITerm.swift, Tmux.swift` | not known to have a floor | No sessions found at all, and nowhere to send a prompt |
| The tab title, and the status glyph Claude Code puts in front of it | `Transcript.swift` | not known to have a floor | The wrong conversation in ⌘J, or a stray glyph in the name |
| Reading an image off the system pasteboard on Ctrl-V, as [Image #N] | `Targets.swift` | 1.0.93 | A dropped image arrives as nothing, and the prompt points at a picture that is not there |

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
