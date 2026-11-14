# Session recaps

A Session recap is one or two plain sentences about a Session — what it is working on, where it got
to, whose move it is — written by **the Session's own assistant from a throwaway fork of its
conversation**, together with a fuller account for the voice copilot. The voice copilot reports from
recaps first, and Session info shows the short recap under Status. The person-facing description is in
[the voice copilot guide](user/voice-copilot.md#session-recaps).

## Why

On 2026-11-14 the person said they could not keep track of their Sessions through the voice
copilot. The copilot's call log showed three failures:

- Asked what the working Sessions were doing, it answered with the step of the minute ("filling in
  six new limits' strings", "cutting the function body, then the probe history"), because it read
  each Session's newest transcript page. Asked to explain, it read out another, finer step.
- It said it would go through every Session, covered two of eight, and stopped.
- Once it answered only that the machine's briefing was not ready.

The idea comes from the Recap mod for Herdr (github.com/mrolafsson/herdr-recap), which forks
the conversation so the assistant that did the work answers from all of it.

## Two parts, in the console's language

The fork answers in two parts separated by `---`. Part one (at most 400 characters) is what a screen
shows. Part two (`detail`, at most 1200 characters) is shaped by the two questions the person said
they ask the copilot most: "what has this Session finished so far, and what do I need to decide?"
and "report this Session's summary". It names the goal, what was finished since the conversation
began, what is in progress or stuck, and every decision owed with its choices. `get_session_summary`
and `get_sessions_summary` hand it to the copilot as `recap_detail`; `list_sessions` does not, so a
list stays short.

The prompt names the console's language (`RecapLanguage`). Left to "the language of the
conversation", Haiku answered a Taiwan Chinese conversation in English, and in that Session's own
voice: "I can't give you that status summary in the requested format right now. Two things block
it…". The prompt now says the fork describes the Session from the outside and always answers, and an
answer that still opens with a refusal is not kept.

The recap first appeared as a second line in the Session list; the person found the list too dense
and asked for it in Session info instead (2026-11-14).

## How a recap is written

`internal/adapters/planner/recap.go` runs one turn:

| Assistant | Command (prompt on stdin) |
|---|---|
| Claude Code | `claude -p --resume <uuid> --fork-session --no-session-persistence --model haiku --effort low --tools "" --permission-mode dontAsk --strict-mcp-config --mcp-config '{"mcpServers":{}}' --disable-slash-commands --output-format json`, in the Session's directory |
| Codex | `codex exec fork --ephemeral --skip-git-repo-check --disable shell_tool --disable unified_exec -c sandbox_mode="read-only" -c web_search="disabled" -c agents.enabled=false -c approval_policy="never" -c model_reasoning_effort="low" -o <file> <uuid> -`, in a scratch directory |

Measured on this machine on 2026-11-14 (Claude Code 2.1.325, codex-cli 0.166.0):

- The original record's size and the number of session files were unchanged after both forks.
- Claude, tools off, Haiku: 7–8 s, one turn, 0.008–0.009 USD on a 42k-token fork. The same fork on
  Sonnet was 0.17 USD and on the Session's own Opus 0.34 USD; tools off misses the prompt cache, so
  the cheap model is the right trade.
- Codex: 12 s.
- Sending `/recap` resolved to the person's own skill named recap: 72 s, five turns with tool
  calls, a full report. The question is asked in words, with slash commands disabled.

Tools are off because this repository's settings let some shell commands run without a prompt; a
fork that carried on with the work instead of answering must not be able to act. The prompt leads
with "Stop: this is a question about this session, not a continuation…", and a Claude answer marked
as an error or reached after more than one turn is discarded.

## When

`copilot.Settings.SessionRecaps` (Settings → Voice copilot → Session recaps) is one of:

- `off` (default): nothing is forked and rows carry no recap.
- `background`: `recapBook.Observe` rides the inventory scans. A stopped Session (idle or waiting)
  is recapped after standing still for three minutes, and again only after its record grew. A
  working Session is recapped after two minutes and at most every ten minutes while its record
  grows. One fork runs at a time, waiting first, then idle, then working. A failed fork waits ten
  minutes before it is tried again.
- `on_demand`: nothing runs in the background. `POST /v1/voice/copilot-recap {"session", "fork": true}`
  returns the kept recap when the record has not grown since, otherwise forks now. Three run at once;
  a fourth is refused as `session_recaps_busy`. The copilot waits up to 45 s; a slower fork goes on
  and its recap is kept.

The copilot's tools (`forkedRecap` in `copilot-tools.ts`) use a current recap from the row, ask the
machine for one in either mode when it is missing or stale, and fall back to the stale one.

## Storage and visibility

Recaps are kept in `session-recaps.json` (mode 0600) beside `copilot.json`, one per conversation,
200 at most, the oldest giving way. A recap is never shown for another conversation in the same
terminal. `current` says whether the record has grown since.

A recap is content. It travels in `GET /v1/sessions` and its Cloud list projection to the person's
paired browsers, as the transcript does, and never in the signed `ss/` status projection. The audit
line `session.recap` records assistant, state, duration and outcome, never the text.

The bounds are registered in `internal/domain/capacity` as `session.recap_*`.
