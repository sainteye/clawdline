# Clawdline

**One bar for every Claude Code session already running on your Mac.**

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Platform](https://img.shields.io/badge/macOS-13%2B-black.svg)](#install)
[![Swift](https://img.shields.io/badge/Swift-5-orange.svg)](Sources)
[![Dependencies](https://img.shields.io/badge/dependencies-none-brightgreen.svg)](#install)

English · [繁體中文](README.zh-TW.md)

<img src="docs/assets/demo.gif" width="760" alt="Press Option-Space, type, press Enter. The message lands in Claude Code.">

Claude Code draws its input box at the bottom of a terminal window. That is fine for one session.
With four, the day is spent walking *to* sessions: to say anything to one, you find its tab; to
learn whether it is still working, you find its tab; and what you are choosing from is a row of tab
titles, which are *tasks* — two projects can be working on tasks that read alike. There is no
setting for this and no plugin can fix it: plugins add commands, agents, hooks, MCP servers and
skills, not TUI layout.

Clawdline is one place for all of them. Press <kbd>⌥</kbd><kbd>Space</kbd>, type, and the message
lands in the session you point it at. Press <kbd>⌘</kbd><kbd>K</kbd> and every session becomes a row
that says what it is doing: **working, finished, or waiting for an answer.**

**Nothing is installed into Claude Code.** No hooks, no MCP server, no wrapper around the `claude`
command, no edits to your settings. Clawdline reads the screens your sessions are already drawing
and the transcripts they are already writing — which is why the four sessions you started by hand an
hour ago appear in the list too, not just the ones something dispatched for you. iTerm2 is supported
directly; every other terminal works through tmux.

Nothing to migrate, nothing to undo. Quit it and your setup is exactly as it was.

## Features

| | |
| --- | --- |
| **Which session wants you** `⌘K`<br><br>Every session in one list. A working session carries the live line Claude Code draws for itself; a session with a question on screen is the loud one, because that is the only state costing you something for every second it goes unnoticed. Each row wears its project's own mark.<br><br>[How each state is decided →](docs/interface.md#which-session-wants-you) | <img src="docs/assets/sessions-live.gif" width="380" alt="The session list, live: the selection walks down it, one session is answered and goes quiet, another finishes, and a third starts asking."> |
| **Read a session back** `⌘J`<br><br>Not a screenshot of a terminal. Clawdline reads the session's transcript file, so you get real message boundaries, full history, headings, bordered tables and code — with finished runs of tool calls folded to one line each. `⌘F` fills the screen.<br><br>[What the pane does →](docs/interface.md#reading-a-session-back) | <img src="docs/assets/transcript.png" width="380" alt="The transcript pane: a heading, a bordered table and a code block, laid out rather than scraped."> |
| **The same sessions on your phone**<br><br>Your Mac serves a page; your phone opens it and reads every session, transcript and all — and types into them if you arm the second switch. Off by default, bound to loopback, every device paired by a code shown only on the Mac. Reaching it from outside is `cloudflared`, which is your own install.<br><br>[From a browser, or your phone →](#from-a-browser-or-your-phone) | <img src="docs/assets/web-wide.png" width="380" alt="The same page on a laptop: the session list down the left with the one that is waiting picked out in the accent colour, its transcript beside it, and a box to type in underneath."> |
| **Dictation that keeps up with two languages**<br><br>Words appear as you speak, and the recogniser is fed your own prompt history, so `webhook` and `rebase` survive being said inside a Chinese sentence. Claude Code's own `/voice` streams audio to Anthropic's servers, needs a Claude.ai account, and [does not support Chinese](docs/compatibility.md#claude-code-has-its-own-dictation-now). Add [Whisper](docs/whisper.md) — one `brew install` — and a second pass reads the same audio back, so one sentence can hold two languages.<br><br>[What it does while you talk →](docs/interface.md#talk-instead-of-type) | <img src="docs/assets/voice.gif" width="380" alt="Speaking into the bar: the words appear live, then Whisper reads the recording back and replaces them."> |

**Says it in the notch, too.** Your mascot lives in the camera housing. It sleeps while nothing
runs, leans out while something does, names the session that wants you, and dances when a long job
finishes. On a display without a notch it becomes a pill below the menu bar. `"notch": false`
removes it entirely. [More →](docs/interface.md#the-notch)

<img src="docs/assets/island.gif" width="760" alt="The menu bar, with the notch cut into it: the mascot leans out of the camera housing while one session runs, a count appears beside it when three do, then the shape stretches out to the right to name the session that is waiting — and when a long job finishes, a green dot and the mascot dancing.">

Also:

- **Your dev servers, where you already type** — `⌘S` lists each project's long-running processes,
  how long they have been up, every port as a link, and start/stop/restart. Clawdline never spawns a
  process of its own; it runs the commands your repo declares in `.devstack.json`.
  [Format →](docs/devstack.md) · [Adopting it →](docs/devstack-adopting.md)
- **Which project, not just which task** — the bar names the repository, its branch, what is
  uncommitted, a deploy in flight and a backlog, with the project's own icon and colour.
  [Format →](docs/project-status.md)
- **The terminal's tab follows** — move through the list and iTerm2 moves with you, without coming
  to the front. The bar's target and the tab in front of you stop being two different sessions.
- **Images and files** — drop a file on the window or paste an image. Images arrive in Claude Code
  as `[Image #3]`, exactly as a paste does; anything else goes as a path.
  [How →](docs/interface.md#dropping-in-a-file-or-an-image)
- **Prompt history** — <kbd>↑</kbd> and <kbd>↓</kbd> walk back through what you have sent, and those
  same words are what dictation is told to expect.
- **Bring your own mascot** — the character is one JSON file: a pixel grid, a palette and seven
  routines. No fork required. [Format →](docs/mascots.md) · [Gallery →](docs/gallery.md)
- **Fourteen languages** — English, Chinese (Traditional and Simplified), Japanese, Korean,
  Spanish, Portuguese, French, German, Russian, Italian, Hindi, Indonesian and Turkish. The
  interface follows the system, or pin one in the config.

> ### Connecting your own project
>
> Paste this repository's address at your Claude Code agent and ask it to connect your project.
> **[docs/connect.md](docs/connect.md) is written for it** — the files to create, the formats, and
> how to check its own work. Every integration is a small JSON file that Clawdline reads; nothing is
> installed and no dependency is added to your project.
>
> *"Connect this project to Clawdline — https://github.com/sainteye/clawdline"* is the whole
> instruction.

## Install

**Homebrew**

```sh
brew install --cask sainteye/tap/clawdline
xattr -dr com.apple.quarantine /Applications/Clawdline.app
```

**Script**

```sh
curl -fsSL https://raw.githubusercontent.com/sainteye/clawdline/main/install.sh -o install.sh
less install.sh          # 40 lines; worth the ten seconds
bash install.sh          # or: bash install.sh ~/Applications
```

**By hand** — download the `.zip` from
[Releases](https://github.com/sainteye/clawdline/releases/latest), unzip it into `/Applications`,
then run the `xattr` line above.

**From source** — no package manager, no dependencies, a few seconds:

```sh
git clone https://github.com/sainteye/clawdline.git
cd clawdline && ./build.sh
open ~/Applications/Clawdline.app
```

> **Why the `xattr` line?** Releases are ad-hoc signed but not notarized, because notarizing
> requires a paid Apple Developer account. macOS refuses to open an unnotarized download until the
> quarantine flag comes off. An app you compiled yourself was never downloaded, so building from
> source skips this entirely.

The first time you send something, macOS asks whether Clawdline may control iTerm2. Say yes — it
cannot send anything without that. Menu bar ✳ → **Launch at login** makes it stick around.

## Use it

Press <kbd>⌥</kbd><kbd>Space</kbd> in iTerm2, type, press <kbd>Enter</kbd>.

| Key | Action |
| --- | --- |
| <kbd>⌥</kbd><kbd>Space</kbd> | Show / hide the bar |
| <kbd>Enter</kbd> | Send to the current target |
| <kbd>⇧</kbd><kbd>Enter</kbd> | New line |
| <kbd>Tab</kbd> / <kbd>⇧</kbd><kbd>Tab</kbd> | Next / previous session |
| <kbd>⌘</kbd><kbd>K</kbd> | Open the session list |
| <kbd>⌘</kbd><kbd>1</kbd>…<kbd>⌘</kbd><kbd>9</kbd> | Jump straight to a session |
| <kbd>↑</kbd> / <kbd>↓</kbd> | History, when the field is empty |
| <kbd>⌘</kbd><kbd>J</kbd> | Read that session back |
| <kbd>⌘</kbd><kbd>F</kbd> | Fill the screen with it |
| <kbd>⌘</kbd><kbd>R</kbd> | Newest message at the top |
| <kbd>⌘</kbd><kbd>+</kbd> / <kbd>⌘</kbd><kbd>−</kbd> / <kbd>⌘</kbd><kbd>0</kbd> | Text size in that pane |
| <kbd>⌘</kbd><kbd>S</kbd> | The project's servers |
| <kbd>⌘</kbd><kbd>L</kbd> | Dictate instead of typing |
| <kbd>⌘</kbd><kbd>M</kbd> / <kbd>⌘</kbd><kbd>D</kbd> | Switch mascot / make it dance |
| <kbd>⌘</kbd><kbd>/</kbd> | Show the rest of the keys |
| Drag or <kbd>⌘</kbd><kbd>V</kbd> | Drop a file or paste an image |
| <kbd>Esc</kbd> | Close |

The hotkey only fires while your terminal is in front; everywhere else <kbd>⌥</kbd><kbd>Space</kbd>
is whatever it was before. Set `"scope_app": ""` to make it global.

**The bar always names its target along the bottom edge.** It never sends blind — a prompt box that
will not tell you where the text goes is worse than no prompt box at all.
[How the target is chosen →](docs/interface.md#which-session-it-sends-to)

## From a browser, or your phone

Turn it on in **Settings → Remote**. If the browser is on this Mac, *Open in a browser* mints a
device and opens the page already signed in. For a phone, *Pair a phone…* draws a QR code.

<img src="docs/assets/web.gif" width="300" alt="The page on a phone: six sessions, each with its project's mark, and the one that is asking pulled out in the accent colour. Then another session's transcript, where a run of two tool calls sits folded to one line until it is opened; then a reply typed into the box at the bottom and sent.">

It is off in a fresh install, and stays off until you go and switch it on — a listening socket is
the difference between a program on your machine and a service on your machine.

What stands in the way of a request, in the order it meets them:

- **Loopback only.** The listener is created with a required local endpoint, so there is no
  interface on your network to find it on. The way out is a tunnel that dials *out*, never a port
  that waits.
- **The `Host` header is checked first.** DNS rebinding cannot change `Host`, so a request naming a
  host this server does not answer to is refused on the spot.
- **Cross-site requests are refused**, on the `Sec-Fetch-Site` header a page cannot forge. Anything
  that mutates is checked against `Origin` as well.
- **Everything else needs a device token** — 256 random bits, stored as a SHA-256 and compared in
  constant time. There is no exception for loopback, because once a tunnel is up, a phone in another
  country arrives from `127.0.0.1` like everything else.
- **Pairing needs your screen.** The six-digit code appears on the Mac and is never in the reply the
  asker got. Five guesses, two minutes, one pairing at a time.
- **Reading and writing are two switches.** Reading hands over a repository name and a task title;
  writing is remote code execution, because Claude Code runs `bash`.
- **A tunnel refuses to start until something has been paired**, and every pairing, revocation and
  send is appended to `~/.config/clawdline/remote-audit.jsonl`.

A paired device can also subscribe to notifications and buzz when a session starts waiting for you.
The message is sealed to the device, and carries the project and the state — never the task text.
With sending on, it can start a new session too, in a directory this Mac has already worked in: the
client never sends a path, only an opaque id out of a list the Mac built for itself.

**[docs/remote.md](docs/remote.md)** has the threat model in full, including what this does *not*
defend against. **[docs/api.md](docs/api.md)** is the HTTP surface a script or a plugin talks to:
every session, every transcript, an event stream, and `curl` as the only SDK.

## How it works

**Reading.** Clawdline lists every iTerm2 session and tmux pane, checks each one's TTY against
`ps`, and keeps the ones actually running `claude`. State comes from each session's own screen — a
spinner line means working, a menu with a caret parked on it means waiting, and a screen that could
not be read reports *unknown* rather than *idle*, because drawing "no idea" as "idle" would be a
confident wrong answer about somebody's work. Where a transcript file exists, the
<kbd>⌘</kbd><kbd>J</kbd> pane reads that instead of the screen.

**Writing.** Text is not sent as synthetic keystrokes and is not written to the terminal's pty —
you cannot write to another process's TTY on modern macOS. It goes through iTerm2's scripting
interface (or `load-buffer` + `paste-buffer` in tmux), wrapped in a bracketed paste:

```
ESC[200~ your text, newlines and all ESC[201~     ← one paste, not a row of Enters
CR                                                ← then a single Return to submit
```

Without that wrapper, a two-line prompt submits itself after the first line. The other benefit is
that the terminal never has to come to the front, which is the entire point.

**Optional hooks.** Away from the bar, a reading happens every twenty seconds, so a permission
dialog can sit unnoticed for a while. **Settings → Claude Code hooks → Install** puts five entries
in `~/.claude/settings.json`; after that, a note lands the moment a turn starts, ends, or needs an
answer, and the reading happens in under a second instead. A note only says *when* to look — never
what the screen says — so the screen remains the authority. Removing the hooks leaves nothing
behind. [The full contract →](docs/hooks.md)

## Other terminals: run Claude Code in tmux

Terminal.app, Warp, Tabby, Ghostty, Alacritty and Kitty all work, provided Claude Code runs inside
tmux:

```sh
tmux new -s work
claude
```

That is the whole setup, and tmux needs no macOS permission at all — it is an ordinary subprocess,
not cross-app automation. If your terminal is not iTerm2, widen the hotkey scope so
<kbd>⌥</kbd><kbd>Space</kbd> fires there too:

```json
{ "scope_app": "com.apple.Terminal,com.googlecode.iterm2" }
```

**Why not support those terminals directly?** Because they cannot receive text. Terminal.app's
`do script` returns success and delivers nothing to a program blocked on `read`; Warp and Tabby have
no equivalent interface. The only route left is synthetic keystrokes, which needs the accessibility
permission — the right to observe every key you press, for a tool whose whole job is opening a text
box — and needs the terminal in front, which is the thing this exists to avoid.

## Configuration

Menu bar ✳ → **Settings…** has a control for everything worth changing, and every control applies
the moment you move it. Underneath is `~/.config/clawdline/config.json`, which is hand-editable and
stays the truth. Editing it while the app is running is fine: it writes back only what it changed
itself.

**The bar**

| Key | Default | |
| --- | --- | --- |
| `hotkey` | `option+space` | cmd / option / control / shift + one key |
| `scope_app` | `com.googlecode.iterm2` | comma-separated; `""` makes the hotkey global |
| `y_fraction` · `width` | `0.30` · `720` | where the bar sits, and how wide |
| `language` | `auto` | or any tag: `ja`, `pt`, `zh-Hant` … |
| `mascot` · `notch` | `clawd` · `true` | the character, and whether it lives in the notch |
| `follow_target` | `true` | the terminal's tab follows what the bar points at |
| `tmux_path` | `""` | empty looks in the usual places |

**Reading a session**

| Key | Default | |
| --- | --- | --- |
| `output_mode` | `auto` | `auto` · `transcript` · `terminal` |
| `output_font` | `Menlo` | match your terminal, or box-drawing breaks |
| `output_height` · `output_size` | `340` · `11.5` | pane height and text size |
| `output_newest_first` | `false` | <kbd>⌘</kbd><kbd>R</kbd> |
| `card_opacity` · `backdrop` | `0.55` · `0.5` | glass and blur; raise over bright windows |
| `reopen_on_return` | `true` | come back when the terminal does |

**Dictation, files, integrations**

| Key | Default | |
| --- | --- | --- |
| `voice_settle_seconds` | `1.8` | how long a pause ends a sentence; 0 = off |
| `voice_stop_seconds` | `4.0` | how long a silence ends the session |
| `voice_vocabulary` | `[]` | names a transcriber cannot be expected to know |
| `voice_language` | `auto` | pin the language; `voice_engine` and the Whisper keys are in [whisper.md](docs/whisper.md) |
| `send_images_as_paste` | `true` | images arrive as `[Image #3]`, not as a path |
| `hooks` | `true` | believe Claude Code's hooks when installed |
| `status_dir` · `icons_file` | `""` | project status files and the icon registry |

**Remote**

| Key | Default | |
| --- | --- | --- |
| `remote` · `remote_port` | `false` · `7717` | serve the web interface; loopback only |
| `remote_write` | `false` | may a paired device type, or only read |
| `remote_tunnel` | `off` | `off` · `quick` · `named` |
| `remote_tunnel_name` · `remote_hostname` | `""` | both required for a named tunnel |
| `cloudflared_path` | `""` | empty looks where package managers put it |
| `push_on_finish` · `push_on_deploy` | `true` · `false` | when a phone should buzz |

## Permissions and privacy

| What | Why | When |
| --- | --- | --- |
| **Automation → iTerm2** | the only way to put text into a session | once, on your first send |
| **Microphone + speech recognition** | dictation | only if you press the microphone |
| *(nothing else)* | no accessibility, no screen recording | — |

The global hotkey uses Carbon's `RegisterEventHotKey` rather than an `NSEvent` monitor specifically
to **avoid** the accessibility permission: a tool that opens a text box has no business being able
to read every key you press.

**Two things here can use the network, and both are switches you threw.** Remote access is one — off
in a fresh install, loopback only until you point it at a tunnel, and it is your own `cloudflared`
install that carries anything off the machine. Dictation is the other: macOS recognises speech
locally for the dictation languages you have downloaded and sends audio to Apple for the ones you
have not, and which of the two is happening is written across the bottom of the bar the whole time
it is listening. Install the language in System Settings › Keyboard › Dictation if you would rather
it never left the machine.

With remote access off and the microphone untouched, nothing here talks to the network at all. Your
prompt history lives in `~/.config/clawdline/config.json` and goes nowhere.

## Requirements and limitations

- **Apple silicon, macOS 13 or newer.** The build is arm64 only, so a release download will not
  start on an Intel Mac. Building from source there is a one-word change to `build.sh`, and
  untested.
- **iTerm2, or tmux for everything else.**
- **One direction.** Claude's replies still live in the terminal — <kbd>⌘</kbd><kbd>J</kbd> reads
  them back, but the bar is for what you send. That half scrolls upward anyway; this fixes the half
  nailed to the bottom-left corner.
- **Claude Code's screen and transcript format are not a promised interface.** Every field is
  optional on the way in and anything unrecognised is skipped.
  [Which versions this was run against →](docs/compatibility.md)

## Troubleshooting

Everything the app does is logged to `~/Library/Logs/Clawdline.log`.

- **Nothing happens on <kbd>⌥</kbd><kbd>Space</kbd>** — check the log for `hotkey registered`. If it
  is missing, another app owns that combination; pick a different one in the config.
- **"No Claude Code session found"** — the automation permission was probably declined. Run
  `tccutil reset AppleEvents dev.sainteye.clawdline`, then reopen the bar to be asked again.
- **A send fails** — the bar comes back with your text still in it and the reason along the bottom.
  It never eats what you typed.

## Documentation

| | |
| --- | --- |
| [The bar, up close](docs/interface.md) | the session list, the <kbd>⌘</kbd><kbd>J</kbd> pane, dictation, files, the notch |
| [Connecting a project](docs/connect.md) | written for an agent: the files to create and how to check them |
| [The dev stack](docs/devstack.md) · [adopting it](docs/devstack-adopting.md) | `.devstack.json`, and the three heights of adopting it |
| [Project status files](docs/project-status.md) | the mark, the colour, the deploy, the backlog |
| [From somewhere else](docs/remote.md) · [the API](docs/api.md) | the threat model in full, and the HTTP surface |
| [Hooks](docs/hooks.md) | the five events, and why the screen still decides |
| [Whisper](docs/whisper.md) | dictating in more than one language |
| [Mascot packs](docs/mascots.md) · [gallery](docs/gallery.md) | the format, and where packs get posted |
| [Versions](docs/compatibility.md) | which Claude Code releases this was run against |

## Contributing

Plain AppKit, no dependencies, no build system beyond `swiftc`.

```sh
./test.sh     # 1264 checks, a couple of seconds
./build.sh    # builds and relaunches if it was running
swift build   # only so your editor can index the code
```

[CONTRIBUTING.md](CONTRIBUTING.md) has the rest: where things are, how to add a language or a
mascot, and what a third way of sending text would look like. Corrections to any of the fourteen
translations are welcome — the ones nobody here speaks natively are the ones most likely to need
them.

## Credits

The mascot is fan art of the pixel character that appears in Claude Code, known in the community as
**Clawd**. This project is not affiliated with, endorsed by, or connected to Anthropic. Claude and
Claude Code are trademarks of Anthropic.

Putting live agent activity in the MacBook's camera housing is
[CLI Island](https://github.com/bistin/cc-island) by [bistin](https://github.com/bistin), which got
there first; the implementation here is its own and works differently, but the idea is borrowed with
thanks. The shape of the notch itself comes from
[DynamicNotchKit](https://github.com/MrKai77/DynamicNotchKit) by way of
[boring.notch](https://github.com/TheBoredTeam/boring.notch).

[![Support Clawdline on Ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/sainteye)

## License

[MIT](LICENSE)
