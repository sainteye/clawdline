# Dictating in more than one language

Clawdline's microphone uses Apple's speech recogniser by default. It needs nothing installed and
the words appear as you say them — but it hears **one language at a time**. That is a property of
the API, not a setting: neither `SFSpeechRecognizer` nor its replacement changes language
mid-sentence. So this is a sentence it cannot be asked to hear:

> cambia el retry a exponential backoff

Whisper transcribes that as a matter of course. Installing it is optional, and worth it only if
you actually speak that way.

**It does not replace the live one, it finishes what it started.** Apple's recogniser keeps
writing as you speak, so the box fills in while you are talking. When you stop, Whisper reads the
same recording and replaces the whole run with its version. You get the feedback of one and the
sentence of the other, and the two engines are good at opposite halves of the same job.

| | Apple alone | with Whisper |
|---|---|---|
| install | nothing | a binary and a model, ~600 MB |
| while you speak | text appears | text appears |
| when you stop | that was it | the run is replaced with a better reading |
| more than one language in a sentence | no | yes |
| leaves the machine | only for languages you have not downloaded | the second pass never does |

---

## Ask Claude Code to do it

Open Clawdline, press <kbd>⌘</kbd><kbd>J</kbd> if you want to watch, and send this:

> Set up Whisper for Clawdline's dictation. Do all of it, then tell me what to press.
>
> 1. `brew install whisper-cpp` — it installs a `whisper-cli` binary.
> 2. Download a model into `~/.cache/whisper/`. Use
>    `ggml-large-v3-turbo-q5_0.bin` from
>    `https://huggingface.co/ggerganov/whisper.cpp/resolve/main/` unless my Mac has less than
>    16 GB of memory, in which case use `ggml-medium-q5_0.bin`. Check the file size afterwards —
>    a failed download is an HTML error page with a `.bin` name, and it will look installed.
> 3. Verify it runs: transcribe any short WAV and show me the output. `say -o /tmp/t.aiff "testing
>    one two three"` then `afconvert -f WAVE -d LEI16@16000 -c 1 /tmp/t.aiff /tmp/t.wav` gives you
>    one.
> 4. Clawdline finds both on its own, so there is nothing to configure — but confirm it agrees by
>    checking that `whisper-cli` is on the PATH and that the model is the only `ggml-*.bin` in
>    `~/.cache/whisper/`.
>
> Do not change `~/.config/clawdline/config.json` unless step 4 shows it is needed.

Then press <kbd>⌘</kbd><kbd>L</kbd> in Clawdline. It will say Whisper is going to take another
look when you stop — and when you do, the words you watched appear get replaced by its reading of
the same audio.

---

## Doing it by hand

```bash
brew install whisper-cpp

mkdir -p ~/.cache/whisper
curl -L -o ~/.cache/whisper/ggml-large-v3-turbo-q5_0.bin \
  https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-large-v3-turbo-q5_0.bin

ls -lh ~/.cache/whisper/          # ~570 MB. A few kilobytes means the download failed.
```

That is the whole installation. Clawdline looks for `whisper-cli` in the usual places and for the
largest `ggml-*.bin` in `~/.cache/whisper`, `~/Library/Application Support/Clawdline/models` or
`~/models`, and switches over as soon as it finds both.

### Models

| file | size | when |
|---|---|---|
| `ggml-large-v3-turbo-q5_0.bin` | 547 MB | the default choice; fast enough on Apple silicon |
| `ggml-medium-q5_0.bin` | ~540 MB | a Mac with less memory to spare |
| `ggml-small.bin` | ~490 MB | slower machines; noticeably weaker on names |

Measured on an M4 with the turbo model: **1.6 s** to read back three seconds of speech, and
**12 s** the first time after a reboot — that one is the model being read off disk, not the
transcription. So the first sentence you dictate in a session is slow and the rest are not, which
is worth knowing before you conclude it is broken.

Bigger is not automatically better for a given sentence, and the difference between them is
mostly proper nouns.

### Words it cannot be expected to know

"Clawdline" is in no model's vocabulary and "Claude Code" is two ordinary English words in an
order no corpus has seen, so both come back as something that merely sounds right — *cloud code*,
*clawed line*. Put them in `voice_vocabulary` and they are repaired in the text afterwards:

```jsonc
{ "voice_vocabulary": ["Clawdline", "Kubernetes", "Zhuoyi"] }
```

