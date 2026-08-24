# Contributing

Plain AppKit. No dependencies, no Xcode project, no build system beyond `swiftc`.

```bash
./test.sh     # 1479 checks, a couple of seconds
./build.sh    # builds the .app and relaunches it if it was running
```

That is the whole loop. `./build.sh` writes to `~/Applications/Clawdline.app` and puts the app
back the way it found it, so you can rebuild while using it.

## Getting an editor to understand the code

```bash
swift build
```

`Package.swift` exists **only** so SourceKit-LSP has something to index — VS Code, Zed, Neovim
and the rest then give you completion, jump-to-definition and inline errors. What it produces is
a bare executable with no `Info.plist` and no `Resources`, which cannot register a hotkey or find
a mascot pack. Never ship it; use `./build.sh`.

## Where things are

| | |
|---|---|
| `Sources/Controller.swift` | the panel: layout, keys, and everything wired to everything |
| `Sources/Panel.swift` | the views — card, mascot, key hints, the prompt text view |
| `Sources/Targets.swift`, `ITerm.swift`, `Tmux.swift` | finding sessions and sending text to them |
| `Sources/Transcript.swift`, `Markdown.swift`, `Ansi.swift` | reading a session back and drawing it |
| `Sources/Voice.swift`, `Whisper.swift` | dictation, and the optional second pass |
| `Sources/Project*.swift` | which repository a session is in, and its status |
| `Sources/Copy+*.swift` | one file per language |
| `Resources/mascots/*.json` | mascot packs — see [docs/mascots.md](docs/mascots.md) |
| `docs/backlog.yaml` | what is not done and why — read the header before adding to it |

## The tests

They cover the parts a change can quietly break: pack decoding and validation, keyframe sampling,
colour parsing, hotkey specs, silence detection, and the two parsers that decide where text ends
up (`ps` output and `tmux list-panes`). Anything that needs a window on screen is deliberately
absent — a test that cannot run in CI is a test nobody runs.

Two habits worth keeping:

- **Break it on purpose.** A test that has never failed has not been shown to work. Several of
  the ones here were written, passed, and then only earned their place when reverting the fix
  turned them red.
- **Test the thing, not the theme.** A check that asserts text is fully opaque is testing macOS's
  choice of `labelColor` (85% black in the light appearance), not your code.

## Comments

Comments here explain *why*, especially where the obvious approach was tried first and did not
work. Those notes are the useful part of this codebase — please keep the habit, and write them
for whoever finds the same dead end in a year.

Everything a reader outside this repository sees — the interface, the docs, commit messages — is
in English.

## Adding a language

Copy [`Sources/Copy+English.swift`](Sources/Copy+English.swift) to `Copy+<Language>.swift`,
translate the values, and add one line to `L.catalog` in
[`Sources/Strings.swift`](Sources/Strings.swift). Two things then hold it up:

- the **compiler** refuses a language that is missing a string, and
- the **tests** refuse one that is still in English, because a copied file compiles perfectly.

Put more specific tags above broader ones in the catalog — the match is by prefix, so a bare
`zh` above `zh-Hant` sends every reader of Traditional Chinese to Simplified without a word.
A test pins that too.

Corrections to any existing translation are welcome, and the ones nobody here speaks natively
are the ones most likely to need them.

## Adding a mascot

A pack is a JSON file: a grid, a palette, poses and keyframed routines. Nothing is compiled in.
[docs/mascots.md](docs/mascots.md) is the format, `tools/validate-pack.py` checks one the same
way the app does at load time, and CI runs it on every pull request that touches
`Resources/mascots`. [docs/gallery.md](docs/gallery.md) is where packs get listed.

## Sending text somewhere other than iTerm2

Two paths exist: iTerm2 scripting, and tmux. A third would be a file next to `ITerm.swift` and
`Tmux.swift` producing the same shape into `Targets.swift`, with its parsing split out from the
part that shells out — the parsing is where bugs live and running the binary is not, which is
why those two are separate everywhere here.

Terminal.app, Warp and Tabby cannot receive text at all, and that is measured rather than
assumed: Terminal's `do script` reports success and a program blocked on `read()` never sees a
byte. Run Claude Code inside tmux and any emulator works.

## Pull requests

Small ones are easier to take. Say what you measured, if you measured something — a number in a
commit message is worth more here than an adjective. If a change fixes something that looked
fine from outside, say what it looked like, because that is the part nobody can reconstruct
later.
