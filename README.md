<div align="center">

# Clawdline

**Your Claude Code prompt line, at eye level.**

A Spotlight-style bar that floats in the middle of your screen. Type into it and the message
lands in Claude Code; press <kbd>⌘</kbd><kbd>J</kbd> and the session reads back in the same
place — laid out, not scraped. You never look at the corner of the terminal again.
Works with iTerm2 directly, and with every other terminal through tmux.

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/macOS-13%2B-black.svg)](#install)
[![Swift](https://img.shields.io/badge/Swift-5-orange.svg)](Sources)
[![Dependencies](https://img.shields.io/badge/dependencies-none-brightgreen.svg)](#install)

[![Ko-fi](https://img.shields.io/badge/ko--fi-support-ff5e5b.svg?logo=ko-fi&logoColor=white)](https://ko-fi.com/sainteye)

English · [繁體中文](README.zh-TW.md)

<img src="docs/assets/demo.gif" width="760" alt="Press Option-Space, type, press Enter. The message lands in Claude Code.">

</div>

---

## What it is for

**Claude Code asks you to look at the bottom-left corner of a full-screen terminal a few hundred
times a day.** Clawdline is a second place to type — one that appears where your eyes already
are, sends into the session you were last in, and hands focus straight back.

Everything else follows from one consequence: **if you are not going to look at the terminal, the
bar has to tell you what the terminal would have.**

## What it does that a prompt box does not

- **Takes dictation in two languages at once.** Words appear as you speak them; when you stop,
  Whisper reads the same audio back and replaces them. *"cambia el retry a exponential backoff"*
  is one sentence, and no live recogniser will hear it — Apple's changes language between
  sessions, never inside one. The pauses in your speech are where it settles, so earlier
  sentences stop moving while you carry on.
  → [Dictation](#talk-instead-of-type) · [Whisper setup](docs/whisper.md)

- **Reads the session back, laid out.** Not a screenshot of a terminal: headings, tables with
  borders, code — and runs of tool calls folded to one line each, because thirty lines of paths
  is not what you came back to read. A live line says what it is doing right now.
  → [The transcript pane](#reading-a-session-back)

- **Tells you which project, not just which task.** Two tabs can be working on tasks that read
  the same. The bar names the repository, its branch, what is uncommitted, a deploy in flight and
  a backlog — with the project's own pixel icon and colour.
  → [Which project](#which-project-not-just-which-task)

- **Takes a screenshot straight from your clipboard.** Drop a file anywhere on the window or
  paste an image; it appears as a thumbnail. What gets sent is the path, because that is what
  Claude Code can read.
  → [Files and images](#dropping-in-a-file-or-an-image)

- **Remembers what you sent.** <kbd>↑</kbd> and <kbd>↓</kbd> walk back through your own prompts,
  and those same words are what dictation is told to expect — so the terms you actually use are
  the ones it gets right.
  → [Use it](#use-it)

- **Wears a mascot you drew.** The character on the bar is a JSON file: a pixel grid, a palette
  and five animations. Swap it without forking anything.
  → [Mascots](#bring-your-own-mascot)

<div align="center">

<img src="docs/assets/voice.gif" width="760" alt="Speaking into the bar: the words appear live, then Whisper reads the recording back and replaces them.">

<img src="docs/assets/transcript.png" width="760" alt="The transcript pane: a heading, a bordered table and a code block, laid out rather than scraped.">

</div>

## Contents

- [Install](#install) · [Use it](#use-it)
- **Reading** — [the transcript pane](#reading-a-session-back) · [which project](#which-project-not-just-which-task)
- **Writing** — [dictation](#talk-instead-of-type) · [files and images](#dropping-in-a-file-or-an-image) · [which tab it sends to](#which-tab-does-it-send-to)
- **Making it yours** — [mascots](#bring-your-own-mascot) · [config](#config) · [other terminals](#other-terminals-run-claude-code-in-tmux)
- **Under it** — [how it works](#how-it-works) · [permissions and privacy](#permissions-and-privacy) · [limitations](#limitations) · [troubleshooting](#troubleshooting)
- **Going further** — [Whisper for mixed languages](docs/whisper.md) · [project status files](docs/project-status.md) · [mascot format](docs/mascots.md)

## Why

Claude Code draws its input box at the bottom of the terminal, and terminals are usually
full-screen. So the thing you look at a few hundred times a day sits in the bottom-left corner
of the display — the furthest point from where your eyes rest.

There is no setting for this. The input box is pinned to the bottom of the viewport, and a
plugin cannot move it: plugins add commands, agents, hooks, MCP servers and skills, not TUI
layout.

So Clawdline goes the other way. It leaves the terminal alone and gives you a second place to
type — one that appears where you tell it to, takes your text, and puts it in the session you
were last working in. Focus returns to whatever app you were in. Your eyes never travel.

## Install

Pick whichever you trust most — they all end up with the same app.

**Homebrew**

```bash
brew install --cask sainteye/tap/clawdline
xattr -dr com.apple.quarantine /Applications/Clawdline.app
```

**A script that fetches the latest release**

```bash
curl -fsSL https://raw.githubusercontent.com/sainteye/clawdline/main/install.sh -o install.sh
less install.sh          # 40 lines; worth the ten seconds
bash install.sh          # or: bash install.sh ~/Applications
```

**By hand** — grab the `.zip` from [Releases](https://github.com/sainteye/clawdline/releases/latest),
unzip it into `/Applications`, then:

```bash
xattr -dr com.apple.quarantine /Applications/Clawdline.app
```

**From source** — no package manager, no dependencies, a few seconds:

```bash
git clone https://github.com/sainteye/clawdline.git
cd clawdline && ./build.sh
open ~/Applications/Clawdline.app
```

<details>
<summary>Why the <code>xattr</code> line, and why building from source skips it</summary>

The release build is ad-hoc signed but **not notarized** — notarizing needs a paid Apple
Developer account. macOS quarantines anything downloaded from the internet and refuses to
open an unnotarized copy, so the flag has to come off, either with that command or by way of
System Settings → Privacy & Security → Open Anyway.

An app you compiled yourself was never downloaded, so it is never quarantined. If you would
rather not take a stranger's binary on faith, that is the option to use: the whole build is
`swiftc` over a handful of files you can read.

</details>

The first time you send something, macOS asks whether Clawdline may control iTerm2. Say yes;
it cannot send anything without that. Menu bar ✳ → **Launch at login** makes it stick around.

## Use it

Press <kbd>⌥</kbd><kbd>Space</kbd> in iTerm2, type, press <kbd>Enter</kbd>.

| key | what it does |
|---|---|
| <kbd>⌥</kbd><kbd>Space</kbd> | show / hide the bar |
| <kbd>Enter</kbd> | send to the current target tab |
| <kbd>⇧</kbd><kbd>Enter</kbd> | new line (the bar grows) |
| <kbd>Tab</kbd> / <kbd>⇧</kbd><kbd>Tab</kbd> | next / previous Claude Code tab |
| <kbd>⌘</kbd><kbd>K</kbd> | open the session list |
| <kbd>⌘</kbd><kbd>1</kbd>…<kbd>⌘</kbd><kbd>9</kbd> | jump straight to a session |
| <kbd>↑</kbd> / <kbd>↓</kbd> | history, when the field is empty |
| <kbd>⌘</kbd><kbd>J</kbd> | show what that session is saying |
| <kbd>⌘</kbd><kbd>F</kbd> | fill the screen with it |
| <kbd>⌘</kbd><kbd>R</kbd> | newest message at the top instead of the bottom |
| <kbd>⌘</kbd><kbd>+</kbd> / <kbd>⌘</kbd><kbd>−</kbd> / <kbd>⌘</kbd><kbd>0</kbd> | text size in that pane, remembered |
| <kbd>⌘</kbd><kbd>M</kbd> | browse / switch mascots |
| <kbd>⌘</kbd><kbd>D</kbd> | make the mascot dance |
| <kbd>⌘</kbd><kbd>/</kbd> | show the rest of the keys |
| <kbd>⌘</kbd><kbd>L</kbd> or click the microphone | dictate instead of typing |
| drag / <kbd>⌘</kbd><kbd>V</kbd> | drop a file or paste an image anywhere on the window |
| <kbd>Esc</kbd> | close |

<kbd>⌘</kbd><kbd>A</kbd> <kbd>⌘</kbd><kbd>C</kbd> <kbd>⌘</kbd><kbd>V</kbd> <kbd>⌘</kbd><kbd>X</kbd>
<kbd>⌘</kbd><kbd>Z</kbd> work as you expect.

**The hotkey only fires while iTerm2 is in front.** Everywhere else <kbd>⌥</kbd><kbd>Space</kbd>
is still whatever it was before you installed this. Set `"scope_app": ""` to make it global.

### Which tab does it send to?

Clawdline lists every iTerm2 session, checks each one's TTY against `ps`, and keeps the ones
actually running `claude`. It defaults to the session you were last looking at.

The bar always names its target along the bottom edge. **It never sends blind** — a prompt box
that will not tell you where the text goes is worse than no prompt box.

### Dropping in a file or an image

Drag a file anywhere onto the window, or paste an image, and it appears in the box as a
thumbnail — the picture you dropped, not forty characters of directory. What gets **sent** is
the path. That is the whole handoff: Claude Code reads files itself, images included, so a path
is the same thing you would have typed and needs nothing at the other end that is not already
there. What is on screen is for you; what goes down the wire is for Claude Code, and the moment
those are the same string one of them is being made worse to suit the other.

An image off the clipboard has no path yet, so one is written under
`~/Library/Caches/dev.sainteye.clawdline/drops/` and that path is inserted. Those files are the
only thing this leaves behind, so the most recent few are kept and the rest are removed.

### Talk instead of type

<kbd>⌘</kbd><kbd>L</kbd>, or the microphone at the right of the box, turns your voice into text
in it. **It stops on its own when you are done talking** — a pause of a couple of seconds fixes
a sentence, a longer silence ends the session, so a paragraph said in one go needs one keystroke
rather than two. Pausing does not stop it: the recogniser settles a sentence at every pause and
starts the next from nothing, so the sentences are stitched back together on this side rather
than the second one replacing the first. The rings around it follow the same audio being transcribed, so
a ring that will not move means the microphone is hearing nothing — a failure you would otherwise
find out about by reading an empty box afterwards.

**Mixed-language speech is not a switch Apple can offer you.** Neither of its speech APIs changes
language mid-sentence: one recogniser, one locale. What is available is a hundred phrases of
bias, and Clawdline spends them on your own prompt history — the words you have typed at
Claude Code are the words you would say to it, so `webhook`, `rebase` and the name of your repo
survive being said inside a Chinese sentence. It needs no word list to maintain, and a list you
have to curate is one that goes stale the week you write it.

Words still being worked on are drawn faded, and come up to full when they settle. An
underline is what macOS input methods use, and it was the first thing tried here — but it only
speaks to people who already know that convention, and a line under a sentence competes with the
sentence. Fading reads as "not all the way here" to anybody, and it puts the emphasis the right
way round: the words that have settled are the ones that look normal. **Dictation starts at the
caret**, so you can go back and say a sentence into the middle of what you have written. Every pause of about two seconds fixes what you have said so far, so earlier sentences
stop moving while you carry on — `voice_settle_seconds` sets the pause, 0 turns it off. A pause
means quiet *compared to the last few seconds*, not quiet compared to a number: an ordinary room
here measures a third of the way up the scale, so a fixed threshold would be this room and
nobody else's. Four seconds of it ends the session altogether (`voice_stop_seconds`), and a
sentence that broke off mid-clause is given longer than one that arrived with a full stop on it —
being late costs an open microphone in a quiet room, being early costs the keystroke this is
here to remove. Pressing <kbd>Enter</kbd> while still talking means "that was the
end of it": the microphone closes, the last stretch is read back, and then it sends — you do not
have to stop it first. You can also stop, fix a word by hand, and carry on talking: an edit anywhere in the box ends the
current run, and the next thing you say starts after the caret rather than being written into
the middle of the sentence you were correcting.

Recognition runs on this Mac for the dictation languages you have downloaded, and goes to Apple
for the ones you have not. The bar says which, for as long as it is listening. See
[Permissions and privacy](#permissions-and-privacy).

If you do speak two languages in one sentence, **[Whisper](docs/whisper.md) is an optional second
pass that handles it** — a `brew install` and a model file, after which Clawdline uses it without
being told to. It does not replace the live text: Apple's recogniser keeps writing as you speak,
and when you stop, Whisper reads the same recording and replaces the run with its version. The
feedback of one, the sentence of the other. That page has a prompt you can paste into Claude Code
to have it installed for you.

### Which project, not just which task

The bar names its target along the bottom edge, and a tab title is the *task* — "investigate the
webhook" reads much like another project's "investigate the webhook". So the line leads with the
repository, its branch, and how much is uncommitted:

    ▣ atrium  investigate the webhook  ⎇ main *3   9/10

If you use [claude-tools](https://github.com/sainteye/claude-tools) for your terminal status
line, the mark and the colour come from its registry — `~/.claude/project-icons.json` — so the
icon in the bar and the icon in the terminal are the same icon because they are the same row,
not because two programs were kept in step by hand. Clawdline only ever reads that file: it is
usually a symlink into a checkout, and writing through the link would replace it with a copy.

It can show more than the name. A deploy in flight draws its progress, a backlog shows the lane
that is asking for attention, a health check shows a dot — and the first two are links, so the run
or the page opens where you would have gone looking for it anyway.

None of that is computed by Clawdline: they are small JSON files under
`~/.claude/statusline-cache/`. **[docs/project-status.md](docs/project-status.md) is the format**,
with working examples the test suite parses, so the page cannot quietly stop being true. Anything
can write them — a cron job, a git hook, or claude-tools, which already does for its own status
line. Without them the footer simply has less to say.

Without a registry the colour is derived from the path instead, which is stable from one launch
to the next. The branch and the count come from one `git status --porcelain=v2 --branch`.

### Reading a session back

<kbd>⌘</kbd><kbd>J</kbd> opens a pane below the input showing what that session currently says,
refreshed about once a second and following <kbd>Tab</kbd> as you switch. The rest of the
screen blurs behind it, because reading a transcript is a different mode from firing off one
line. The text is selectable — copying an error out of it is most of the point.

It only auto-scrolls when you were already parked at the end where new lines land; being
yanked away while reading something further up is worse than not following at all. And an unchanged terminal produces an
identical capture, which is skipped entirely rather than relaid out under your eyes.

Where it can, the pane shows the **conversation** rather than the screen. Claude Code writes
each session to `~/.claude/projects/<project>/<session>.jsonl` as it goes, and that file has
the structure the screen only implies: who spoke, what they said, which tools ran. Reading it
means real message boundaries, full history rather than one viewport, and typography instead
of a screenshot — speakers get a label, prose gets a proportional face, tool calls recede into
monospace at the edge of the page.

<kbd>⌘</kbd><kbd>F</kbd> makes it the size of the screen — not macOS's full screen, which moves
the window to a Space of its own and is the opposite of what a panel you summon over your work
is for. It is a resize, animated, and the mascot has a routine for it.

Switching to another app puts the panel away, and coming back to the terminal takes it out
again — at whatever size it was. Leaving a panel you had open is "I need to see something for a
moment"; <kbd>Esc</kbd> is how you say "I am done", and something you closed on purpose stays
closed. Set `"reopen_on_return": false` if you would rather every appearance be one you asked
for.

<div align="center">
<img src="docs/assets/fullscreen.png" width="860" alt="⌘F: the same pane filling the screen, with finished runs of tool calls folded to one line each.">
</div>

<kbd>⌘</kbd><kbd>R</kbd> turns the whole thing round, newest message at the top. It follows
whichever end the newest message is at — auto-scrolling to the top rather than the bottom —
and it is remembered. Only the transcript flips: a terminal capture is a picture of a grid,
and reversing its lines would have a wrapped sentence reading upwards.

While the session is working, the pane carries a line saying what it is doing —
`Finagling… (5m 52s · ↓ 18.6k tokens)`. That one is scraped from the terminal even when
everything else comes from the file, because it is never written to the file: the transcript
records messages once they exist, and this is a spinner painted on the screen and erased again.

Runs of tool calls fold. A single answer can sit under thirty lines of paths and shell, and
the shell is not what you came back to read — so each finished run collapses to one line saying
how many steps it took and which tools ran, and clicking it opens the run back up. The run still
going never folds: that one is the part that is changing.

What Claude writes is Markdown, so the pane renders it: headings, lists, tables with real
borders, quotes, emphasis, and code. Leaving a table's pipes in and setting it in monospace
does not work — a CJK glyph comes from a fallback face whose advance is not reliably twice the
monospace one, so pipes that line up in the source land somewhere different on every row.
Anything unrecognised falls through as plain text, which is the one failure mode that matters:
a stray asterisk on screen is a blemish, a sentence swallowed by a parser is a bug.

Finding the right file takes three steps, because no record carries a tty: the session's
working directory gives the project folder, the tab title matches the `aiTitle` the transcript
recorded, and the most recent file breaks any tie. **The format is undocumented and can
change**, so every field is optional on the way in and anything unrecognised is skipped.

When there is no transcript — a plain shell, a non-Claude pane — it falls back to scraping the
terminal. iTerm2 hands over the **visible screen** and no more, since its scripting has no
scrollback; tmux gives the visible pane plus 200 lines of history. Set `output_mode` to
`terminal` or `transcript` to pin it either way.

**The card is frosted glass, and glass takes the colour of what is behind it.** A screen of
green diff or a bright page tints the whole thing and drags the text with it, so a dark layer
sits between the material and everything drawn on it. `card_opacity` is how much of it: 0 is
pure glass, 1 is opaque. Raise it if you work over bright or strongly coloured windows.

**On that fallback path, colour only survives through tmux.** `capture-pane -e` keeps the
escape sequences, which get parsed into real colour. iTerm2's scripting returns a plain string:
it will tell you which red it uses for ANSI red, but not which characters are red, so that path
arrives as plain text. None of this touches the transcript, which is coloured by what the text
means rather than by what the terminal drew.

Set `output_font` to whatever your terminal uses. The default is Menlo; a status line built out
of box-drawing characters comes out at the wrong widths in anything with different metrics,
which is what makes it look broken.

## Bring your own mascot

The character on top of the bar is **data, not code**. One JSON file holds the pixel grid, the
palette, every pose and every animation, so replacing it never means forking this repo.

```
~/.config/clawdline/mascots/clawd.json
```

Edit it, press <kbd>⌥</kbd><kbd>Space</kbd>, and the change is on screen. No rebuild.

### Browse and switch

<div align="center">
<img src="docs/assets/picker.png" width="620" alt="The mascot picker, listing clawd and mochi">
</div>

<kbd>⌘</kbd><kbd>M</kbd> lists every pack you have. Arrow keys **preview as you move** — the
character on the bar changes while the list is still open, so you pick by looking rather than
by reading names. <kbd>⌘</kbd><kbd>1</kbd>–<kbd>⌘</kbd><kbd>9</kbd> jumps straight to one, and
the menu bar ✳ has the same list.

Two ship with the app. [**docs/gallery.md**](docs/gallery.md) is where more get posted:

<div align="center">
<img src="docs/assets/dance.gif" width="420" alt="clawd">
<img src="docs/assets/mochi-dance.gif" width="420" alt="mochi">
</div>

### Make one

The intended way is to let Claude Code do it. Save a reference image, then:

> Here is a reference image: `~/Downloads/my-character.gif`
>
> Make it into a Clawdline mascot pack. The format is in `docs/mascots.md`, and
> `~/.config/clawdline/mascots/clawd.json` is a working example. Grid no larger than 20×16.
> Put the feet on the bottom row so it stands on the bar. Write the six routines: `pop`,
> `idle`, `typing`, `dance`, `cheer`, `stretch`. Save it as
> `~/.config/clawdline/mascots/my-character.json` and point the config at it.
>
> Then check your work: run
> `open "clawdline://snapshot?path=/tmp/m.png&routine=dance&t=0.3"`, look at the PNG, and fix
> whatever is wrong. Repeat until it reads like the reference.

That last instruction is the one that matters. `clawdline://snapshot` renders a frame of any
routine to a PNG **without needing Screen Recording permission**, so the agent can see what it
drew and iterate. Pixel art written blind comes out as a blob.

Full format reference, the six routine triggers, and notes on what reads well at this size:
**[docs/mascots.md](docs/mascots.md)**. Packs are pure data — a grid of characters, colours and
numbers — so one cannot execute anything; the worst a bad one does is refuse to load and say
why. `tools/validate-pack.py` checks a pack, and CI runs it on every pull request.

## How it works

Text does not go in as synthetic keystrokes, and it is not written to the terminal's pty —
you cannot write to another process's TTY on modern macOS. It goes through iTerm2's scripting
interface, wrapped in a bracketed paste:

```
ESC[200~ your text, newlines and all ESC[201~     ← one paste, not a row of Enters
CR                                                ← then a single Return to submit
```

Without that wrapper a two-line prompt submits itself after the first line. The other benefit
is that **the terminal never has to come to the front** — which is the entire point.

## Config

`~/.config/clawdline/config.json`. Menu bar ✳ → **Reload config** applies changes.

```jsonc
{
  "hotkey": "option+space",              // cmd / option / control / shift + one key
  "scope_app": "com.googlecode.iterm2",  // comma-separated; "" makes the hotkey global
  "y_fraction": 0.30,                    // where the bar's top edge sits, 0 = top of screen
  "width": 720,
  "language": "auto",                    // auto | en | zh-Hant
  "mascot": "clawd",
  "tmux_path": ""                        // empty = look in the usual places,
  "output_height": 340                    // ⌘J pane height, 80–900
  "output_font": "Menlo",                // match your terminal, or box-drawing breaks
  "output_mode": "auto",                 // auto | transcript | terminal
  "output_size": 11.5,                   // ⌘+ / ⌘- change this live
  "output_newest_first": false,          // ⌘R: newest at the top
  "card_opacity": 0.55,                  // 0 = pure glass, 1 = opaque
  "reopen_on_return": true,              // come back when the terminal does
  "backdrop": 0.5,                       // ⌘J background blur, 0 = none
}
```

Adding a language means writing one struct in [`Sources/Strings.swift`](Sources/Strings.swift)
and one line in the catalog. The compiler refuses to build a language that is missing a string,
so a translation cannot silently ship half-done. Pull requests welcome.

## Permissions and privacy

| what | why | when |
|---|---|---|
| **Automation → iTerm2** | the only way to put text into a session | once, on your first send |
| **Microphone + speech recognition** | dictation | only if you press the microphone |
| *(nothing else)* | no accessibility, no screen recording | — |

The global hotkey uses Carbon's `RegisterEventHotKey` rather than an `NSEvent` monitor
specifically to **avoid** the accessibility permission — a tool that opens a text box has no
business being able to read every key you press.

**Dictation is the one thing here that can use the network, and it says so while it does.**
macOS recognises speech on the Mac for the dictation languages you have downloaded, and sends
audio to Apple for the ones you have not. Which of the two is happening is written across the
bottom of the bar the whole time it is listening — the microphone is never open without that
line being on screen, and closing the panel stops it. If you would rather it never left the
machine, install the language in System Settings › Keyboard › Dictation.

Nothing else here talks to the network. Your prompt history lives in
`~/.config/clawdline/config.json` and goes nowhere.

## Other terminals: run Claude Code in tmux

iTerm2 is supported directly. Everything else — Terminal.app, Warp, Tabby, Ghostty,
Alacritty, Kitty — works if Claude Code is running inside **tmux**:

```bash
tmux new -s work
claude
```

That is the whole setup. Clawdline lists tmux panes alongside iTerm2 sessions, spots the ones
running `claude` the same way, and sends through `load-buffer` + `paste-buffer` with the same
bracketed-paste wrapper. Nothing else to configure, and **tmux needs no macOS permission at
all** — it is an ordinary subprocess, not cross-app automation.

If your terminal is not iTerm2, widen the hotkey scope so ⌥Space fires there too:

```jsonc
{ "scope_app": "com.apple.Terminal,com.googlecode.iterm2" }
```

<details>
<summary>Why not support those terminals directly?</summary>

Because they cannot receive text. Terminal.app has `do script`, which sounds like it would
work — it does not. A program blocked on `read` in one of its tabs never sees a byte of what
`do script` sends; the call returns success and nothing arrives. Warp and Tabby have no
equivalent interface at all.

The only remaining route is synthetic keystrokes, which needs the accessibility permission —
the right to observe every key you press, for a tool whose whole job is opening a text box —
and needs the terminal in front, which is the thing this exists to avoid. tmux gives the same
result for neither cost.

</details>

## Limitations

- **One direction.** Claude's replies still live in the terminal. That half scrolls upward
  anyway; this fixes the half that was nailed to the bottom-left corner.
- **tmux is required for non-iTerm2 terminals**, per the section above.

## Troubleshooting

Everything the app does is logged to `~/Library/Logs/Clawdline.log`: whether the hotkey
registered, whether the panel opened, what happened to every send.

- **Nothing happens on ⌥Space** — check the log for `hotkey registered`. If it is missing,
  another app owns that combination; pick a different one in the config.
- **"No Claude Code session found"** — the automation permission was probably declined. Run
  `tccutil reset AppleEvents dev.sainteye.clawdline`, then reopen the bar to be asked again.
- **A send fails** — the bar comes back with your text still in it and the reason along the
  bottom. It never eats what you typed.

## Contributing

Plain AppKit, no frameworks, no build system beyond `swiftc`.

```bash
./test.sh     # 478 checks, a couple of seconds
./build.sh    # builds and relaunches if it was running
```

The tests cover the parts a change can quietly break: pack decoding and validation, keyframe
sampling, colour parsing, hotkey specs, and the two parsers that decide where text gets sent
(`ps` output and `tmux list-panes`). Anything needing a window on screen is deliberately
absent — a test that cannot run in CI is a test nobody runs.

Comments explain *why* a thing is the way it is, especially where the obvious approach was
tried first and failed. Those notes are the useful part, so please keep the habit.

## Credits

The mascot is fan art of the pixel character that appears in Claude Code, known in the
community as **Clawd**. This project is not affiliated with, endorsed by, or connected to
Anthropic. Claude and Claude Code are trademarks of Anthropic.

The mascot lives in a swappable JSON file precisely so you can replace it with something of
your own — see [docs/mascots.md](docs/mascots.md).

## License

[MIT](LICENSE)
