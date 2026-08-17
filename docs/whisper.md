# Dictating in more than one language

Clawdline's microphone uses Apple's speech recogniser by default. It needs nothing installed and
the words appear as you say them — but it hears **one language at a time**. That is a property of
the API, not a setting: neither `SFSpeechRecognizer` nor its replacement changes language
mid-sentence. So this is a sentence it cannot be asked to hear:

> 把那個 webhook 的 retry 改成 exponential backoff

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
mostly proper nouns. Whichever you pick, Clawdline passes it the same list of words it gives
Apple's recogniser — your own prompt history — so the terms you use survive being said inside
another language.

### Config

Nothing here is required; set it only to override what was found.

```jsonc
{
  "voice_engine": "auto",        // auto | apple | whisper
  "whisper_binary": "",          // a specific whisper-cli
  "whisper_model": ""            // a specific .bin
}
```

`auto` means "whisper if it is installed". Installing it was the deliberate act; being asked
again in a config file would be a second hoop for no reason. Set `"apple"` to keep the live
transcription even with whisper present.

---

## What changes when it is on

**The words you watched appear get replaced a sentence at a time.** whisper.cpp is not a
streaming transcriber, so it can only read a finished recording — which is why the live text is
still Apple's. Every pause of about two seconds ends a stretch: Whisper reads that stretch,
replaces it, and nothing after that point rewrites it. Talking for two minutes therefore costs a
handful of short reads rather than one long one at the end, and the text you have already read
stops moving. `voice_settle_seconds` changes the pause, and 0 turns it off.

**If the second pass fails, the first one stands.** A missing model or a binary that will not run
leaves the live text exactly where it was; it does not empty the box you were about to send.

**Editing what was dictated stops the replacement**, for the part you edited. Those words are
yours now, and the recording behind them is dropped rather than being transcribed over the top of
your correction.

**Silence costs nothing.** Under a quarter of a second is dropped, so a mis-click does not spin
up a model.
