# Cross-platform inventory: how every feature works on Linux and Windows

> The user's words (2026-09-17, translated from Chinese): "Apart from the few features only a Mac has, it should work across platforms." "You can rethink how these
> things can be used across platforms; where something really can't be done, tell me the limits, and let's discuss how to present it."
>
> **This is a decision document, not an implementation.** Its job is that once you have read the summary table, you know which things on other operating systems
> are "just do the same", which are "doable, but with a different interaction", and which are "this machine really does not have it, and has to say so".

## 0. How this document is edited together

Several children write this document section by section; **the skeleton and the summary table are maintained by the skeleton owner, and everyone else touches only their own section**. Three rules:

1. **The summary table (§3) has one author.** Do not edit its rows directly. If your conclusion differs from the table, write in **your own section**
   a line `**Summary-table correction**: <row> → <new conclusion>`, and root changes them all when merging. The reason: when two people edit one row of a markdown table
   at the same time, the whole block conflicts, and by the time the conflict is resolved somebody's words have always been eaten.
2. **Wrap every section in anchors; what is inside the anchors is yours, and what is outside them you do not touch.**

   ```markdown
   <!-- section:<your id> owner:task-xxxxxxxx -->
   ### 4.14 <your subject>
   …your content…
   <!-- /section:<your id> -->
   ```

   **A new section goes at the end of §4**, numbered with the next unused number; do not insert it between others — inserting moves the number of every
   later section, which amounts to editing other people's files. Anchor ids already used (do not repeat them):

   `sessions`, `screen`, `clipboard`, `whisper`, `secrets`, `notifications`, `autostart`,
   `hotkey`, `notch`, `shell`, `filesystem`, `swiftstore`, `fidelity`. (In the anchors below, the owner's real task id is shown as the fixture `task-aaaaaaaa`; one task wrote all of them.)

   Another task is known to be working on one of these subjects: **the notch island belongs to a separate task** (it claimed
   `shell/darwin/NotchIsland.swift` and `docs/replica.md`); §4.9 writes only the skeleton of the decision, and that task is authoritative on the details.
3. **Four status markers, no others**, written on one line at the start of each section:

   | Marker | Meaning |
   |---|---|
   | `implemented` | Code already runs this path on this platform |
   | `implementable` | The approach is settled and the components exist; it just has not been written |
   | `degraded` | The original thing cannot be done, and a different interaction replaces it; its cost has to be written down |
   | `unsupported` | This machine does not have the thing, and has to say so by name |

**Every Linux/Windows judgement in this document comes from reading source code and from existing knowledge; none of it was measured on a real Linux or Windows
machine.** Once the user authorizes it, AWS machines will be opened for real tests, and §7 is the minimal checklist for that. Everything I am not sure of
is collected in §8; do not treat any of it as known.

---

## 1. In one sentence

**The Go daemon itself is almost cross-platform; what is not are the five things it reaches out to touch — the terminal, the clipboard, the keyboard,
secret storage and the desktop.** Executables for six platforms can already be produced from one machine (`plan.md` §P5), and the Linux one already starts
in a real container; the remaining problems all sit in the adapters and the native shells.

The product takes a different shape on each of the three platforms, and that is **the first thing to make clear at the product level**:

| | macOS | Linux | Windows |
|---|---|---|---|
| **Remote-controlling sessions you already have open** | ✅ tmux + iTerm2 | ✅ tmux (without tmux, visible only, no control) | ❌ none natively (needs tmux from WSL or MSYS2) |
| **Sessions it opens and keeps itself** | ✅ tmux | ✅ tmux / its own pty | ⚠️ needs ConPTY, not yet implemented |
| **Desktop integration (hotkey, tray, start at login)** | ✅ native shell | ⚠️ complete on X11, limited on Wayland, none without a desktop | ⚠️ needs a WebView2 shell |
| **The console web page itself** | ✅ | ✅ | ✅ |

**Windows users get only the "sessions it opens itself" half** (`plan.md` §7 already says so), and this document expands that half-sentence
into a table you can follow.

---

## 2. Two hard rules narrow the options first

Before any approach is chosen, two of this project's rules cut away half the candidates:

### 2.1 `CGO_ENABLED=0`

Producing executables for six platforms from one machine (`plan.md` §P5) depends on exactly this. **What it cuts away is every path that links a C library**:

| What we want | The cgo way | The pure-Go alternative |
|---|---|---|
| macOS Keychain | Security.framework, `go-keychain` | A `/usr/bin/security` subprocess, or leave it to the Swift shell |
| Linux clipboard | `golang.design/x/clipboard` (X11 lib) | `wl-copy`/`xclip`/`xsel` subprocesses |
| Linux global hotkey | Xlib `XGrabKey` | `xgb` (a pure-Go X11 protocol client) or a D-Bus portal |
| Linux shell | WebKitGTK (gotk3) | **No shell**; use the system browser |
| Windows shell | `webview/webview_go` | `go-webview2` (pure-Go COM) |
| Windows processes / DPAPI / Job Object | — | `golang.org/x/sys/windows` has them all |

**Conclusion: the whole Windows column can be done in pure Go; Linux desktop integration relies on subprocesses and D-Bus; the macOS native side
stays in the Swift shell, untouched.** This rule blocks nothing that really needs doing, but it decides how it is done.

### 2.2 "Don't take over your keyboard"

The first rule of `plan.md` §1. It has two concrete consequences across platforms, and both are **refusing a path that looks feasible**:

- **Linux does not use `TIOCSTI`.** Stuffing characters into someone else's tty is technically possible, but that is a keylogger-grade
  capability, and newer kernels turn it off by default (`dev.tty.legacy_tiocsti`). No tmux means no control —
  "can see, can't touch" is the honest answer; sneaking keys in is not.
- **Linux does not use `/dev/input` to grab global hotkeys.** Same reason: that requires the `input` group, which amounts to being able to read every
  character you type. The old `HotKey.swift` chose Carbon over an `NSEvent` global monitor for exactly this reason
  ("a tool that opens a text box should not be able to read every key you press"). Where Wayland has no proper way, the right answer is to degrade,
  not to switch to a more powerful path.

---

## 3. Summary table

Difficulty: **S** = half a day or less; **M** = one to three days; **L** = a week or more; **X** = a new component has to be built first.
The "Today" column is clawdline-go on macOS (master `d40545e`).

### 3.1 Terminals and sessions

| Feature | Relies on | Today | Linux | Windows | How it shows when it can't | Difficulty |
|---|---|---|---|---|---|---|
| Inventory of existing sessions (tmux) | `tmux list-panes` | ✅ `terminal/tmux.go` | ✅ same path, zero changes | ❌ no native tmux; only in WSL/MSYS2 | Windows: the list shows only owned sessions, and the page says "this machine has no tmux" | S |
| Inventory of existing sessions (iTerm2) | osascript/JXA | ✅ `iterm_darwin.go` | ❌ no iTerm2 | ❌ Windows Terminal has **no remote-control API at all** | Unsupported by name (`hosts_other.go` already returns only tmux) | — |
| Process inventory | `/bin/ps -ax -o tty,pid,pgid,tpgid,command` | ✅ `process/ps_unix.go` (darwin\|\|linux) | ✅ works; better to read `/proc` (slim containers have no `ps`) | ⚠️ `process/ps_windows.go` honestly returns `Complete:false` today | Windows: `Complete:false` + "process inventory on this machine is not implemented yet" | M |
| Opening its own session | `tmux new-session -d` | ✅ | ✅ | ⚠️ needs ConPTY (§4.1) | — | L |
| Send message / send key / interrupt / close | `send-keys` (`-l`, `-H`), `C-c`, `kill-pane` | ✅ | ✅ | ⚠️ ConPTY: write straight to the input pipe (`\x03` is the interrupt) | — | M |
| Reading the screen (attached) | `capture-pane -p -e -J` | ✅ | ✅ | ❌ no tmux, no screen | "This session's screen cannot be read", not a blank screen | — |
| Reading the screen (owned) | — | — | Own pty + VT parser | ConPTY + VT parser (**the same component**) | — | X |
| Live screen push | `pipe-pane` → FIFO; readability is the signal | ✅ `signal_unix.go` | ✅ same path | ❌ no mkfifo; `signal_other.go` returns nil | Already right: the screen is marked `on-demand`, and does not pretend to 4 ms | — |
| Live screen push (owned) | — | — | The pty output itself is the signal | The ConPTY output pipe itself is the signal | — | S (comes with ConPTY) |
| Showing it on screen (focus/reveal) | `select-pane` + iTerm2 activate | ✅ | ⚠️ only selection inside tmux; **Wayland has no protocol for a client to raise a window itself** | ⚠️ same; `SetForegroundWindow` has a foreground lock | Already right: `ok` promises only "selected", not that the window is in front of you | S |
| Killing a whole process tree | `Setpgid` + `killpg` | ✅ `supervisor_unix.go` | ✅ | ⚠️ needs a Job Object; today it honestly refuses to start | Already right: better not to start than to let the caller believe it can cancel | M |

### 3.2 Images, clipboard, voice

| Feature | Relies on | Today | Linux | Windows | How it shows when it can't | Difficulty |
|---|---|---|---|---|---|---|
| Attaching / pasting / dragging images in the browser | HTML5, all in the front end | ✅ | ✅ zero changes | ✅ zero changes | — | — |
| Image decoding (PNG/JPEG/GIF) | Go standard library | ✅ | ✅ | ✅ | — | — |
| Image decoding (HEIC/TIFF/WebP/BMP) | `/usr/bin/sips` | ✅ `decode_darwin.go` | ❌ `ErrDecoderUnavailable` | ❌ same | Already right: returns `unsupported_image`, does not silently accept | S (pure-Go webp/bmp decoders can fill in) |
| Handing an image to the assistant in the terminal | Borrow the macOS pasteboard, then restore it | ✅ `pasteboard_darwin.go` | ❌ see §4.3, **recommended not to do** | ❌ same | Already right: `ErrPasteboardUnsupported` → hand over a path instead | — |
| Voice recording | Browser MediaRecorder → 16 kHz PCM | ✅ | ✅ needs a secure context | ✅ same | An http origin that is not localhost: the microphone button must say "this address cannot record" | S |
| Speech to text | `whisper-cli` + `ggml-*.bin` | ✅ | ✅ search paths already written | ✅ search paths already written | Already right: `ErrNoBinary` and `ErrNoModel` are separate | S (what is missing is install guidance, §4.4) |

### 3.3 Secrets, permissions, storage

| Feature | Relies on | Today | Linux | Windows | How it shows when it can't | Difficulty |
|---|---|---|---|---|---|---|
| Device and token storage | 0700 directory + 0600 files + atomic rename | ✅ `devices/`, `cloudkeys/` | ✅ correct natively | ❌ **Windows has no 0600**; `os.Chmod` only changes the read-only bit | See §4.5: needs NTFS ACLs + DPAPI; this is the only blocking security gap today | M |
| Secrets in the system keystore | — | ❌ all files | Secret Service (D-Bus); none when headless | DPAPI/Credential Manager (doable in pure Go) | The settings page says plainly "this machine uses files"; don't pretend | M |
| Refusing symlinks | `O_NOFOLLOW` + Lstat + same-file | ✅ | ✅ | ⚠️ `nofollow_other.go` has only Lstat; junctions/ADS/short names remain | §4.11 | M |
| Event storage | modernc.org/sqlite (pure Go) | ✅ | ✅ | ✅ but WAL cannot live on a network drive | If the state directory is on UNC or a mapped drive, refuse at startup and say why | S |
| State directory location | `config.Dir()` | ✅ | `$XDG_CONFIG_HOME` → `~/.config` | `%APPDATA%` | — | — |
| Path-traversal protection on the documents route | Extension allowlist + root settled first | ✅ `documents/` | ✅ | ⚠️ Windows-specific checks needed (§4.11) | — | M |

### 3.4 Desktop integration

| Feature | Relies on | Today | Linux | Windows | How it shows when it can't | Difficulty |
|---|---|---|---|---|---|---|
| Native window (webview) | WKWebView | ✅ `shell/darwin/` | **Recommended: no shell**, use the system browser | WebView2 + `go-webview2` | Linux: `clawdline open` is already the complete answer | L (Windows) |
| Menu bar / system tray | NSStatusItem | ✅ | StatusNotifierItem (D-Bus); **GNOME has no tray by default** | `Shell_NotifyIcon` | GNOME: fall back to a badge on the browser tab (§4.9) | M |
| Global hotkey | Carbon `RegisterEventHotKey` | ✅ `HotKey.swift` | Possible on X11; **basically impossible on Wayland** (§4.8) | `RegisterHotKey` (user32) | Wayland: the settings page switches to "set a shortcut in your desktop that points to `clawdline panel`" | M |
| Start at login | `SMAppService` | ✅ (off by default) | `systemd --user` + `loginctl enable-linger`; or XDG autostart | `HKCU\...\Run` | No desktop and no systemd: print the manual commands | M |
| Local desktop notifications | **The old version had none** (measured: no `UNUserNotificationCenter` in `Sources/`) | — | `org.freedesktop.Notifications` | Toast (needs an AUMID) | See §4.6: notifications have three outlets, not one switch | M |
| Push to the phone | Web Push (RFC 8291 + VAPID) | ❌ not ported | ✅ cross-platform by nature | ✅ cross-platform by nature | — | L |
| Notch island | The MacBook's physical notch | ❌ not ported (a separate task is in progress) | No notch | No notch | Abstracted into a `Presence` port; see §4.9 | M |
| Drag and drop onto the native window | AppKit | ❌ not ported | — | — | The browser's HTML5 drag and drop already covers it | — |
| Dock/taskbar icon | `.icns` | ✅ | `.desktop` + PNG | `.ico` | — | S |

### 3.5 Remote and interoperation

| Feature | Relies on | Today | Linux | Windows | How it shows when it can't | Difficulty |
|---|---|---|---|---|---|---|
| Local authentication gate | File token + cookie | ✅ | ✅ | ⚠️ the same permission problem as §3.3 | — | — |
| `clawdline open` opening a browser | `open`/`xdg-open`/`rundll32` | ✅ written for all three | ✅ no effect when headless → `--print` | ✅ | Headless: `--print` prints the URL (implemented) | — |
| Six-digit pairing | Printed by the CLI | ✅ | ✅ | ✅ | Platforms without a shell use the CLI anyway (`remote.md` §5 says so) | — |
| tunnel | cloudflared, brought by the user | ❌ not done | ✅ official binaries exist | ✅ official binaries exist | — | M |
| Cloud bridge | PROTOCOL.md | ❌ not done | ✅ pure network | ✅ pure network | — | L |
| Reading the old Swift app's store | `~/.config/clawdline` | ✅ `swiftstore/` | Empty by nature (that machine has no old app) | Empty by nature | The usage page falls back to transcripts (implemented); the crown and the delivery check are simply absent | — |
| Usage ledger | `~/Library/.../usage.sqlite3` | ✅ | ❌ `ObservabilityDir()` returns a value only on darwin | ❌ same | Already right: falls back to computing from transcripts | — |
| Reading transcripts / plan quotas | `~/.claude`, `~/.codex` | ✅ | ✅ same paths | ✅ `%USERPROFILE%` | — | — |
| Schedules | Pure Go | ✅ | ✅ | ✅ | — | — |

---

## 4. Item by item

<!-- section:sessions owner:task-aaaaaaaa -->
### 4.1 Windows has no tmux: how sessions are opened and read

`degraded` (`implementable` in WSL mode)

This is the biggest question in the whole document, because the first rule of `plan.md` §1 — "coordinate the sessions you already have open, don't replace
them" — **cannot fully hold** on Windows. The reason is not laziness; Windows simply does not have the thing this product needs:

- **Windows has no native tmux.** tmux needs a POSIX pty. MSYS2/Cygwin have ports, but that pty is emulated,
  and it controls only processes running in the same MSYS environment; native Windows console applications behave differently under it.
- **Windows Terminal cannot be remote-controlled.** Its `wt.exe` command line can **open** a new tab (`wt -w 0 new-tab`),
  but there is **no public API to send keys to, or read the screen of, a tab that already exists**. So Windows Terminal can only be
  an "opener", not a `TerminalHost`.
- **Windows has no tty and no foreground process group.** `ps_unix.go` relies on `pgid == tpgid` to decide "which process this terminal is
  running right now"; that concept does not exist on Windows. A console can have several attached processes,
  and there is no "foreground group".

So Windows has to be discussed as three shapes; **the recommendation is to build all three, and to label clearly on the settings page which one is in use**:

#### (a) WSL mode — the recommended default

**The whole daemon runs as the Linux binary inside WSL2**, and everything goes the Linux way: tmux, pty, procfs and
FIFOs are all there. On the Windows side there is only a browser (or a future WebView2 shell) connecting to `http://127.0.0.1:7727` — WSL2's
localhost forwarding makes this need no configuration at all.

- **Cost 1**: project files must live in WSL's file system (`/home/...`) to get normal I/O speed and inotify;
  under `/mnt/c/...` both break.
- **Cost 2**: projects that use the native Windows toolchain (MSVC, .NET) are not covered.
- **Cost 3**: `~/.claude` and `~/.codex` are the copies inside WSL, not the Windows ones. A user who has run an assistant on both sides
  will see two sets of history. **The page has to say this plainly**, or they will think their sessions vanished.
- The work on this path is almost zero (a linux/amd64 binary already exists, and has already run in a container), **so it should be
  the first usable shape on Windows**, rather than waiting for ConPTY.

#### (b) Native Windows + owned sessions — ConPTY

The daemon itself uses **ConPTY** (`CreatePseudoConsole`, Windows 10 1809+) to open a pseudo console, and runs the
assistant inside it. The daemon is this pty's host, so:

| What needs doing | How on tmux | How on ConPTY |
|---|---|---|
| Send a message | `send-keys -l <text>` + `Enter` | Write UTF-8 + `\r` to the input pipe |
| Send one raw key (for menus) | `send-keys -H <hex>` | Write that one byte to the input pipe |
| Interrupt | `send-keys C-c` | Write `\x03` to the input pipe |
| Close | `kill-pane` | Close the pty + kill the Job Object |
| Resize | tmux handles it | `ResizePseudoConsole` |
| Read the screen | `capture-pane -p -e -J` | **See the next section: parse the VT stream ourselves** |

Doable in pure Go: `golang.org/x/sys/windows` has every syscall, or use `github.com/UserExistsError/conpty` directly
(MIT, pure Go). **No cgo needed.**

- **This is the only "complete" path on Windows**, but it covers only sessions the daemon opens itself.
- **Cost**: a claude the user opens by hand in Windows Terminal can be neither read nor typed into by the daemon.
  The product has to accept this: **to use Clawdline on Windows, open sessions from Clawdline.**

#### (c) Native Windows + "visible only" for existing sessions

Even without control, they should be visible. `CreateToolhelp32Snapshot` (pure Go) can list pid/ppid/executable name;
the command line (which both `classify()` and `resumeID` need) requires WMI (`Win32_Process.CommandLine`, through pure-Go
COM bindings) or reading the PEB with `NtQueryInformationProcess`.

- **Presentation**: these sessions appear in the list with their icons and titles as usual, but **their state says plainly "detected; this machine cannot control it"**,
  and the composer is disabled with the reason attached. That is better than having them vanish from the list — the user knows the session is running, and not being able to see it would be the fault.
- This matches exactly the `Complete:false` that `ps_windows.go` returns today: **being unable to look and seeing nothing are two different things**, and that file
  already has this rule written down.

**Summary-table correction**: none.

<!-- /section:sessions -->

<!-- section:screen owner:task-aaaaaaaa -->
### 4.2 How screen content is read (ConPTY's buffer)

`implementable` (needs a new component)

There are two entirely different paths for reading the screen, and the dividing line is not the operating system; **it is who opened the session**:

| | attached (opened by someone else) | owned (opened by the daemon itself) |
|---|---|---|
| macOS | tmux `capture-pane`; iTerm2 has `Capture` | pty + VT parser |
| Linux | tmux `capture-pane` | pty + VT parser |
| Windows | **None** | ConPTY + VT parser |

In other words, **"reading the screen of an owned session" is the same component on all three platforms**, and it is worth writing first.

#### ConPTY gives you a VT stream, not a buffer

This is easy to misunderstand, so to be clear: `CreatePseudoConsole` gives you a pair of pipes. What you write in is keyboard input;
what you **read out is a stream of already-rendered VT escape sequences** — cursor moves, line clears, colours, repaints, all of it.
It is not a two-dimensional character array you can dump at any moment.

So to have a "screen", the daemon has to maintain a terminal state machine itself: feed the stream into it, and the state machine always holds
the current screen, which can be output at any time as the same thing `capture-pane -p -e -J` gives.

**The old version evaluated this, and rejected it.** In the table in `LiveScreen.swift`, option B was "`pipe-pane` + a VT
emulator", and the reason it was rejected was:

> a reader joining mid-stream is wrong for 13–14 of 25 lines and, at two of five join points, *never converges*:
> Claude Code repaints only the lines that changed, so a line drawn before you arrived is blank for you forever.

**That reason does not hold for owned sessions**, and this is the key turn: for an owned session the daemon is reading from the **first
byte**; there is no "joining mid-stream" at all. So the very reason the old version rejected B is reversed here —
B is the only workable option, and the right one. The old version measured that B's emulator "reproduced three real streams at 100.00% cell-and-colour fidelity
with zero unimplemented sequences", in 349 lines of Swift, so its size is manageable.

Go candidates: `github.com/hinshun/vt10x`, `github.com/charmbracelet/x/vt`, or writing it ourselves after the old 349 lines.
**The recommendation is to write our own or wrap one**, for the same reason as the old version: this thing has to follow the way Claude Code repaints, and that is not
something a library will maintain for you.

#### Evaluated but not adopted: `ReadConsoleOutput`

Windows has `AttachConsole(pid)` + `ReadConsoleOutput`, which reads a console's character buffer directly.
Not adopted, for three reasons:

1. **A process can be attached to only one console at a time.** The daemon would have to attach and detach in turn, and attaching takes over
   its own stdio.
2. Its applicability to pseudo consoles hosted by ConPTY is limited.
3. It reads that console's buffer, while we are the pty host ourselves — a detour to read something we already have.

#### Latency

- macOS/Linux attached: `pipe-pane` → FIFO; the FIFO's readability is the signal (measured at 0.014 ms). Implemented.
- **owned (all three platforms)**: the pty/ConPTY output pipe itself is the signal — bytes coming out mean the screen moved.
  Simpler than the FIFO arrangement, because no second channel is needed.
- Windows attached: none; `signal_other.go` returns nil, and the screen is marked `on-demand`. **This is right**;
  do not change it into a stub that never wakes up.

#### A flag to remember to set

Under ConPTY, a Windows console's output code page is not UTF-8 by default (on a Traditional Chinese machine it is CP950).
It needs `SetConsoleOutputCP(CP_UTF8)` / `SetConsoleCP(CP_UTF8)`, or all Chinese text comes out garbled.
This one will be hit on the very first run on a real machine.

**Summary-table correction**: none.

<!-- /section:screen -->

<!-- section:clipboard owner:task-aaaaaaaa -->
### 4.3 Clipboard and images: on Linux/Windows the recommendation is "no borrowing"

`unsupported` (recommended to stay as it is)

Today an image is handed to the assistant in the terminal like this: **borrow the system clipboard → put the image in → send Ctrl-V → restore**
(`pasteboard_darwin.go`, matching the old `Targets.send(_ pieces:to:)`). When Claude Code receives Ctrl-V it reads
the clipboard, and the screen shows `[Image #1]`.

**On Linux this path is recommended against**, because of the clipboard ownership model of X11 and Wayland:

- An X11 selection **is not a piece of memory but a declaration of "who owns it now"**. The owner must be alive to hand over the data when
  someone else pastes. That is why `xclip` forks a resident process.
- Wayland is stricter: **setting the clipboard needs focus**. `wl-copy` gets around it by opening an invisible surface, and it too has to
  stay alive.
- This means "borrow, then restore" on Linux is not two calls but **two handovers of ownership**, and in between
  the daemon has to hold the user's clipboard. If the daemon dies in that window, whatever the user had copied is gone.
  That is the same kind of harm as taking over the keyboard.

**The recommended presentation**: on Linux and Windows, sending an image means **handing the file path to the assistant** (which is already
what `ErrPasteboardUnsupported` does today).

- **Cost**: on those machines Claude Code receives a line with a path, not `[Image #1]`. It can still read the image
  (it has a file-reading tool), but it looks different in the conversation, and takes one more tool call.
- **What to tell the user**: the hint line for attaching images says "this machine hands images to the assistant as paths",
  rather than leaving them to infer it from the result.

**Windows** is technically easier (`OpenClipboard`/`SetClipboardData` is one call, and the system keeps the data,
with no resident process needed), so if this is ever really done, **do Windows first**. But the same problem remains: whatever the user copies while the clipboard is borrowed
gets overwritten. The current `change count` protection (don't restore if someone else copied in the meantime) has a Windows counterpart,
`GetClipboardSequenceNumber`, so it can be carried over as is.

#### Image decoding

Only macOS has `sips`. On Linux/Windows, HEIC, TIFF, WebP and BMP are all `unsupported_image` today.

- The cheapest to fix are **WebP and BMP**: pure Go has `golang.org/x/image/webp` (decode only, which is enough)
  and `golang.org/x/image/bmp`. **Recommended: add them now, and all three platforms benefit** — today even macOS takes the detour through `sips`.
- HEIC has no pure-Go decoder; it stays `unsupported_image`. Presentation: the error message says "this machine cannot read HEIC;
  convert it to PNG or JPEG first", not just "unsupported image".

**Summary-table correction**: none.

<!-- /section:clipboard -->

<!-- section:whisper owner:task-aaaaaaaa -->
### 4.4 How whisper-cli is installed

`implemented` (search paths) / `implementable` (install guidance)

The search paths in `internal/adapters/whisper` are already written for all three platforms, and `ErrNoBinary` and `ErrNoModel`
are two separate errors — the header of `whisper.go` already explains why they are kept apart: *Told only "off", a person goes and checks
the thing they already did.* **What is missing is not code but guidance.**

| | Where the executable comes from | Search paths (already written) | Where the model comes from | Model paths (already written) |
|---|---|---|---|---|
| **macOS** | `brew install whisper-cpp` | `/opt/homebrew/bin`, `/usr/local/bin` | `download-ggml-model.sh`, or reuse one the old app downloaded | `~/.cache/whisper`, `~/Library/Application Support/Clawdline/models` |
| **Linux** | **No mainstream distribution package.** In practice you build it: `git clone whisper.cpp && cmake -B build && cmake --build build -j` | `/usr/local/bin`, `/usr/bin`, `/opt/whisper.cpp/build/bin`, `~/.local/bin`, `~/whisper.cpp/build/bin` | Same as above | `~/.cache/whisper`, `~/.local/share/whisper`, `/usr/share/whisper`, `/opt/whisper.cpp/models` |
| **Windows** | The official release has a prebuilt zip; unzip it into `%LOCALAPPDATA%\whisper.cpp\` | `%LOCALAPPDATA%\whisper.cpp[\bin]`, `%ProgramFiles%\whisper.cpp[\bin]` | Same as above | `%LOCALAPPDATA%\whisper`, `%LOCALAPPDATA%\whisper.cpp\models` |

Three things to do:

1. **The settings page prints the paths.** Next to "whisper-cli not found", list **the directories this machine actually searched**,
   and this platform's install command, with a copy button. The user's commonest mistake is having installed it somewhere else.
2. **Say plainly which one it does not take.** The `openai-whisper` installed by pip/uv is a different tool with different CLI arguments, **not compatible**.
   Linux users easily install that one and think it is broken. The error message has to name it.
3. **The model can be downloaded for the user; the executable cannot.** Recommended: add `clawdline voice install-model <name>`, which downloads
   `ggml-<name>.bin` into `~/.cache/whisper` (the first search location on all three platforms). The executable needs compiling,
   and we don't do that for them — it can fail in a hundred places, and the failure's error message would become our responsibility.

**There is one more precondition on Linux**: for the browser to record, the page must be a secure context. `http://127.0.0.1:7727`
counts (localhost is an exception), but **`http://192.168.x.x:7727`, reached over a LAN IP, does not**, and the browser will refuse
the microphone. This is not Linux-specific, but "opening a browser on another machine and connecting over" is a common way to use Linux, so
when `#mic` is disabled it has to give the real reason.

**Summary-table correction**: none.

<!-- /section:whisper -->

<!-- section:secrets owner:task-aaaaaaaa -->
### 4.5 Key storage: Keychain / Credential Manager / Secret Service / files

`implemented` (files) / `implementable` (keystore) / **Windows has a blocking gap**

Today: `internal/adapters/cloudkeys` (the device Ed25519 seed, the account master secret) and
`internal/adapters/devices` (local token, machine token, device hashes, audit log) **are all files**:
a 0700 directory + 0600 files + writes through a temporary file in the same directory, then a rename + every open with `O_NOFOLLOW`. The seam is
`cloud.KeyStore`. `remote.md` §4 already records this as a deliberate first-version choice.

#### First, the blocking gap

**On Windows, "0600" is an empty phrase.** On Windows `os.Chmod` changes only the read-only bit and sets no ACL. So
the rule written in the headers of the `cloudkeys` and `devices` packages — that this file belongs to this person and nobody else can read it
— **does not hold** on Windows today. Another user account on the same machine can read the local token, and the local token
can send and can manage devices.

It takes two things done together to close it, **and the recommendation is to do both, not to pick one**:

1. **NTFS ACL**: when creating the file, use `SetNamedSecurityInfo`/`CreateFile` with a DACL containing only the current user's SID,
   and turn off inheritance. Doable in pure Go (`golang.org/x/sys/windows`).
2. **DPAPI**: `CryptProtectData` (user scope) encrypts the content before it is written. Even if the file is copied away,
   another account cannot decrypt it. Doable in pure Go.

Item 2 is also the Windows keystore answer, so it is not extra work; it **merges that row of §3.3 and this row
into one job**.

#### The keystore on four platforms

| | Approach | Pure Go? | Limits |
|---|---|---|---|
| **macOS** | Keychain | ❌ needs cgo (Security.framework) | Alternatives: a `/usr/bin/security` subprocess (but the password appears in the command-line arguments), or **let the Swift shell hold it** — the shell can reach Security.framework anyway |
| **Windows** | DPAPI (user scope) + files; or Credential Manager | ✅ | Credential Manager's limit is 2560 bytes per entry, plenty for a 32-byte key |
| **Linux (with a desktop)** | Secret Service (D-Bus `org.freedesktop.secrets`, gnome-keyring/KWallet) | ✅ (`godbus/dbus`) | **Needs a desktop session with the keyring unlocked.** A session reached over SSH usually has neither |
| **Linux (headless/container)** | 0600 files | ✅ | This is today's state |

**For macOS, the recommendation**: don't turn on cgo for Keychain — that would break "one machine builds six platforms".
Either keep the files, or have the Swift shell read the secrets from Keychain at startup and hand them to the daemon. The latter is more right, because the shell
already does the same kind of thing (it injects the local token into WKWebView's cookie).

#### Three rules, written into the design

1. **Files are the floor; the keystore is an extra.** Detect it at startup: use it if it works, and files if it doesn't.
   **Never refuse to start because the keystore is missing** — headless Linux is a normal deployment shape, not a broken desktop.
2. **A degradation is said out loud, and in two places**: one line on the settings page, and one field in `/v1/health`
   (so the CLI and monitoring can see it too). "Where the secrets are kept" is something the user has a right to know.
3. **Unreadable ≠ absent.** This rule is already in the `cloudkeys` header, and it is especially dangerous when changing keystores:
   the user switched desktop environments, the keyring is not unlocked, DPAPI stopped working because an administrator reset the account password — in these cases
   **it must stop and report an error, and must not mint a new key**. Minting a new one would silently invalidate every device they have paired,
   and the first symptom they would notice is "nothing can be decrypted any more".

#### Migration

Once a platform starts using a keystore, there has to be a path for moving over from files, and **after the move the files are deleted and an audit entry is recorded**.
The reverse direction (keystore → files) is not done automatically: that is a degradation, and it needs the user's explicit consent.

**Summary-table correction**: `3.3 / Device and token storage / Windows`: this cell's difficulty should be **M**, and it is **blocking** —
the Windows version should not be released publicly before ACL + DPAPI are in place.

<!-- /section:secrets -->

<!-- section:notifications owner:task-aaaaaaaa -->
### 4.6 Notifications

`implementable`

First, a measured fact that is good news for cross-platform:

> **The old Swift app did not use the macOS local notification center.** `grep -rn "UNUserNotificationCenter|NSUserNotification|display notification" Sources/`
> gives **zero results** under the old app's `Sources/`. The only thing it actively sent off this machine was **Web Push**
> (`WebPush.swift`, RFC 8291 + VAPID, encrypted and handed to Apple's, Mozilla's or Google's push service).

In other words, **"notifications" as a feature is cross-platform by nature**, because the subscription lives in the browser and the daemon only needs to be able to make outbound
HTTPS requests. Linux servers, Windows and macOS are all the same.

So notifications should not be one switch on the settings page but **three outlets, each with its own state**:

| Outlet | Needs | macOS | Linux | Windows |
|---|---|---|---|---|
| **1. Web Push to paired devices** (the main path) | A browser subscription + outbound connectivity for the daemon | ✅ | ✅ | ✅ |
| **2. Local desktop notifications** (an extra, when there is a shell and a desktop) | A platform API | `UNUserNotificationCenter` (needs a signed bundle; the shell is already an `.app`) | `org.freedesktop.Notifications` (D-Bus, on nearly every desktop; none when headless) | Toast (`Windows.UI.Notifications`; **needs an AUMID, and shows only if there is a Start menu shortcut**) |
| **3. The console page itself** (always there) | Nothing | ✅ | ✅ | ✅ |

Outlet 3 is the floor: the badge in the tab title, the favicon, the counts in the header. It needs no permission and no platform API,
and while the user is looking at the page it is actually the best of the three.

#### Three limits the presentation must state

1. **A Service Worker needs a secure context.** For the browser to subscribe to Web Push, the page must be https or localhost.
   `http://127.0.0.1:7727` works; **`http://192.168.x.x:7727` does not**. So "someone connecting from another machine
   gets no push" — unless they come through a tunnel's https. This has to be said in the notifications block of the settings page, or users will think
   it is a bug.
2. **The Windows Toast AUMID.** A toast from a process without a registered AppUserModelID does not show, and
   **reports no error**. On Windows this is the item easiest to "think is done when it isn't", and it belongs on the real-machine checklist.
3. **Headless Linux has no outlet 2.** That is a normal state, not a fault. The settings page lists "Local desktop notifications: this machine
   has no desktop session".

#### Recommended order

Web Push (outlet 1) has to be built for Cloud anyway, and once it is done all three platforms have it. Outlet 2 is three separate small jobs,
best scheduled after the native shells, because they all need the shell to provide an identity (bundle/AUMID/`.desktop` name).

**Summary-table correction**: none.

<!-- /section:notifications -->

<!-- section:autostart owner:task-aaaaaaaa -->
### 4.7 Starting automatically at login

`implemented` (macOS) / `implementable` (Linux, Windows)

macOS already has it: `SMAppService.mainApp`, one switch, off by default (the native shell table in `replica.md`).

#### Windows

Four candidates; **the recommendation is the first**:

| Approach | Pure Go | Upside | Downside |
|---|---|---|---|
| **`HKCU\Software\Microsoft\Windows\CurrentVersion\Run`** | ✅ `x/sys/windows/registry` | One registry value; the user can see it, and turn it off, in Task Manager's "Startup" tab | Runs only after login; some antivirus software takes a second look |
| Startup folder (a `.lnk` in `shell:startup`) | ⚠️ needs COM to create the shortcut | Users understand it | Fiddly, with no benefit |
| Task Scheduler (`schtasks /create /sc onlogon`) | ✅ (subprocess) | Can delay the start, can restart automatically | Often locked down in enterprises; the XML is hard to maintain |
| Windows Service (SCM) | ✅ | Runs at boot, with no login needed | **Don't** — see below |

**Don't make it a Windows Service.** Services run in Session 0, isolated from the user's desktop session: a service cannot see the user's
terminals, cannot get the user's `%USERPROFILE%\.claude`, and cannot open windows. This daemon's job **is**
what happens in that user's session. Making it a service yields something that is technically running and, as a product, can do nothing.

#### Linux

Two ways; **the recommendation is to support both and detect automatically**:

| Situation | Approach |
|---|---|
| A desktop | XDG autostart: `~/.config/autostart/clawdline-next.desktop`. Every mainstream desktop supports it |
| No desktop / a server | `systemd --user`: `~/.config/systemd/user/clawdline-next.service` + `systemctl --user enable --now` |

**The `loginctl enable-linger $USER` step must be written out.** Without linger, systemd by default kills all of a user's processes when their last
session ends (`KillUserProcesses`) — that is, log out of SSH and the daemon is gone.
This is the first pit anyone hits on AWS, and its symptom ("but I enabled it") looks exactly like a configuration mistake.

#### One interface

Recommended: a `clawdline autostart enable|disable|status` subcommand that keeps the three platforms' differences inside it;
the settings page only calls it and only shows the state it reports. The reason: **the switch on the settings page has to be able to answer honestly "is it on or off right now"**,
and "how to ask" is completely different on the three platforms; with the asking in one place, the settings page does not need to know what the registry looks like.

Machines with no desktop and no systemd (minimal containers): `status` answers "this machine has no usable autostart mechanism",
and prints the manual commands.

**Summary-table correction**: none.

<!-- /section:autostart -->

<!-- section:hotkey owner:task-aaaaaaaa -->
### 4.8 Global hotkeys, and the limits of Wayland

`implemented` (macOS) / `implementable` (Windows, X11) / `degraded` (Wayland)

#### macOS (today)

Carbon's `RegisterEventHotKey`. The header of `HotKey.swift` says why it does not use an `NSEvent` global
monitor: that needs the accessibility permission, and **a tool that opens a text box should not be able to read every key you press**. This reason
will be used again for every platform below.

#### Windows

`RegisterHotKey` (user32), callable from pure Go. Simple, and needs no permission. Two facts to handle:

- **If another program registered the same combination first, `RegisterHotKey` fails.** Report it by name ("`Ctrl+Shift+K` is already
  taken by another program on this machine"); don't fail silently and leave the user pressing keys to no effect.
- It needs a message loop to receive `WM_HOTKEY`, so it lives in the shell, not in the daemon.

#### Linux — X11

`XGrabKey`. Pure Go has `github.com/BurntSushi/xgb` (a pure-Go client for the X11 protocol, with no Xlib needed).
Workable, difficulty M. "Already taken" (`BadAccess`) has to be handled here too.

#### Linux — Wayland: **not allowed, by design**

This is the core fact to tell users: **in Wayland's design, an ordinary client cannot intercept global keys.**
Only the compositor sees keyboard events, and a window without focus receives nothing. This is not a missing library;
it is a deliberate security boundary — and its reason is exactly the same as the reason `HotKey.swift` refused the `NSEvent` monitor.

Three paths:

1. **The XDG Desktop Portal's `org.freedesktop.portal.GlobalShortcuts`** (the only "proper" way).
   - Support is uneven: KDE Plasma implements it; GNOME long has not (**to be confirmed on a real machine with `busctl introspect`,
     see §8**); the wlroots family (Sway, Hyprland) relies on `xdg-desktop-portal-wlr`, with incomplete coverage.
   - Even where it exists, **which key gets bound is decided by the compositor, not the app**, and the first registration pops up a system dialog
     for the user to confirm. So the "record a hotkey" UI on the settings page does not work on this path.
2. **Ask the user to set up a shortcut in their own desktop.** GNOME "Settings → Keyboard → Custom Shortcuts", KDE "System Settings →
   Shortcuts", pointing at a command. **This works on every desktop and needs no permission and no portal**,
   and the user fully controls which key is bound.
3. Read `libinput`/`/dev/input` directly. **Refused** — that requires the `input` group, which amounts to a keylogger.

**The recommended degraded presentation** (a safe default, not waiting for the user's decision):

- Detect `$WAYLAND_DISPLAY` or `$XDG_SESSION_TYPE=wayland`.
- The hotkey field on the settings page is **replaced, not disabled**: it becomes an explanation, "on your desktop (Wayland) the compositor manages
  shortcuts", + a copyable command line + the settings paths of two or three mainstream desktops.
- For this a `clawdline panel` subcommand is needed: it asks the daemon to bring up the console (open a browser, or call up the shell's
  window). **This subcommand is useful on all three platforms**, not just Wayland — it is also the entry point for "Linux without a shell".
- Only if the `GlobalShortcuts` portal can be found on D-Bus is an extra "set it with the system dialog" button shown.

**Cost**: Wayland users have to do one more setup step, and it happens outside our UI. **This is an honest cost**,
better than a record box that does nothing when pressed, and far better than asking them to add the daemon to the `input` group.

#### Incidentally: "bringing a window to the front" doesn't work on Wayland either

The other side of the same boundary. X11 has `_NET_ACTIVE_WINDOW` (`wmctrl`/`xdotool`); **Wayland has no protocol for a client
to raise itself** (`xdg-activation` needs a token produced by user interaction, which the daemon does not have).
So "show it on screen" on Linux can only do `select-pane`/`select-window` inside tmux — which is exactly
what `reveal_other.go` does today, and its comment already gets this right:
**`ok` means "selected", not "it is in front of you".**

**Summary-table correction**: none.

<!-- /section:hotkey -->

<!-- section:notch owner:task-aaaaaaaa -->
### 4.9 What replaces the notch

`unsupported` (hardware) / `implementable` (abstracted into Presence)

> **Ownership**: the notch island itself is being implemented by a separate task ("The notch island (Mac native) and its cross-platform counterparts"),
> which claimed `shell/darwin/NotchIsland.swift` and `docs/replica.md`. This section writes only the skeleton at the decision level;
> that task's conclusions are authoritative on the implementation details, and where the two disagree, it wins and this section is corrected to match.

First, the facts straight: **only MacBooks with a notch have a notch.** External displays, iMac, Mac mini and Mac Studio have none.
So this is not "Macs have it and others don't" but "some Macs have it" — it needed a fallback from the start, and that fallback
happens to be exactly what the other platforms need. The old version itself calls it decoration (`NotchIsland.swift`: "This one is play").

#### What it is actually conveying

Taken apart, the notch island says only three things, and none of them is something only a notch can say:

1. how many things are running now;
2. which session is waiting for you;
3. a long job has finished (the dance).

So the right approach **is not to find something notch-like to stand in for the notch, but to abstract these three things into a port** —
provisionally named `Presence` — and give each platform its own implementation:

| Implementation | Platform | How it shows |
|---|---|---|
| **Generic web badge (the floor, always there)** | All | A tab title prefix (`(2) Clawdline`), a dynamic favicon, the counts in the header. **Needs no native API** |
| Notch island | MacBooks with a notch | Today |
| Menu bar mark | macOS | Exists (`NSStatusItem`) |
| Taskbar overlay icon + progress | Windows | `ITaskbarList3::SetOverlayIcon` (a corner badge) + `SetProgressState/Value` (**while something runs, the taskbar button becomes a progress bar, which is actually closer than the notch to "seeing at a glance that something is running"**) |
| System tray icon | Windows | `Shell_NotifyIcon`, changing the icon to show state |
| StatusNotifierItem | Linux (KDE and most desktops) | D-Bus, doable in pure Go |

#### GNOME's pit, first

**GNOME has no system tray by default.** It relies on the user installing the AppIndicator extension. So on Linux the tray cannot be treated
as something that is always there — which is exactly why the "generic web badge" has to be the floor: it holds on GNOME, on headless +
a remote browser, anywhere.

#### Recommendation

- **Build the floor first** (tab title + favicon badge). It is pure front end, all three platforms get it, and it also covers
  the case of "the user has the console open in a browser on another machine" — which the notch can never reach.
- Windows taskbar progress comes after the WebView2 shell; it is cheap and works well.
- Linux StatusNotifierItem comes last: it needs a shell, and the recommendation for Linux is no shell (§4.10).

**Cost**: outside the Mac there is no "fun thing". That is true, and it should be said plainly — the notch island is this app's personality,
not a feature, and personality cannot be ported to a screen with no notch. What can be ported are the three things it says.

**Summary-table correction**: none.

<!-- /section:notch -->

<!-- section:shell owner:task-aaaaaaaa -->
### 4.10 Native shells and the embedded browser

`implemented` (macOS) / `implementable` (Windows) / `degraded` (Linux: no shell)

#### Linux: recommended, no shell

WebKitGTK needs cgo, and cgo would break "one machine builds six platforms". `plan.md` §3 already says "WebKitGTK,
**or just use the browser**" — the recommendation is the latter, and `clawdline open` has already done it: it issues a read-only device,
carries the token in the fragment, exchanges it for a cookie at `/v1/auth/adopt`, and redirects to `/`.

**The cost has to be listed plainly: this is a whole row of features that do not exist**:

| Without a shell there is no | Substitute |
|---|---|
| Global hotkey | §4.8's compositor shortcut + `clawdline panel` |
| System tray | §4.9's web badge |
| Local desktop notifications | `notify-send` can be sent directly by the daemon (D-Bus, no shell needed) — **this one actually needs no shell** |
| UI for starting at login | The `clawdline autostart` CLI (§4.7) |
| Native local-console window | The user runs `clawdline open`; Cloud already opens in the chosen browser on every platform |
| Granting microphone permission on the user's behalf | The browser asks by itself, and asks more clearly |

**Conclusion: on Linux, "no shell" is not a defect but an acceptable shape**, as long as the table above is visible on the settings page.
If a shell is ever really needed, deal with it then, and the first thing to build then is a systemd user service + the browser, not GTK.

#### Windows: WebView2

`go-webview2` (pure Go, through COM) rather than `webview/webview_go` (needs cgo).

- Runtime: built into Windows 11; Windows 10 needs the Evergreen Runtime installed. **The installer has to detect it and install it**,
  or the first launch is a blank window.
- One WebView2 hosts only the local console and its local token. The Cloud button opens `https://app.clawdline.com`
  in the system browser, so the shell never needs a second cookie store.
- The native bar across the top (read-only address, back, Cloud and open-outside buttons) has to be drawn again on Windows.
  Colours and spacing likewise come from `legacy/tokens.css`.

#### macOS

The Swift shell owns one local-console WKWebView. Its `Cloud` button opens the hosted console in the system browser,
and its address is a selectable, read-only value rather than navigation UI. It remains the only shell with Keychain,
the notch, Carbon hotkeys and `SMAppService`; these are exactly "the few features only a Mac has".

**Summary-table correction**: none.

<!-- /section:shell -->

<!-- section:filesystem owner:task-aaaaaaaa -->
### 4.11 Storage, paths, file systems

`implemented` / `implementable`

#### SQLite

`modernc.org/sqlite` is pure Go and works on all three platforms. Two limits should become checks at startup:

- **The state directory cannot be on a network drive.** SQLite's WAL locking is unreliable over SMB/NFS. On Windows, a user's
  `%APPDATA%` may be redirected to a network drive in enterprise environments (folder redirection). **Recommended: check the drive type
  at startup, and if it is a network drive, refuse and say why** — far cheaper than chasing an occasional data corruption afterwards.
- Windows file-locking semantics differ from POSIX (`LockFileEx` locks are mandatory, POSIX locks are advisory).
  When one file is open in two daemons, the symptoms on Windows will differ from macOS.

#### Windows path traps (checks the `documents` route needs)

The header of `internal/adapters/documents` already draws the boundary clearly: "a name that arrives from a device may only choose *within* a
root this code computed". On Windows that rule needs a few more checks, because Win32 rewrites a name **after**
Go has seen it:

| Trap | Why it is dangerous |
|---|---|
| `\` and `/` are both separators | Blocking only `/..` does not block `\..` |
| ADS (`notes.md:hidden`) | The extension allowlist sees `.md`, while what gets opened is another data stream |
| 8.3 short names (`PROGRA~1`) | One directory has two names, and allowlist matching misses one |
| Trailing spaces and dots (`x.md.`, `x.md `) | Win32 strips them when opening, so the name compared and the file opened differ |
| Reserved device names (`CON`, `NUL`, `COM1`…) | What opens is not a file |
| Case insensitivity | Comparisons must be case-insensitive, or the allowlist can be bypassed |
| `\\?\` and UNC prefixes | They bypass normalization |
| Reparse points (symlink/junction/hardlink) | `nofollow_other.go` has only Lstat + the same-file check today |

**Recommended approach**: on Windows, don't rely on string checks alone. After opening, use `GetFinalPathNameByHandle` to get **the real path as the kernel
sees it**, then confirm it is under the root. That is the only check none of the traps above can get around, because what it asks about is
the handle that is already open.

#### Path length

Windows's default `MAX_PATH` is 260. Paths with a worktree + task id are long (`…\worktrees\clawdline-go-xxxxxxxx\<uuid>\…`
already comes close). Go's `os` adds `\\?\` to absolute paths automatically, but not in every case; **verify it on a real machine** (§7).

#### Line endings and encoding

- Transcripts, contracts and the console are all UTF-8, and unaffected.
- Under ConPTY, `SetConsoleOutputCP(CP_UTF8)` is needed (§4.2).
- git's `core.autocrlf` defaults to true on Windows; the diff parsing in `internal/adapters/git` needs checking so that
  `\r` doesn't misalign it.

**Summary-table correction**: none.

<!-- /section:filesystem -->

<!-- section:swiftstore owner:task-aaaaaaaa -->
### 4.12 Interoperating with the old Swift app: only macOS has it, and that is right

`implemented`

`internal/adapters/swiftstore` reads the old app's `~/.config/clawdline`, read-only (who Clawdfather is, the task list,
delivery records, session titles, images, the usage ledger). **On Linux and Windows the old app does not exist, so this whole area
is empty by nature**, and the code already handles that correctly:

- `ObservabilityDirIn()` returns `""` off darwin → the usage page falls back to computing from transcripts (implemented).
- `ProcessStart()` returns the zero time off darwin → it never matches any recorded identity (the comment in `procstart_other.go`
  already explains this).
- Any other read that finds no file → by the rules, "unknown", not "empty".

**One presentation problem to watch**: on a Mac, "the crown, task chips, coordination waits, the delivery check" are there; on Linux/Windows
they are not. A user who switches between two machines will think the Linux one is broken. **Recommendation**: these elements come from the old app's
store, and by `plan.md` §4 that store is **transitional** — once clawdline-go owns its own dispatch and delivery records,
these fields will be supplied by its own store, the same on all three platforms. Until then, on Linux/Windows don't leave an empty slot;
the element is simply absent (which is how it is today).

**Summary-table correction**: none.

<!-- /section:swiftstore -->

<!-- section:fidelity owner:task-aaaaaaaa -->
### 4.13 An acceptance question to settle first: cross-platform is not measured 1:1

`degraded` (the acceptance standard)

The acceptance method of `replica.md` is **the same browser window at the same zoom, comparing `getComputedStyle` and
`getBoundingClientRect`**. On Linux and Windows this method **will certainly be all red**, and not because the code is wrong:

- those machines have no SF Pro/`-apple-system`, so the font falls back to another typeface;
- once glyph widths change, every `getBoundingClientRect` changes;
- Linux font rendering (freetype hinting, subpixel) differs from macOS;
- Windows DPI scaling is a different system again.

**The user is asleep, so a safe default is chosen and written down here, to be overturned when they wake up**:

> The cross-platform acceptance standard is **the same structure and strings**, not the same pixels. That is: the same DOM, the same classes,
> the same string keys, the same `id`s, and **every place "this machine cannot do it" said by name on screen**.
> Pixel-level 1:1 is measured only on macOS, against the old version.

The reason: the target of the 1:1 replica is **the old Swift app**, and the old app exists only on macOS. On a machine without the old app
there is no oracle to compare against — what needs verifying there is something else.

**Summary-table correction**: none.

<!-- /section:fidelity -->

---

## 5. How "this machine can't do it" is presented: four rules

This is the passage of the document that most deserves to be treated as a standard. The existing code in fact already keeps it (`Unsupported` in `terminal/errors.go`,
`Complete:false` in `ps_windows.go`, `signal_other.go` returning nil rather than a stub,
`ErrPasteboardUnsupported` in `pasteboard_other.go`, `whisper` separating "not installed" from "no model");
here it is written as four rules for whoever comes next:

1. **Refuse by name, never silently.** Every platform difference returns a typed error, and the error says why **this machine**
   cannot do it. `terminal.Unsupported{Op:…}` is the existing template.
2. **"Don't know" and "none" are two things, and must never be spelled as the same answer.**
   `ps_windows.go` returning `Complete:false` instead of an empty list is the best demonstration of this rule: an empty list claims
   "nothing is running", and it has no standing to claim that.
3. **Disable, don't remove.** A button that is visible but disabled, and can say why, is better than a button that vanished.
   A user comparing with screenshots from a Mac understands "this machine has no iTerm2"; seeing the item gone only confuses them.
   (`replica.md` already does this for items with no backend.)
4. **A degradation is stated in two places**: on the screen the user sees (the settings page / next to the feature itself), and in
   fields of `/v1/health` (so the CLI, monitoring and programmers can get it too).
   Recommended: add a `capabilities` block to `/v1/health` listing every platform-dependent capability on this machine and its state;
   its content is the summary table of §3 answered for **this machine**. The settings page renders it directly instead of judging again for itself.

---

## 6. Difficulty and recommended order

Three tiers. Each tier's acceptance is the matching layer of §7.

### Tier 1: make Linux really usable (estimated 1–2 weeks)

Linux is the closest to "usable", because tmux, pty, FIFOs and procfs are all there.

| # | Item | Difficulty | Why it is here |
|---|---|---|---|
| 1 | The `capabilities` block of `/v1/health` + the settings page rendering it | S | Every later degradation relies on it to be presented; build it first, and each later item only fills in a cell |
| 2 | `clawdline autostart` + a systemd user unit + the linger explanation | M | Without it the daemon dies at logout, and everything else is wasted |
| 3 | A procfs process inventory (replacing `/bin/ps`) | S | Slim containers have no `ps`; `/proc` is also more accurate |
| 4 | Secret Service + a file fallback + presenting the degradation | M | Paves the way for Cloud; with no desktop, honestly use files |
| 5 | `clawdline panel` + the degraded UI for Wayland hotkeys | M | Wayland is the default on Linux desktops today |
| 6 | whisper install guidance and printed paths + `install-model` | S | Pure copy + one download, high return |
| 7 | The generic web badge (tab title + favicon) | S | All three platforms benefit, and it is the floor of Presence |
| 8 | Local desktop notifications (`org.freedesktop.Notifications`) | S | Needs no shell; sent directly over D-Bus |

### Tier 2: make Windows usable (estimated 3–5 weeks)

| # | Item | Difficulty | Notes |
|---|---|---|---|
| 1 | **Documentation and detection for WSL mode** | S | **Almost zero work, yet it gives Windows users full functionality directly. It should be done first.** |
| 2 | NTFS ACL + DPAPI | M | **blocking**: before this, the Windows version should not be released publicly (§4.5) |
| 3 | ConPTY + VT parser (owned sessions) | X | The biggest piece. Once done, Linux owned sessions come with it |
| 4 | Job Object (`supervisor_windows.go`) | M | Tied to 3: a tree that cannot be killed should not be started |
| 5 | Win32/WMI process inventory (visible only, no control) | M | So existing sessions at least appear in the list |
| 6 | Windows path checks for the `documents` route | M | A security item; review it together with 2 |
| 7 | WebView2 shell (with cookies separated across two environments) | L | |
| 8 | `RegisterHotKey`, `HKCU\Run`, Toast, taskbar badge | M | All need the shell first |

### Tier 3: extras

- Pure-Go WebP/BMP decoding (all three platforms at once, and macOS calls `sips` one time less).
- Linux StatusNotifierItem (needs a shell first, so possibly never).
- Windows clipboard borrowing (if it is ever really done, Windows first, never Linux; §4.3).
- macOS Keychain (if it is decided on, through the Swift shell holding it, without turning on cgo).

---

## 7. What to verify: the minimal checks on AWS Linux and Windows machines

**This section is for the real tests.** Each item is written as "run what → what you must see for it to pass". An item not run must never count as passing;
an item run with a different result is worth more written down than claimed as passing.

Recommended machines: one Linux machine **with a desktop** (GNOME on Wayland) + one **headless** (Amazon Linux/Ubuntu
Server, reached over SSH); one Windows machine, **Windows Server 2022 or Windows 11** (with a desktop, because the shell and
Toast both need one). **Two Linux machines are necessary, not insurance** — the differences between headless and desktop fall exactly on hotkeys,
keystore, notifications and autostart, and those four are where this document is least certain.

### L0 — Does it survive (both machines)

| Check | Passes when |
|---|---|
| `clawdline doctor` | Resolves the state directory correctly (Linux `~/.config/clawdline-next`, Windows `%APPDATA%\clawdline-next`) |
| `clawdline serve` | Binds 127.0.0.1:7727, and the log has no panic |
| `GET /v1/health` | 200, with values for the `scheduler`'s `considered`/`due`/`fired` |
| SQLite | The db can be created in the state directory; **kill the daemon and start it again, and the events are still there** |
| Reboot | The daemon comes back by itself (once §4.7 is done) |
| Long paths (Windows) | Run `serve` under a path close to 260 characters, and it doesn't blow up |
| Network drive (Windows, optional) | With `%APPDATA%` redirected, startup refuses and says why (once the check is done) |

### L1 — The authentication gate (both machines)

| Check | Passes when |
|---|---|
| Hit `/v1/sessions` without a token | `401 unauthorized`, with the old version's exact message |
| With `local-token` | 200 |
| `clawdline open` | With a desktop: it really opens a browser and gets to `/`. Headless: `--print` prints the URL |
| `fetch('/v1/auth/logout')` | The cookie is cleared and the device revoked |
| **File permissions** | Linux: `stat -c %a ~/.config/clawdline-next/local-token` = `600`, directory `700`. **Windows: `icacls` lists only the current user** (until §4.5 is done this item will be red; that red is expected, and should be recorded) |
| symlink | Replace `local-token` with a symlink; the daemon must refuse it, not follow it |

### L2 — Sessions (mainly Linux)

| Check | Passes when |
|---|---|
| Inventory after `tmux new-session -d` | `/v1/sessions` lists that pane, with tty/cwd/assistant correct |
| Inventory with no tmux server | An empty list with `Complete:true` (**this is "authoritatively empty", which is different from unreadable**) |
| Inventory with no tmux installed | The note says "tmux is not installed", `Complete:true` |
| Open a **disposable** claude session and send a sentence | The sentence appears on screen |
| Read the screen | `/screen` returns content, and Chinese text is not garbled |
| Send a key (`/key`) | Ask a harmless question with AskUserQuestion, press an option, and the one selected is the one pressed |
| Interrupt, close | The pane really stops / really disappears |
| focus | Returns `ok`, and **neither the docs nor the UI claim the window will jump to the front** (on Wayland it won't) |
| Killing the whole tree | Control: with `Setpgid`, 0 orphans |
| **Windows** | WSL mode: rerun every item above inside WSL. Native mode: open/send/read/interrupt/close for owned sessions (once ConPTY is done) |
| **Windows existing sessions** | Open a claude by hand in Windows Terminal; it must **appear** in the list, marked "cannot be controlled" (once §4.1(c) is done) |

### L3 — Every degradation is visible (both machines; this layer matters most)

For each item: **call the matching API and see that it returns a named error; open the page and see whether that spot says why.**

| Feature | The answer Linux should give | The answer Windows should give |
|---|---|---|
| Sending an image to the assistant | `ErrPasteboardUnsupported` → hands over a path instead, and the screen says so | Same |
| HEIC images | `unsupported_image`, and the message can say which format | Same |
| iTerm2-related | Does not appear in the backend list | Same |
| Live screen | tmux present → `live`; no tmux → `on-demand` | `on-demand`, without pretending |
| Process inventory | Complete | `Complete:false` + a note |
| supervisor | Normal | `supervisor_unsupported` (until the Job Object is in place) |
| The old app's store | The crown and the delivery check are simply absent, and no empty slot is left | Same |
| Usage page | Falls back to transcripts, and the page explains that the numbers come from a different source | Same |
| Global hotkey | Wayland: the settings page shows the compositor-shortcut explanation, not a box that cannot record | When `RegisterHotKey` is taken, it says so by name |
| Secret storage | The settings page says "keyring" or "files" | Says "DPAPI" or "files" |
| Notifications | Each of the three outlets lists its own state | Same, and the Toast can really be seen (the AUMID pit) |

### L4 — Optional components (both machines)

| Check | Passes when |
|---|---|
| whisper's three states | Not installed → `ErrNoBinary`; installed without a model → `ErrNoModel`; both → it really transcribes known content |
| The secure context for recording | Works from `127.0.0.1`; from a LAN IP it must give the real reason |
| git panel | Changes show on a **disposable** repository; on Windows, `\r` doesn't misalign the diff |
| cloudflared | Refused by name when not installed; starts when installed (optional) |

### L5 — Front end (both machines)

| Check | Passes when |
|---|---|
| Console loads | In the platform's default browser, `booting=false`, and the list draws |
| SSE | Left open for ten minutes without dropping; reconnects if it drops |
| Layout | **No pixel measurement** (§4.13). What is measured: the same set of `id`s are all present, no overflow, no horizontal scroll bar, usable at 760 wide and at desktop width |
| Fonts | After fallback, Chinese text is still readable, with no boxes |

### How to record

Record each item in three states, "run / not run / result"; **items not run stay on the list**. Half the value of this document is that
the next time someone asks "can Windows actually be used", the answer is a table with ticks, crosses and blanks, not an impression.

---

## 8. What I am not sure of, to be confirmed on real machines

This section is the honest boundary. Every item below affects the recommendations above, but **I have not verified any of them on a real machine**:

1. **Whether GNOME actually implements `org.freedesktop.portal.GlobalShortcuts`.** My understanding is that it long has not,
   but that changes. How to check: on the desktop Linux machine run
   `busctl --user introspect org.freedesktop.portal.Desktop /org/freedesktop/portal/desktop | grep -i GlobalShortcuts`.
   If it is there, §4.8's recommendation should add the portal path.
2. **Whether whisper.cpp has packages on scoop/winget.** §4.4's Windows row says "the prebuilt zip from the official
   release"; if a package-manager version exists, the guidance should change.
3. **How far Go's handling of paths beyond `MAX_PATH` reaches on Windows.** Go adds `\\?\` to absolute paths automatically,
   but not every path operation does. Run it for real under a path close to 260 characters (L0).
4. **`modernc.org/sqlite`'s WAL behaviour on Windows.** Pure Go should be fine, but it has not been run.
5. **What ConPTY's output stream really looks like under Claude Code's TUI.** The VT parser has to follow its repaint style,
   and only hooking it up and looking will tell the cost. The old version measured on tmux (349 lines, 100% fidelity); ConPTY may differ.
6. **Whether WSL2's localhost forwarding is stable under the user's network setup.** Some enterprise VPNs block it.
7. **Whether a Windows Toast shows at all without a Start menu shortcut.** My understanding is that it doesn't, and reports no error; to be tested.
8. **Whether Secret Service on a Linux desktop hangs waiting to be unlocked under SSH + `DISPLAY` forwarding.** If it does,
   there has to be a timeout, rather than letting startup hang.

---

## Appendix: the source locations this document is based on

| Subject | Files |
|---|---|
| Every platform-split file | `grep -rn '^//go:build' internal/ shell/` |
| Terminal backend list | `internal/adapters/terminal/hosts_darwin.go`, `hosts_other.go` |
| tmux | `internal/adapters/terminal/tmux.go`, `screen.go`, `keys.go`, `type.go`, `launch.go` |
| iTerm2 | `internal/adapters/terminal/iterm_*.go` |
| Live screen signal | `internal/adapters/terminal/signal_unix.go`, `signal_other.go` |
| Showing it on screen | `internal/adapters/terminal/reveal.go`, `reveal_darwin.go`, `reveal_other.go` |
| Process inventory | `internal/adapters/process/ps_unix.go`, `ps_windows.go` |
| Process trees | `internal/adapters/supervisor/supervisor_unix.go`, `supervisor_windows.go` |
| Clipboard and image decoding | `internal/adapters/artifacts/pasteboard_*.go`, `decode_*.go` |
| Voice | `internal/adapters/whisper/whisper.go` (`binaryDirs()`, `modelDirs()`) |
| Secrets | `internal/adapters/cloudkeys/`, `internal/adapters/devices/`, `nofollow_*.go` |
| Paths and the state directory | `internal/config/config.go` |
| The documents route's boundary | `internal/adapters/documents/documents.go` |
| The old app's store | `internal/adapters/swiftstore/` (`ObservabilityDirIn` in `usagedb.go`, `procstart_*.go`) |
| Opening a browser | `openURL` in `cmd/clawdline/auth.go` |
| macOS native shell | `shell/darwin/` (`HotKey.swift`, `SMAppService` in `main.swift`, `Browser.swift`) |
| The old Swift app (read-only reference) | `~/code/clawdline/Sources/`: `LiveScreen.swift`, `WebPush.swift`, `NotchIsland.swift`, `Whisper.swift`, `CloudKeys.swift`, `Targets.swift` |

---

# Merged in: two sections that landed first (the input bar, the notch island)

## The input bar (quick panel)

The macOS half is in `shell/darwin/Bar.swift`, the web half in `web/console/src/bar/`.

### 1. Global hotkey (⌥Space)

**What the limit is.** macOS uses Carbon's `RegisterEventHotKey` (`shell/darwin/HotKey.swift`), which needs no
accessibility permission, and gives each combination exactly one owner across the whole system.

- **Linux**: under X11 it is `XGrabKey`, and it can grab; **under Wayland it cannot**. Wayland deliberately does not let ordinary programs watch other
  windows' keyboards; the legitimate paths are the desktop environment's own settings (GNOME's `org.gnome.settings-daemon.plugins.
  media-keys.custom-keybindings`, KDE's `kglobalaccel`), or the `xdg-desktop-portal`
  GlobalShortcuts portal (relatively new, and not on every desktop).
- **Windows**: `RegisterHotKey` can grab, but UIPI keeps it out of elevated windows, and `VK_SPACE` with `MOD_ALT`
  fights the system menu.

**How it should be presented.** The shell knows at startup whether it can grab: if it can, carry on as before; if it can't, **don't quietly do nothing**,
but (a) write to the log which kind of failure it is, (b) say on the shortcut row of the settings page "on this desktop the system sets this; the command is
`clawdline-next://toggle`", and (c) always leave a way to open it that doesn't depend on the hotkey — on macOS the menu bar's
"open the input bar" item (`menuOpen`), on Linux/Windows that item of the tray menu plus that URL scheme. `main.swift`'s current
`applyConfiguredHotKey(alertOnFailure:)` is the macOS version of this shape (when registration fails it says the combination is "most likely taken by some other
app", rather than pretending it registered).

### 2. Window level and "not stealing focus"

**What the limit is.** macOS's `NSPanel` has `.nonactivatingPanel`: the window gets the keyboard, but **the application is not
activated**, so the app underneath still looks "in front". This is the premise the whole input bar stands on.

- **Linux/X11**: `_NET_WM_WINDOW_TYPE_DOCK` or `override-redirect` can keep it on top and unmanaged, but the focus
  model is different; you mostly have to `XSetInputFocus` yourself, and every WM behaves differently.
- **Linux/Wayland**: needs `wlr-layer-shell` (the wlroots family: sway and Hyprland have it). **GNOME has no**
  layer-shell, and an ordinary program cannot make "a panel floating above everything".
- **Windows**: `WS_EX_TOPMOST` with `WS_EX_NOACTIVATE` comes close, but a `NOACTIVATE` window that wants the keyboard has to go round through
  `AttachThreadInput`, and runs into the foreground-window lock (the limits of `SetForegroundWindow`).

**How it should be presented.** On platforms that cannot "not steal focus", make it **an ordinary window that does steal focus**, and give
focus back when it closes (`hide(returnFocus:)` in `Bar.swift` already does this). Don't imitate it with an always-on-top
borderless window that ignores focus — that turns into the keyboard wandering off halfway through typing. On GNOME/Wayland be honest: open an ordinary window,
and say on the settings page that this desktop has no floating panels.

### 3. Frosted glass

**What the limit is.** The blur behind the card is **a blur of the desktop**, and a web page cannot see the desktop, so that layer has to be in the shell
(macOS: `NSVisualEffectView`). Windows has Mica/Acrylic through `DwmSetWindowAttribute` (Win11);
Linux relies on the compositor's blur-behind (KDE has `KWindowEffects`, GNOME does not).

**How it should be presented.** Without frosted glass, **do nothing**: the scrim in `bar.css` is a 55% dark layer, and if what is behind the shell
is black, what gets drawn is a somewhat more solid card, and nothing breaks. This is the one item that "degrades just right".

### 4. Appearing and hiding with the terminal

**What the limit is.** macOS uses `NSWorkspace.didActivateApplicationNotification` to know "which app is in front now",
and gets its bundle id (`com.googlecode.iterm2`).

- **Linux/X11**: `_NET_ACTIVE_WINDOW` with `WM_CLASS` can do it. **Wayland cannot** — there is no API an ordinary program can use to read
  "who is in front now", deliberately.
- **Windows**: `SetWinEventHook(EVENT_SYSTEM_FOREGROUND)` plus the process's image name can do it.

**How it should be presented.** On platforms that cannot get the foreground app, **turn this feature off entirely**; don't guess from "the window lost focus"
(losing focus includes switching to the browser or to Slack, and reappearing every time would be very noisy). That settings row (`settingsReopen`,
"the input bar appears and hides with the terminal") must be disabled with the reason on such platforms, rather than switched on and doing nothing.

### 5. Selecting the chosen session in the terminal (Reveal/follow)

**This one isn't turned on even on macOS yet, and the reason is not the platform.** The old `Controller.follow(_:)` calls
`Targets.reveal(target, activate: false)`, and its comment is clear: "**Without activating.** Bringing
iTerm2 forward on every Tab press would hand it the keyboard, which is the one thing this whole
application exists to avoid doing."

The new daemon's `POST /v1/sessions/<id>/focus` (`internal/app/focus.go`) calls
`h.Reveal(ctx, s, true)` — **it always activates**, and the route has no parameter to ask it not to. Pressing ⇥ in the input bar brings the
terminal to the front, the keyboard immediately leaves the input box, and the input bar itself closes because it lost focus.

So: the web half (`follow` in `Bar.tsx`) is already wired up, but the `clawdline-bar-state.follow` the shell sends
**is always false**. Turning it on needs an `activate` field on `/focus` (default true, for compatibility),
with `app.Actions.Focus` passing it into `Reveal`; that is a one-line change, but it touches files another child just landed, so it is left to
root or the next round.

**The cross-platform part**: tmux's `select-pane`/`select-window` exists on all three platforms (on Windows, inside WSL);
iTerm2 is macOS only; Windows Terminal has no automation interface for selecting a tab. `internal/adapters/terminal` already
has the named error `terminal.Unsupported`; platforms with no matching implementation return it, and never quietly succeed.

### 6. Clipboard and editing keys

**What the limit is.** This window has no menu bar, and WKWebView acts only when someone sends it `cut:`/`copy:`/`paste:`
(`performKeyEquivalent` in `Bar.swift`, copying what `ConsoleWindow` does).

- **Linux (WebKitGTK) / Windows (WebView2)**: the webview's built-in keyboard handling already handles Ctrl+C/V/X/A/Z,
  so this part is not needed.

**How it should be presented.** This is an item macOS has to fill in and the other platforms don't. The modifier keys differ too: the web half
(`onKey` in `Bar.tsx`) is already written as `ev.metaKey || ev.ctrlKey`, so ⌘K and Ctrl+K are the same thing;
the `⌘K` on the hint row is the only place still hard-wired to macOS — later it should show `⌘`/`Ctrl` by platform,
and the words should be found in `Copy+*.swift`, not made up.

### 7. Where the send history is kept

**What the limit is.** The old version wrote 60 history entries into `~/.config/clawdline/config.json` (`Config.shared.history`).
The input bar is now a web page, and a web page writing the user's messages into the config file is not appropriate (the settings page displays the config file), so the history lives in this
origin's `localStorage` (`web/console/src/bar/history.ts`).

**The consequence, and it is real.** Switch to another webview, clear the site data, or use a private window, and the history is gone; the old version's history survived even a
reinstall. It is the same on all three platforms, so this is a difference of this version, not of platforms — **it is written here because it will be mistaken for a bug**.
The fix would be for the daemon to open a route of its own to store it, not for the web page to write the config file.

### 8. Not wired up yet (not a platform limit; this round didn't do it)

Things drawn on screen that don't work yet, listed here so they are not mistaken for cross-platform problems:

| Item | Today | To wire it up |
|---|---|---|
| Microphone (⌘L, `hintVoice`, "voice") | The button is drawn, `disabled`, with the old `hintVoice` as its `title` | `POST /v1/voice` already exists (wave 8, B); the recording part is in `legacy/voice-bridge.ts`, but it is tied to the composer's DOM and has to be split out |
| ⌘J, `hintOutput` ("show output") | On the hint row; pressing it does nothing | In the old version it is the whole output panel of `Controller.refreshOutput`; needs `/v1/transcript` and a new panel |
| ⌘M `hintMascot` ("change character"), ⌘S `hintStacks` ("servers") | Same | This app doesn't draw the mascot yet; the dev stack has no backend route |
| ⌘F `hintFullscreen` ("full screen"), ⌘R `hintOrder` ("reverse order"), ⌘+ `hintTextSize` ("text size") | Same | All three only make sense for the output panel, and will come with ⌘J |
| Dragging files onto the card | `bar.css` has a border rule for `[data-drop="on"]`, and nothing sets it | In the old version, `DropTargetView` + `Drop.swift`; cross-platform needs each platform's drag types |
| "N in the background" on a row | Not drawn | The wire has no such field (the old version read the Swift app's subagent files) |
| Coordination waits on a row | Not drawn | Same; it reads the old app's orchestrator store |

### 9. Two initial states deliberately different from the old version

`Controller.show()` opens with `listMode = .none`, `keysShown = false`: one line of input box and one hint row reading
⌘/ plus `hintKeys` ("shortcuts"), nothing else. This version **opens with the list open and the shortcuts laid out** — that is the screen the user provided,
and also what the old version looks like after pressing ⌘K and ⌘/.

The reason: the old "collapsed" state still had somewhere to go (⌘J opens the output panel over it); this version has no panel (see the previous section),
and with the list collapsed as well, only a one-line card would remain. Both keys still toggle; changing it back is the two constants at the top of `Bar.tsx`.

---

## The notch island (an always-visible waiting count, and a character)

### This feature's essence is not "the notch"

The notch is only a prop. The camera cutout on a Mac is pure black and always there, so a black window drawn flush against it
looks as if the cutout grew two ears — the character lives in the left ear, and the right ear is the status. **Only the Mac has this trick.**

Take the prop away, and what remains is the feature, in three parts:

1. **A waiting count visible without switching windows** — how many sessions are waiting for your answer, or how many are running.
2. **A character**, which sleeps, wakes up, moves faster when busy, and dances when a run finishes. This is mood, not data;
   everything it says, the ✳ in the menu bar already says (the old source says so itself: "the same reading, wearing a costume").
3. **Two targets to press**: the character = this app (opens the console), the number = that session (selects its terminal tab).

So when porting to other platforms, the question is not "where is Linux's notch" but **"on this machine, where is a place that is always visible,
fits a small picture and a number, and can be pressed"**. The three platforms answer differently, and none of the answers is a window.

### Linux

| Approach | What it can do | Limits |
|---|---|---|
| **System tray (StatusNotifierItem/AppIndicator, over D-Bus)** | A self-drawn icon (the character can be drawn into it), a tooltip, left- and right-click menus; changing the picture is swapping a pixmap, so low-frame-count animation is possible | GNOME has had no built-in tray since 3.26, and the AppIndicator extension must be installed to see it; KDE Plasma supports it natively. The icon is about 22–24 px, so the character can only be a silhouette |
| **A small always-on-top floating window (layer-shell/DOCK window)** | Closest to the old version: a real, permanently present patch of screen, where the character can move and the number can be text | **X11 lets you choose the position yourself; Wayland does not** — Wayland clients have no global coordinates, and need `wlr-layer-shell` to pin a surface to the screen edge. sway, Hyprland and KDE have it; **GNOME's Mutter does not implement layer-shell**, and there the only path is writing a gnome-shell extension |
| **Launcher badge (`com.canonical.Unity.LauncherEntry`, D-Bus)** | A number badge on the launcher icon, honoured by both KDE and Ubuntu Dock | A number only, no character; needs a `.desktop` file to know whose it is; GNOME natively doesn't honour it either |

**Recommendation: the system tray first, the floating window optional.** The reason is the "always visible" condition: the tray **may** be missing on any desktop environment,
but it does not need to decide its own position, so it cannot be flatly impossible on Wayland; the floating window is the reverse — where it can be built it is the most like the old version,
but on the most widespread setup, GNOME on Wayland, it has no solution, and making it the main path means handing in a blank sheet for the largest group of users.
The launcher badge is not the main path: it cannot draw the character, and does only one of the three essential things.

Implementation order: the tray icon (the character shrunk to a silhouette + the waiting count drawn in the bottom right) → the tooltip uses the same strings → left click opens the console,
and the right-click menu lists the waiting sessions. **On sway/Hyprland/KDE, additionally provide a layer-shell floating window**,
and only there draw the character moving. Cut the number of animation frames: the tray swaps pixmaps, and 60 fps would become a D-Bus flood;
swapping once when the state changes, and playing a short action of a few frames when a run finishes, is enough.

### Windows

| Approach | What it can do | Limits |
|---|---|---|
| **Notification area icon (`Shell_NotifyIcon`)** | Every Windows has it; the icon can be replaced with a new HICON at any time, so both the character and the number can be drawn in; a built-in tooltip | Windows 10/11 by default **tuck new tray icons into the overflow menu**, and the user has to drag the icon out for it to stay visible. 16×16 (scaled with DPI), again only a silhouette |
| **Taskbar badge / overlay icon (`ITaskbarList3::SetOverlayIcon`)** | Overlays a small picture on the app's taskbar button, and the number can be drawn in | **It needs a taskbar button to overlay** — when the window is closed it is gone, and the whole point of this feature is the time when the window is closed. With MSIX packaging there is also `BadgeUpdateManager`'s number badge, but that needs a package identity |
| **A small always-on-top borderless window (`WS_EX_TOPMOST` + `WS_EX_NOACTIVATE` + `WS_EX_TOOLWINDOW`)** | Windows **allows** an app to choose its own screen coordinates, so the old "small patch pinned to the top edge of the screen" can be built here, and the character can move at full speed | It covers other things on screen; multiple displays and DPI changes have to be handled by hand; it has to rely on `SHQueryUserNotificationState` to hide during full-screen presentations and games |

**Recommendation: the notification area icon first, the always-on-top window optional.** A tray icon tucked into the overflow menu is a settings problem,
solved by the user dragging it out once; the taskbar badge is a structural problem — it does not exist while the window is closed, which directly fails this feature's premise.
The always-on-top window is the only approach across the three platforms that carries the old version over unchanged, so it stays as an option — but not the default,
because "a permanent window that covers part of someone's screen by default" is something the user has to agree to, not something that should be there right after installing.

### This version builds only the Mac; where the interface attaches

One file, `shell/darwin/NotchIsland.swift`, and only four more lines in `main.swift` to mount it. The file is split into three layers;
**the middle one is cross-platform, the two ends are not**:

| Layer | Content | When changing platforms |
|---|---|---|
| Readings | The bridge script injected by `attach(to:open:)`, the `shellIsland` message, `IslandSession` | **Copy as is.** WebKitGTK's API is `window.webkit.messageHandlers.<name>.postMessage`, so the same JS needs no change; WebView2's is `window.chrome.webview.postMessage`, with a three-line shim |
| State machine | `IslandMode`, `refresh()` (waiting takes precedence over celebrating, celebrating over progress; a 3.4-second celebration; `pendingFinished` consumed only once), the width rules of `ears(for:bar:)` | **Copy as is**; it only touches numbers and strings |
| Screen and interaction | `Notch.rect/path/screen`, `IslandPanel` (overrides `constrainFrameRect` to be able to draw above the menu bar), `IslandView`, `IslandMascotView` | **Replace the whole layer.** A tray version only needs to "draw what `IslandView` draws into a pixmap"; the mascot's pack format and sampling (`IslandMascotPack`) can be copied as is |

Two actions are shared, and on both sides they are already routes of the local daemon, not platform APIs:

- The character is pressed → open the console window (the shell's own business).
- The number is pressed → `POST /v1/sessions/{id}/focus`, the same route as the console's "show on Mac",
  so the two can never select different things. The Linux/Windows shells send the same request, and whether the terminal is really brought up
  is decided at the daemon's end.

**One thing not yet decided, left for when a second platform arrives**: whether the state machine moves into Go, so that the daemon emits an
`island` event directly (who is waiting, how many are running, who just finished), and all three shells only draw and take presses. It doesn't move now, because there is only one shell,
and moving it would only rewrite one piece of code in another language; once a second shell appears and the 3.4 seconds start being written once on each side, it should move.
