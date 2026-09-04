# Versions

<!-- Generated from Sources/Compat.swift by tools/build-compatibility.py. Do not edit. -->

Clawdline reads things the assistants it drives were never obliged to keep still: a
transcript file, a spinner drawn on a terminal, a process name, a clipboard convention,
the shape of a dialog. That is a reasonable way to build this and an unreasonable thing
to leave unwritten, because **each of those changing looks exactly like Clawdline being
broken.** This page is what it was run against, and what you would see if that stopped
being true.

## The short version

- Built and used against Claude Code **2.1.260**.
- And against Codex **0.151.0**.
- The oldest that everything here works with is **2.1.224**. 4 rows below name a floor at all; the rest have none known.
- Nothing refuses to run on an older one. What you lose is whichever of those rows names
  a floor you are under, and the second table below says which.
- A **newer** Claude Code is the normal state of the world, and is only mentioned when a
  newer Clawdline is out too — that is the one case where there is something to do about it.

## Tested against

| Clawdline | Claude Code | Codex | |
|---|---|---|---|
| 0.7.0 | 2.1.260 | 0.151.0 | Picking a recorded conversation back up, which depends on `claude --resume` taking a session id and on the names Claude Code writes for the conversations it has already recorded: if either moves, the list a project shows is empty or opens the wrong one. iTerm2 stops being the only terminal — everything else runs through tmux control mode, so a second program's shape now matters as much as an assistant's. And the Web transcript reads Codex's rollout item types directly, which are not the same list as its live events and are the half that changes without a version bump. |
| 0.6.0 | 2.1.235 | 0.149.0 | Answering a session from a phone, which adds two dependencies of a different kind. The hook contract — nine matcher groups under eight event names, written into ~/.claude/settings.json — replaces reading the screen when it is installed. And answering a multiple-choice question sends the single byte its picker reads, so if that picker stops taking a bare digit the phone can still see the question and can no longer answer it. This is also the first release that can see Codex, which adds five shapes of its own — a rollout file, a live line, a dialog, a process name and the word that ends a session. |
| 0.5.0 | 2.1.234 | not applicable | Reads every session's screen to say which is working, which has stopped and which is waiting — so it depends on the shape of the spinner line and of the box Claude Code asks a question in. |
| 0.4.0 | 2.1.233 | not applicable | Images go over as [Image #3] rather than as paths, which adds the clipboard-on-Ctrl-V dependency. |
| 0.3.0 | not recorded | not applicable | Transcript reading, the spinner line, and the project footer. |
| 0.2.0 | not recorded | not applicable | Mascot packs and the picker. |
| 0.1.0 | not recorded | not applicable | First release. |

The two version columns are what somebody actually had installed while using that
release — not a supported range. A range nobody tried is how a compatibility table starts
saying things that are not true. "Not applicable" is a release that predates this being
able to see Codex at all, which is a different thing from nobody having written it down.

**Older than 2.1.260 gets a line in the menu bar**, because then a missing feature really is
missing rather than broken here.

**A newer Claude Code is the normal state of the world**, and for a long time nothing said
anything about it: that assistant updates itself, this did not, and a line you cannot act on
is a line you stop reading. Clawdline now checks once a day whether a release of its own is
out, so that case has split in two. Ahead of what this was checked against with no newer
Clawdline to move to is still silent. Ahead **and** a release waiting is one line naming all
three versions, which needs both halves at once and so cannot become a weekly notice.

## What it depends on, and how you would know

| Whose | What | Where | Works since | If it changes |
|---|---|---|---|---|
| Claude Code | The session transcript: one JSONL file per session, under ~/.claude/projects/ | `Transcript.swift` | not known to have a floor | ⌘J shows nothing, or stops partway through a conversation |
| Claude Code | The spinner line Claude Code draws while it works, scraped off the screen | `Activity.swift` | not known to have a floor | The bar never says what a session is doing, even while it is doing it |
| Claude Code | The process being called `claude` | `Assistant.swift, Tmux.swift` | not known to have a floor | No sessions found at all, and nowhere to send a prompt |
| Claude Code | The tab title, and the status glyph Claude Code puts in front of it — as the last-resort way of ranking candidate transcripts, never as a session's name | `Transcript.swift` | not known to have a floor | ⌘J ranks by a title that no longer matches and lands on the wrong conversation, in the cases where nothing else identifies the session. A session's `label` is unaffected: it is never read off the tab — see `SessionNaming` |
| Claude Code | Reading an image off the system pasteboard on Ctrl-V, as [Image #N] | `Targets.swift` | 1.0.93 | A dropped image arrives as nothing, and the prompt points at a picture that is not there |
| Claude Code | Its dialogs: numbered rows marked with an indented caret, drawn inside a box frame, with the question in the lines above them | `SessionState.swift` | not known to have a floor | A session waiting on a permission dialog or a `/model` menu looks idle, and cannot be answered from a phone |
| Claude Code | The AskUserQuestion picker, whose caret sits flush left — the same column as the caret in front of the composer, which is why a hook note has to open the gate before that shape is trusted | `SessionState.swift, HookBridge.swift, SessionRegistry.swift` | not known to have a floor | A question waiting for an answer reads as ordinary output and is never surfaced — only while the registry below is unavailable too, since that opens the same gate without anything being installed; or an echoed numbered list is offered as a picker, and the answer lands in the composer as a stray digit |
| Claude Code | `/exit` ending a session, and a bare digit answering a picker | `Assistant.swift, Targets.swift` | not known to have a floor | "End" closes the tab on a session that is still running; a menu answer does nothing |
| Claude Code | The hook contract: nine matcher groups under eight event names, the notification types those groups match on, and the `tool_input.questions` shape a PreToolUse note carries | `HookBridge.swift, Resources/clawdline-hook.sh` | not known to have a floor | Hooks report themselves installed and no note ever arrives, so every reading waits for the next poll again; or a question comes through with no options on it |
| Claude Code | The session registry: one file per session at ~/.claude/sessions/<pid>.json, stating `peerProtocol: 1`, and the `status` in it — `idle`, `busy`, `waiting`, `shell` — kept current by the session itself | `SessionRegistry.swift` | 2.1.224 | Every reading falls back to the screen: a question is only noticed once its menu is recognised, and away from the panel that can be twenty seconds after it was asked |
| Claude Code | `procStart` in that file: the process start time as `LC_ALL=C TZ=UTC ps -o lstart=` prints it, which is what says a leftover file is not about the process now holding its pid | `SessionRegistry.swift` | 2.1.224 | Nothing visible, and that is the point — the check failing quietly means every registry entry is discarded and the screen answers alone |
| Claude Code | `sessionId` in that file naming the session's own transcript | `SessionRegistry.swift, Transcript.swift` | 2.1.224 | ⌘J falls back to matching by project, tab title and modification time, and can land on a background agent's transcript instead |
| Claude Code | `parkedJobId` appearing in that file once a conversation is moved to the background, and the `kind: "bg"` file whose `jobId` matches it — which is where that conversation carries on, under an id of its own | `SessionRegistry.swift` | not known to have a floor | A tab whose conversation has moved to the background is read from the file it stopped writing: the transcript ends mid-sentence at the moment of the move, and the status frozen there outranks everything the screen can see. A version that never parks writes no such field, and none of this applies |
| Claude Code | The empty composer: a bare `❯`, or the grey `Try "…"` suggestion a new session draws instead | `Orchestrator.swift` | not known to have a floor | A session opened to be handed work is never seen as ready for it, and the instructions are never typed in |
| Claude Code | The name of a project's transcript folder: the working directory with every character that is not a letter or a digit turned into a dash | `Transcript.swift, StartPoints.swift` | not known to have a floor | One project has no transcripts at all while every other project is fine — the ones whose path holds a space, a dot or an underscore |
| Claude Code | What a transcript record holds inside: `type`, `isSidechain`, `quotaLimits` and `rate_limits`, `<command-name>`, and the `Set model to …` line a `/model` prints | `Transcript.swift, SessionInfo.swift` | not known to have a floor | A subagent's conversation leaks into the pane, or a card stops saying which model it is on and how much of the window is left |
| Claude Code | The subagent sidecars it writes — `<session>/subagents/agent-<id>.meta.json` — and a running one being the one with no finish recorded in it | `Subagents.swift` | not known to have a floor | The panel says no agents are running, or keeps showing one that finished long ago |
| Codex | The rollout: one JSONL file per session, under ~/.codex/sessions/YYYY/MM/DD/ | `Codex.swift` | not known to have a floor | A Codex session's transcript is empty, and the pane falls back to the terminal capture |
| Codex | A rollout's completed `FileChange` item, whose `changes` rows carry `type`, `content`, `unified_diff` and `move_path` | `Codex.swift` | not known to have a floor | File edits appear only as filenames instead of red and green inline diffs |
| Codex | A rollout's `token_count.rate_limits`: `limit_id`, `credits.has_credits` and `plan_type` — which quota bucket answered, whether credits remain, and the account's own plan name | `SessionInfo.swift, AssistantQuota.swift` | not known to have a floor | Once the primary bucket is full, the account moves to an unnamed credits bucket; without those fields, genuine exhaustion looks unknown and the broker allows more work |
| Codex | A session holding its own rollout open, and saying `thread_source` in its first line | `Codex.swift` | not known to have a floor | Two Codex sessions in one directory show each other's conversation, or a session shows one of its subagents' |
| Codex | Its live line — a bullet, a word, and a clock: • Working (10s • esc to interrupt) | `Activity.swift` | not known to have a floor | A Codex session never looks busy, even while it is |
| Codex | The process being called `codex`, natively or behind the published Node shim | `Assistant.swift` | not known to have a floor | No Codex sessions in the list at all, and nowhere to send a prompt |
| Codex | Its dialogs: numbered rows under a caret in column zero, with the composer taken away while one is up | `SessionState.swift` | not known to have a floor | A Codex session that is waiting for an answer looks idle, and cannot be answered from a phone |
| Codex | `/quit` ending a session, and a bare digit answering a dialog | `Assistant.swift, Targets.swift` | not known to have a floor | "End" closes the tab on a session that is still running; a menu answer does nothing |
| Codex | The model list its own picker shows, cached in ~/.codex/models_cache.json as `slug`, `visibility` and `display_name` | `SessionInfo.swift` | not known to have a floor | The model button on a phone is empty, or offers a model this Codex no longer has |
| Codex | `codex app-server`'s JSON-RPC — `initialize`, `model/list`, `thread/list`, `thread/read` and `thread/name/set`. The one dependency here with a real contract: it has a generator, `codex app-server generate-json-schema` | `CodexNaming.swift` | not known to have a floor | A Codex session opened to be handed work never gets its name, or the resume sheet has no Codex history to offer |
| Codex | Which subcommands are not an interactive session — `exec`, `mcp-server`, `app-server` and the rest of that list | `Assistant.swift` | not known to have a floor | A batch `codex exec` turns up in the list as a session somebody can type into, and the prompt goes nowhere |
| claude-bestiary | `rate-limits.json` in the status line's cache directory: `at`, `session_id` and a `rate_limits` block of windows | `SessionInfo.swift` | not known to have a floor | The plan's five-hour and weekly windows go blank until a session actually hits a limit and its transcript says so |

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
