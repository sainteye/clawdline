<!-- clawdline-doc: kind=reference audience=both -->

# Voice input settings

Settings › Voice input has three cards: **Recognition**, **LLM cleanup** and **Personal
vocabulary**. Recognition is the local Whisper setup described in
[recording recovery](voice-recording-recovery.md) and the [Whisper record](whisper.md). This page
covers the other two.

## LLM cleanup

Whisper's Chinese punctuation is sparse and uneven, most of all when someone speaks a long sentence
without pausing. The initial prompt that seeds punctuation does not fix it: on 2026-11-11, seven
synthetic Chinese and mixed-language clips came out with identical punctuation with and without the
seed, and this machine, which always runs with the seed, still showed the inconsistency.

With **LLM cleanup** on, the daemon sends each transcript to a small model for one tool-less turn
after Whisper (`internal/adapters/planner/polish.go`). The model adds punctuation, corrects a
mis-heard word only when the context makes it certain, and, at the default level, removes filler
words. It does not translate, summarize or answer. Chinese stays in the
script the transcript is already in (`whisper.Plan.Script`): Traditional, Simplified, or, when
nothing chose one, whatever characters Whisper wrote.

- **Off unless set** (`voice_polish`). With it off, nothing about a recording leaves the machine.
  With it on, the transcript and the vocabulary go to the chosen assistant's provider, through the
  CLI that is already signed in on this machine. No API key is involved.
- **Assistant** (`voice_polish_assistant`): `claude` runs Claude Haiku; `codex` runs Codex's small
  model. Measured on 2026-11-11 with the product's own flags, Haiku took 3.1–4.3 s for one sentence
  and Codex took 5.1–7.7 s; Settings quotes these. (A first prototype without those flags measured
  2.0–3.1 s and 5.3–10.5 s over twelve transcripts.) A browser-side wait of 120 s covers either, and
  the turn runs after Whisper hands back its queue slot, so a cleanup never makes the next
  recording wait.
- **Level** (`voice_polish_level`): `punctuation` keeps every word; `tidy` (the default) also
  removes filler words.

A cleanup is discarded and Whisper's text is used whenever the turn is slow (15 s), the
assistant is not installed or out of usage, the transcript is very long, or the answer changed the
words. "Changed the words" is `planner.Faithful`: ignoring punctuation, spaces and case, at most
20% of the characters may differ at the `punctuation` level and 35% at `tidy`. In the 2026-11-11
measurement Haiku once dropped a three-word phrase; that kind of deletion stays under the ceiling,
while dropping a clause, answering the dictation or translating it does not.

The answer from `POST /v1/voice` carries `polish.status` (`applied`, `unchanged`, `unavailable`,
`unfaithful`, `too_long`, `quota`, `timeout` or `failed`). When the cleanup replaced the text,
`raw` holds Whisper's own words. The composer shows a tag over the box: **Cleaned up by LLM ·
Show original** swaps the dictated words back and forth while they are still in the box unedited;
a fallback says why the original was used. The daemon log records the status, the duration and
both lengths, never the words.

Dictated text is inert content in the prompt. Both CLIs run without tools, MCP servers, slash
commands or saved sessions, so a sentence such as "commit and push to main" is only punctuated.

## Personal vocabulary

- **Your terms** (`voice_vocabulary`): at most 200 distinct one-line terms of at most 64
  characters. They are repaired in every transcript after Whisper (`whisper.ApplyVocabulary`) and,
  with cleanup on, shown to the model.
- **Project names** (`voice_vocabulary_projects`, off unless ticked): the folder names of the
  forty places the project picker lists. In Clawdline a project's name is its folder's name, which
  is normally the repository's name. The card lists exactly which names a ticked box adds, because
  with cleanup on they are sent too.
- **Built in**: `whisper.AlwaysExpected` (Clawdline, Claude Code and the rest), read-only.

## From a phone

The two cards read and write their own narrow route, `/v1/settings/voice`: the five keys above,
plus `polish_assistants`, `vocabulary_builtin` and `vocabulary_projects`, and refusing every other
setting. A paired Cloud console carries it as `voice-settings` and `voice-settings-update`, so
app.clawdline.com shows the same cards under Settings › Voice input for the machine selected in
its header, and names that machine (`machine` in the answer), because a phone can switch between
several. Switching machines reads the cards again. A recording made in a Session is transcribed
by that Session's machine, so its cleanup follows that machine's settings. Changing them from a remote
device needs Send, as the default models do. The Recognition card (language, model) stays on the
machine's own console. A machine from before API level 43 answers that it needs an update, and the
cards say so instead of drawing controls that cannot save.

The bounds are registered in `internal/domain/capacity` as `voice.vocabulary_terms`,
`voice.vocabulary_term_characters`, `voice.vocabulary_project_terms`, `voice.polish_terms`,
`voice.polish_characters` and `voice.polish_timeout_milliseconds`.