Afterwards rather than in the prompt, for the reason below. It is deliberately timid — a term
under six letters is matched exactly or not at all, because at that length one edit reaches every
ordinary word nearby, and a corrector that rewrites real words is worse than none. Measured
against both directions: *cloudline* becomes Clawdline, and *the claw came off* is left alone.

**Clawdline does not send Whisper a word list.** Apple's recogniser gets one — `contextualStrings`
is a list field, and it costs nothing there. Whisper's `--prompt` is not a list field: it is a
sample of the text that came before, and the transcript imitates it. Measured on one clip,
changing only the prompt:

| prompt | transcript |
|---|---|
| a short punctuated sentence | …更有結構化**。**我們需要有目錄…在哪裡**。** |
| that sentence plus twenty terms | …更有結構化我們需要有目錄…在哪裡 |

(A Chinese clip, because that is where the loss is easiest to see: the same words, and in the
second row every full stop has gone.) Every punctuation mark, gone. Punctuation is worth more here than a nudge towards spellings the
model is already good at, so the prompt is two ordinary sentences and nothing else.

### Config

Nothing here is required; set it only to override what was found.

```jsonc
{
  "voice_engine": "auto",        // auto | apple | whisper
  "voice_language": "auto",      // "zh-TW", "en", … — auto lets Whisper decide
  "voice_settle_seconds": 1.8,   // how long a pause ends a sentence; 0 turns it off
  "voice_stop_seconds": 4.0,     // how long a silence ends the session; 0 leaves it listening
  "whisper_binary": "",          // a specific whisper-cli
  "whisper_model": ""            // a specific .bin
}
```

### Language and script

`voice_language` does two things that sound like one.

It **names the language**, which stops the model guessing. Guessing is right when you genuinely
switch languages and wrong in a quiet room, because a model asked what silence says will answer
anyway — usually with a short, confident English sentence.

It also **fixes the script**. Whisper writes Chinese in Simplified whichever way you ask, so
`zh-TW` seeds the prompt with a Traditional sentence — and then converts what comes back, using
the transliterator macOS already ships. The seed is a preference; the conversion is the
guarantee. English inside a Chinese sentence passes through untouched, which is the whole point
of using Whisper here.

One limit worth knowing: it converts characters, not vocabulary. 网络 becomes 網絡, not 網路.
Wording is a regional choice, and a character table is not the layer that can make it.

`auto` means "whisper if it is installed". Installing it was the deliberate act; being asked
again in a config file would be a second hoop for no reason. Set `"apple"` to keep the live
transcription even with whisper present.

---

## What changes when it is on

**The words you watched appear get replaced a sentence at a time.** whisper.cpp is not a
streaming transcriber, so it can only read a finished recording — which is why the live text is
still Apple's. Every pause of about two seconds ends a stretch: Whisper reads that stretch,
replaces it, and nothing after that point rewrites it. A pause means "quiet compared to the last
few seconds", not quiet compared to a number — measured here, an ordinary room sits at a third of
the scale, so a fixed threshold would be either this room or somebody else's. Talking for two minutes therefore costs a
handful of short reads rather than one long one at the end, and the text you have already read
stops moving. `voice_settle_seconds` changes the pause, and 0 turns it off.

**A longer silence closes the microphone.** A settle says "that sentence is finished"; four
seconds of quiet says "I am finished", and the session ends without being pressed again — which
is what makes a long paragraph one keystroke instead of two. The two thresholds are different
claims about the same silence, so they are separate numbers. A stretch that ended mid-clause
waits `1.75×` longer than one that ended on a full stop: being late costs an open microphone in
an empty room, being early costs exactly the keystroke this removes. A microphone that was opened
and never spoken into is never closed this way — nothing has happened for the silence to be the
end of.

**While it reads, the microphone becomes a turning arc and the bar counts the seconds.** The
count is there because the first run after a reboot spends twelve seconds loading the model
before it transcribes a word, and a spinner with no number beside it cannot be told apart from a
hang.

**If the second pass fails, the first one stands.** A missing model or a binary that will not run
leaves the live text exactly where it was; it does not empty the box you were about to send.

**Editing what was dictated stops the replacement**, for the part you edited. Those words are
yours now, and the recording behind them is dropped rather than being transcribed over the top of
your correction.

**Silence costs nothing.** Under a quarter of a second is dropped, so a mis-click does not spin
up a model.
