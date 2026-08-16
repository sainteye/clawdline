<div align="center">

# Clawdline

**Your Claude Code prompt line, at eye level.**

A Spotlight-style bar that floats in the middle of your screen and sends what you type
straight into a Claude Code session — without ever looking at the terminal.
Works with iTerm2 directly, and with every other terminal through tmux.

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/macOS-13%2B-black.svg)](#install)
[![Swift](https://img.shields.io/badge/Swift-5-orange.svg)](Sources)
[![Dependencies](https://img.shields.io/badge/dependencies-none-brightgreen.svg)](#install)

English · [繁體中文](README.zh-TW.md)

<img src="docs/assets/demo.gif" width="760" alt="Press Option-Space, type, press Enter. The message lands in Claude Code.">

</div>

---

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
| <kbd>⌘</kbd><kbd>M</kbd> | browse / switch mascots |
| <kbd>⌘</kbd><kbd>D</kbd> | make the mascot dance |
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
> Put the feet on the bottom row so it stands on the bar. Write the five routines: `pop`,
> `idle`, `typing`, `dance`, `cheer`. Save it as
> `~/.config/clawdline/mascots/my-character.json` and point the config at it.
>
> Then check your work: run
> `open "clawdline://snapshot?path=/tmp/m.png&routine=dance&t=0.3"`, look at the PNG, and fix
> whatever is wrong. Repeat until it reads like the reference.

That last instruction is the one that matters. `clawdline://snapshot` renders a frame of any
routine to a PNG **without needing Screen Recording permission**, so the agent can see what it
drew and iterate. Pixel art written blind comes out as a blob.

Full format reference, the five routine triggers, and notes on what reads well at this size:
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
  "tmux_path": ""                        // empty = look in the usual places
}
```

Adding a language means writing one struct in [`Sources/Strings.swift`](Sources/Strings.swift)
and one line in the catalog. The compiler refuses to build a language that is missing a string,
so a translation cannot silently ship half-done. Pull requests welcome.

## Permissions and privacy

| what | why |
|---|---|
| **Automation → iTerm2** | the only way to put text into a session. Asked once, on first send. |
| *(nothing else)* | no accessibility, no screen recording, no network. |

The global hotkey uses Carbon's `RegisterEventHotKey` rather than an `NSEvent` monitor
specifically to **avoid** the accessibility permission — a tool that opens a text box has no
business being able to read every key you press.

Clawdline talks to nothing but iTerm2 on your own machine. Your prompt history lives in
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
./test.sh     # 84 checks, about two seconds
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
