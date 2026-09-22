# Running clawdline-go on Windows

Measured on 2026-09-20 on a stock Windows Server 2022 Datacenter image (10.0.20348, amd64,
2 vCPU, 4 GB, no tmux, no node, no git, no assistant installed), reached over SSM with no
inbound port opened. Everything below was run; nothing here is inferred.
`docs/cross-platform.md` decides what Windows *should* do — this file says what it *did*.
`docs/linux.md` is this file's shape, and none of its conclusions were carried over.

**Every command ran as `NT AUTHORITY\SYSTEM`, because that is who SSM is**, so `%APPDATA%`
resolved to `C:\Windows\system32\config\systemprofile\AppData\Roaming` throughout. That
changes one measurement and only one — §4.6, where the state directory's ACL comes from
whatever it is created inside — and that one was re-measured on a directory under `C:\`
to get the answer a normal install gives.

**In one sentence: the daemon, the state directory, the auth gate, the SQLite store, SSE,
the capability vocabulary and the named refusals all work on Windows, and two habits stopped
a stock build dead — the console could not be loaded at all, and no brief could even be
described — both of them a Unix path spelling standing where this platform's own belongs.**
Four changes, built and measured on the same box (`artifacts/windows-fixes.patch`), turn
both into working, and then the dispatch loop stops exactly once, in the right place, saying
exactly why.

## 1. Get it running

The daemon is one static binary and needs no Windows toolchain to make. Cross-compile it
anywhere:

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o clawdline.exe ./cmd/clawdline   # 27 MB, 2.7 s
(cd web && npm install && npm run build)                                            # web/console/dist
```

Ship `clawdline.exe` and `web/console/dist` to the machine by any means that needs no new
infrastructure. Nothing on a stock Windows Server image helps: there is no `aws` CLI, no AWS
Tools for PowerShell, no `git`, no `node`, no tmux — `curl.exe` and `Expand-Archive` are the
whole toolbox. What worked here was an S3 `GetObject` signed in PowerShell against the
instance role's own IMDS credentials, then `Expand-Archive`; `AWS-RunRemoteScript`'s
`aws:downloadContent` needs `s3:ListBucket` even for one object, which a tight role does not
have. The binary's SHA-256 on the box matched the one built on the Mac.

On the box:

```powershell
$env:CLAWDLINE_NEXT_DIR = 'D:\clawdline\state'   # optional; default below
$env:CLAWDLINE_NEXT_PORT = '7727'                # optional
$env:CLAWDLINE_NEXT_WEB = 'C:\clawdline\dist'    # required, or the console 501s by name
.\clawdline.exe doctor      # version, port, state dir, store counts
.\clawdline.exe serve
```

`doctor` printed, unchanged:

```
version   0.0.1-p0
port      7727
upstream  none (an unowned route answers 501 not_implemented; CLAWDLINE_NEXT_UPSTREAM_PORT asks for one)
dir       C:\Windows\system32\config\systemprofile\AppData\Roaming\clawdline-next
store     0 events, 0 broker tasks
```

**State goes to `%APPDATA%\clawdline-next`**, which is what `docs/cross-platform.md` §3.4 says
it should, and `CLAWDLINE_NEXT_DIR` overrides it — both verified, including onto a UNC path,
which is §4.9's problem. `doctor` alone creates the SQLite file; `serve` adds `local-token`,
`orchestrator-token`, `remote-audit.jsonl`, `remote.json`, `push\vapid-p256-v1`, a projected
`dispatch-policy.md`, `logs\daemon.log` and a copy of the binary at `bin\clawdline.exe`.

Nothing installs a login item on Windows — `launch_at_login` says so by name and names the
manual answer (a shortcut to `clawdline serve` in `shell:startup`). A daemon started detached
survives the session that started it: it kept answering across twenty-odd separate SSM
invocations over forty minutes.

**Do not run the daemon from `%APPDATA%\clawdline-next\bin\clawdline.exe`**, which is the path
the skill stub names. Windows will not let a running image be replaced, so the next build's
startup self-copy cannot land: overwriting that file while the daemon was it was refused with
*"The process cannot access the file … because it is being used by another process."* Run it
from its own directory and let it keep the stable copy fresh.

## 2. What works, measured

| Item | What was run | Result |
|---|---|---|
| `doctor` | three times, two state directories | version, port, dir and store counts correct, no panic |
| `serve` | four times, across two binaries | listens on `127.0.0.1:7727`; survives the SSM session that started it |
| Second `serve` on a taken port | deliberately | `clawdline: listen tcp 127.0.0.1:7727: bind: Only one usage of each socket address … is normally permitted.`, exit 1 |
| `GET /v1/health` | no token / token | `200`; `authed` flips correctly |
| Auth gate | `/v1/sessions` with no token | `401 unauthorized`, `"This needs a paired device."` |
| SQLite store | write, kill the daemon, restart, read back | a snippet written before the restart came back after it, CJK bytes intact |
| Disk free | `/v1/diagnostics` | `disk_free_bytes: 32291221504` — `diskfree_windows.go` works |
| Session list | `GET /v1/sessions` | `sessions: []`, `complete: false`, `emptyAuthoritative: false` — **unseen, not absent**; see §3 |
| SSE | `GET /v1/events` | `text/event-stream`, `event: sessions` and `event: orchestrator` pushed at once |
| Console document | `GET /` **with §4.1 fixed** | `200`, 53 187 bytes, `text/html; charset=utf-8`, the strings slot filled |
| Console assets | every path the document names, 20 of them | all `200`, sizes match the bundle |
| Path traversal | `/../../../Windows/win.ini`, `/..%5c..%5cWindows%5cwin.ini`, `/C:/Windows/win.ini` | `400` from the gate, `400`, `404` — nothing left the bundle |
| Dispatch, as far as it goes | `POST /v1/orchestrator/tasks` **with §4.2 fixed** | `409 no_child_capability`, three capabilities named, *"Nothing was recorded, made or opened."* |
| `task finish` | LF, CRLF, and CRLF+BOM | LF and CRLF pass; the BOM is §4.4 |
| Web Push key | `GET /v1/push/key` | a VAPID key pair minted on Windows |
| `clawdline assistants` | read | both `unknown` with `unknown_reason: "no_record"`, and the paths in the sentence spelled `~\.claude\statusline-cache\rate-limits.json` |
| `clawdline skill install` / `uninstall` | both | stub written to `%USERPROFILE%\.claude\skills\clawdline\SKILL.md`, recorded, and put back exactly |
| `clawdline guide zh-TW` | raw stdout bytes | 29 068 bytes, 4 094 CJK code points, **0 replacement characters** — the bytes are right; §4.5 is the screen |
| `clawdline landings`, `tunnel --json`, `cloud status`, `board tracks` | read | all answered, Windows paths spelled with backslashes throughout |
| `/v1/settings`, `/v1/places`, `/v1/projects`, `/v1/board` | read | `200`, empty and honest about it |

## 3. Refused by name, not crashed

`/v1/diagnostics` carries the `capabilities` block (`docs/cross-platform.md` §5 rule 4, first
half), and on this machine it said, in its own words:

- **`open_child`** — unavailable, *"windows has no iTerm2, and tmux is not installed."*
- **`read_screen`**, **`send_keys`** — unavailable, *"tmux is not installed; windows has no
  iTerm2, and a console this daemon owns (ConPTY) is not built yet"*
- **`clipboard`** — unavailable, *"there is no clipboard on windows that Clawdline can lend a
  picture to; a send hands the assistant the picture's path instead"*
- **`global_hotkey`** — unavailable, *"RegisterHotKey needs a native shell with a message loop,
  and there is no Windows shell yet; bind a shortcut in your desktop's own settings to
  `clawdline open` instead"*
- **`notch`** — unavailable, *"there is no notch on windows; what the island shows — how many
  sessions are working and which one waits for you — is on the console's session list"*
- **`launch_at_login`** — unavailable, *"nothing on windows registers a login item yet; a
  shortcut to `clawdline serve` in shell:startup does it by hand"*

Item by item, for the macOS-only features:

| Feature | What Windows answered | Where |
|---|---|---|
| Global hotkey | named unavailable, with the substitute named too | `/v1/diagnostics` capabilities |
| Built-in browser (the Swift shell) | there is no shell in this daemon on any platform; `clawdline open --print` prints a sign-in URL instead, and it worked | `cmd/clawdline/auth.go` |
| Mascot | not a capability and not a route — a settings field the shell drew. `/v1/settings` returns `"mascot": null` and nothing reads it | `/v1/settings` |
| iTerm2 | absent from the backend list, and the capability block says so. Read, not run: `ITermRunning` is a constant false off macOS and `NewITermTab` returns *"this terminal backend cannot open an iTerm2 tab on this platform yet"* | `terminal/launch_other.go` |
| Apple Events | reached only through iTerm2, whose every file is `//go:build darwin`, so nothing on Windows compiles a call to one. Read, not run | `terminal/iterm_darwin.go` |
| Notifications | Web Push only, and it works: a VAPID key pair was minted here. With nothing subscribed, `clawdline notify` answered `409 not_subscribed`, *"No device has asked for notifications yet."* | `/v1/push/key` |
| Keychain | by design there is none: `cloudkeys` and `devices` are files on every platform, *"Files, not a keychain, on purpose."* On Windows that choice has a cost — §4.6 | `adapters/cloudkeys/files.go` |
| cloudflared | `/v1/tunnel` → `installed: false`, `mode: "off"`, `state: "off"`, and it names the config path it would use | `/v1/tunnel` |
| Voice | `503 no_whisper`, `reason: "no_binary"`, *"This machine has no whisper-cli, so there is nothing here to read a recording with."* The 16 kHz rule is stated in full rather than quietly resampling | `POST /v1/voice` |

Two more that answered well:

- **The session list does not lie.** With no process scan and no tmux, `/v1/sessions` returns
  `sessions: []` with `complete: false` and `emptyAuthoritative: false`. `ps_windows.go`
  returns `Complete: false` on purpose — *"An empty inventory would claim that nothing is
  running, which is a statement this adapter is in no position to make"* — and the snapshot
  carries that through. Linux's mislabelling (a session outside tmux stamped `backend:
  "iterm"`) **cannot happen here**: `ps_unix.go` is `//go:build darwin || linux`, and the file
  Windows compiles instead scans nothing at all. Nothing is ever called an iTerm session on
  this machine. What the list cannot do is say *why* it is empty — §4.8.
- **A session id that exists and is not seen.** `GET /v1/sessions/%250/screen` (that is `%0`
  percent-encoded) answered `409 session_unknown`, *"this reading of the machine was
  incomplete (merged), so %0 is not absent, it is unseen"* — the distinction D05 ③ asks for,
  said out loud on a platform that can see nothing.

Routes this daemon does not own answer `501 not_implemented` and name themselves:
`/v1/devices`, `/v1/assistants`, `/v1/schedules`, `/v1/voice/capabilities`, `/v1/capabilities`.
That is not a Windows fact — it is the same answer everywhere with nothing behind the port —
but on Windows there is no Swift app that could ever be behind it, so it is permanent here.

## 4. What stops a stock build, with the evidence

### 4.1 The console cannot be loaded at all: `filepath.Clean("/")` is not `/` on Windows

`internal/transport/http/page.go` cleans the request path with `filepath.Clean` and compares
the result with `"/"`:

```go
clean := filepath.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
if clean == "/" { p.document(w); return }
full := filepath.Join(p.root, clean)
```

On Windows `filepath.Clean("/")` is a lone backslash, so the first branch never runs, and
`filepath.Join(root, "\\")` is the root itself with no trailing separator, so the
`HasPrefix(full, root+separator)` check below refuses it:

```
GET /            -> 400 {"detail":"that path leaves the console","error":"bad_path"}
GET /index.html  -> 301 (ServeFile redirects index.html to "/" — which 400s)
```

Every asset loads (`/assets/*`, `/favicon.ico`, `/manifest.webmanifest`, `/settings.html`,
`/bar.html` all `200`), so the bundle is served fine and **the one file nobody can get is the
document**, and the `/index.html` redirect closes the loop. The console is unreachable in a
browser on Windows.

A URL path is a slash path whatever the machine spells its own paths with. With `path.Clean`
for the URL and `filepath.FromSlash` at the moment it becomes a file name, `GET /` answered
`200` with 53 187 bytes and the strings slot filled, all twenty assets the document names
answered `200`, and the three traversal attempts were still refused (`400`, `400`, `404`).

### 4.2 No Windows brief can be described: `usableDir` tests a spelling, not a property

`internal/app/orchestrator/draft.go`:

```go
func usableDir(path string) bool {
	if path == "" || !strings.HasPrefix(path, "/") || strings.ContainsRune(path, 0) {
		return false
	}
```

So a dispatch naming a real, existing, absolute Windows directory is refused, and refused
with a sentence that is not true of it:

```
POST /v1/orchestrator/tasks   project_dir=C:\clawdline\proj
422 {"error":{"code":"bad_task","message":"project_dir must be an absolute path to a directory"}}
```

`C:\clawdline\proj` **is** an absolute path to a directory. `filepath.IsAbs` says so; a leading
slash does not. With that one word changed, the same dispatch was admitted and went on to the
refusal it should always have met (§4.3).

**This is a class, not a spelling.** Counted in this tree, outside tests, the same
`strings.HasPrefix(x, "/")` test for "is this absolute" stands at **18 sites** — the one above
plus 17 more, in `adapters/projects/identity.go` (×4), `adapters/projects/lifecycle.go`,
`adapters/projects/places.go`, `adapters/swiftstore/project.go`,
`adapters/transcript/turns.go` (×2), `adapters/documents/documents.go`,
`app/cloudops/ops.go`, `app/orchestrator/draft.go:341`, `app/orchestrator/inventory.go` (×2),
`domain/schedule/schedule.go` (×2) and `transport/http/timeline.go`. Some of those refuse a
leading slash rather than require one — a relative-path check — and those are the same mistake
wearing the other coat: on Windows they let `C:\Windows\win.ini` and `\\server\share` through
as "relative".

Beside them stand **8 containment tests** of the form `a == b || strings.HasPrefix(a, b+"/")`
— `adapters/projects/lifecycle.go:1064`, `adapters/projects/places.go:202`,
`app/orchestrator/inventory.go` (×3), `app/orchestrator/reclaim.go:450`, `domain/icon/icon.go`
(×2) — and those are worse than the plain ones, because on Windows they answer *false* for a
directory that really is inside the repository, which is a wrong answer rather than a refused
one. Only the site the dispatch meets first was measured; the other 25 are read, not run.
**A path is absolute when the platform says it is, and one path contains another when their
elements match, not when their bytes do.** Every site that decides either question wants
reading, not just this one.

### 4.3 The dispatch loop's real stopping point, and it is the right one

With §4.2 out of the way, a dispatch of a plain `custom` task with a live-looking root reached
exactly one gate and stopped there:

```
409 no_child_capability
"This machine cannot open a child session — open_child is unavailable: windows has no iTerm2,
 and tmux is not installed; read_screen is unavailable: …; send_keys is unavailable: ….
 Nothing was recorded, made or opened."
missing: ["open_child","read_screen","send_keys"]   platform: "windows"
```

That is the answer `broker-design.md` #43 asks for: refused before anything exists, with the
three capabilities named and each one's reason carried in the body. **The Linux round got a
child briefed, self-reporting and `state: success`; Windows cannot, and the square it stops on
is `checkChildCapability`, one step before anything is written.** Nothing else in the chain
was reached, so nothing else in the chain is known to work here.

Making it go further is not a bug fix, it is the ConPTY work in `docs/cross-platform.md`
§4.1(b). Until then Windows can dispatch nothing, and it says so correctly.

### 4.4 A child's result written by a Windows tool is refused, and the refusal does not say why

`Set-Content -Encoding UTF8` — the default way PowerShell 5.1 writes a text file — puts a
UTF-8 byte-order mark in front of it. `clawdline task finish` then answers:

```
first bytes: ef bb bf 7b
task result preflight: invalid — result.json.tmp is not readable JSON
Nothing was written. Correct result.json.tmp and run this again.
```

The JSON is perfect. Three bytes in front of it are not, and nothing in the refusal says so,
which leaves a child rewriting a file that was already correct. CRLF alone is fine — the same
content with CRLF and no BOM passed — so **the line endings are not the problem and the BOM
is**, which is worth knowing because CRLF is what everyone suspects first.

The fix belongs at `readBounded` in `internal/adapters/taskdir/broker.go`, the one place this
package reads `task.json`, `result.json.tmp`, `result.json`, `progress.json` and
`accepted.json`, so what is parsed and what is copied onward are the same bytes. Measured with
it: a `task.json` **and** a `result.json.tmp` both written with a BOM gave `task result
preflight: valid`, and the `result.json` put in place began `7b 22 63 6c` — the mark did not
travel.

(Trying it one layer up, inside the JSON decoder helper, is not enough: a second parse of the
same bytes then fails instead, with `invalid character '\ufeff' looking for beginning of
value`. Measured, on the way to the right place.)

### 4.5 Everything this program prints in Chinese is mojibake on a Windows console

`clawdline guide zh-TW` writes 29 068 bytes containing 4 094 CJK code points and zero
replacement characters — the bytes are correct UTF-8 (`e4 bd bf` = 使). A Windows console
decodes stdout with its own code page, which was **437** on this US image and is **950** on a
Traditional Chinese one, and neither reads UTF-8:

```
# Clawdline Σ╜┐τö¿µîçσìù

ΘÇÖµÿ» `clawdline guide` τÜäτ╣üΘ½öΣ╕¡µûçτëêπÇé
```

Nothing calls `SetConsoleOutputCP(CP_UTF8)`. One `init()` in a `//go:build windows` file fixes
every command at once; measured, the code page read 65001 afterwards and the bytes were
unchanged. **Its one cost is worth stating: the code page belongs to the console, not to the
process, so it stays 65001 after `clawdline` exits** — which is what `chcp 65001` does by hand
and what most CLIs that do this accept, but it is a side effect on somebody else's window and
the owner should decide it knowingly.

### 4.6 On Windows `0600` is an empty phrase, and here is what it cost

`docs/cross-platform.md` §4.5 predicted this; it is now measured. The state directory is
created with `0700` and the token files with `0600`, and on Windows `os.Chmod` sets no ACL at
all — it moves the read-only attribute and nothing else. Every ACE on `local-token` was
inherited (`(I)`), so the tokens are exactly as private as whatever directory they were
created inside:

```
# state dir left at its default, inside a profile — safe, by inheritance, not by us
local-token   NT AUTHORITY\SYSTEM:(I)(F)   BUILTIN\Administrators:(I)(F)

# the same daemon with CLAWDLINE_NEXT_DIR=C:\clawdline-state-probe
C:\clawdline-state-probe   …  BUILTIN\Users:(I)(OI)(CI)(RX)
local-token                NT AUTHORITY\SYSTEM:(I)(F)
                           BUILTIN\Administrators:(I)(F)
                           BUILTIN\Users:(I)(RX)
```

**`BUILTIN\Users:(RX)` on the local token.** Any local account on that machine can read it and
drive the daemon. `CLAWDLINE_NEXT_DIR` is documented, ordinary and the obvious thing to reach
for when `%APPDATA%` is redirected — and it is all it takes. The same applies to
`orchestrator-token`, the SQLite store and the cloud key material, which are the same files in
the same directory. This is the blocking gap §4.5 named, and it blocks: **the daemon should
set its own DACL and turn off inheritance rather than inherit whatever it landed in.**

### 4.7 `clawdline open` says it opened a browser on a machine that has none

```
PS> clawdline.exe open
opened a browser as device f85a22ca-…
exit=0
```

There is no browser, no interactive desktop and no window. `openURL` runs
`rundll32 url.dll,FileProtocolHandler <url>`, and that handler returns 0 whatever happens, so
`cmd.Run() == nil` is not evidence that anything opened. Every call also mints a device —
`devices.list` reached 3 rows from three `open` calls here, one of them `--print`. The sentence
is the only thing wrong: **`--print` is the honest path on a headless Windows box and it works
perfectly.** What the plain command can truthfully say is that it asked Windows to open the
address, and that `--print` is there if nothing appeared.

### 4.8 The one thing the session list cannot say is why it is empty

`ps_windows.go` writes a note with its incomplete reading — *"the Windows process scan is not
implemented yet"* — and `tmux.go` writes *"tmux is not installed"* with its complete empty one.
`internal/app/inventory.go:91` merges both into `merged.Notes`, and **no transport reads that
field.** `contract.Scan` has `complete`, `completed`, `emptyAuthoritative`, `epoch`,
`generation` and `provenance`, and no `notes`; `/v1/diagnostics` reports its sources as
`[{"complete":false,"source":"ps"},{"complete":true,"source":"tmux"}]`, also with no notes.

So a person looking at an empty console on Windows is told, correctly, that the reading was
incomplete, and is never told that the reason is that this platform's scanner does not exist
yet. The adapters already write the sentence. Carrying it costs one field on `Scan` and one on
the diagnostics source rows — not a one-line change, since it is the generated contract, which
is why it is described here and not in the patch.

### 4.9 Smaller things worth knowing

- **A state directory on a UNC path is accepted in silence.** `CLAWDLINE_NEXT_DIR=\\127.0.0.1\C$\clawdline-unc`
  and `doctor` printed that path and created `clawdline.sqlite3` on it, exit 0, no warning.
  `docs/cross-platform.md` §3.4 and §9 ask for a refusal at startup with a reason, because
  SQLite's WAL locking is unreliable over SMB; there is no `GetDriveType` call anywhere in the
  tree. On Windows this is not exotic — enterprise folder redirection puts `%APPDATA%` on a
  network share by policy.
- **Nothing can reclaim a dead daemon's effects.** `store/pid_other.go` (`//go:build !unix`)
  returns `(false, false)` from `processGone` — honestly unknown, and unknown is correctly not
  "gone" — so the outbox effect and the coordination lease of a daemon that was killed wait
  for a person, on every Windows machine, always. The Windows answer is
  `OpenProcess`/`GetExitCodeProcess`; the file's own comment already says what it is standing
  in for.
- **`GET /assets/` returns a directory listing** (`200`, 591 bytes). `http.ServeFile` does that
  for a directory, and the page handler does not stop it. It is not a Windows fact and it leaks
  nothing but the bundle's own file names, but it is a route nobody meant to publish.
- **The filesystem folds case and the routes do not.** `/INDEX.HTML` and `/Index.Html` both
  served the document (NTFS is case-insensitive), while `/v1/HEALTH` answered `501` and
  `/V1/health` `404`. Nothing here depends on a path's case today, so this is a trap rather
  than a defect: any future allow- or deny-list keyed on an exact spelling under the web root
  will be case-insensitive on Windows and case-sensitive everywhere else.
- **A bare `%` in a path never reaches this daemon's vocabulary.** `/v1/sessions/%0` is not
  valid percent-encoding, so Go's own mux answers `400 Bad Request` in plain text before any
  handler runs; `%250` reaches the handler and gets the good refusal quoted in §3. tmux pane
  ids are `%0`-shaped, so every client must encode them — and when one forgets, what comes
  back is not one of this project's sentences.
- **Files held open cannot be deleted or replaced.** The running `clawdline.exe` refused
  deletion; `clawdline.sqlite3` refused deletion while the daemon held it
  (*"because it is being used by another process"*). Neither is a defect — it is what Windows
  is — but it means an uninstaller must stop the daemon first, and it is the mechanism behind
  the `bin\clawdline.exe` warning in §1.
- **The settings page still offers what this machine does not have.** The built settings bundle
  mentions `hotkey`, `iTerm`, `mascot` and `notch`, and mentions neither `capabilities` nor
  `diagnostics` — it never asks what this platform can do. Rule 4's second half is unbuilt,
  exactly as the Linux round found; on Windows the offer is an ⌥Space hotkey and an iTerm2
  bundle-id list on a machine that has neither.
- **A `POST` to `/v1/settings` is `405`**: the route is `GET or POST`, and `PUT` is refused by
  name. Reading the file is fine.

## 5. Not run

The console was never opened in a real browser: this image has no browser, and no inbound port
was opened, so what is measured is that the document and all twenty assets it names are served
and that the HTML carries its filled strings slot — not that it draws. Nothing was measured
about ConPTY, because it does not exist. No assistant is installed, so `clawdline assistants`
read two `unknown`s rather than a real quota, no transcript was ever parsed, and the usage
route had nothing to fall back to. Not run: a Traditional Chinese Windows (CP950) console —
§4.5's numbers are from a CP437 one; a non-SYSTEM interactive user, so the profile ACL in §4.6
is inherited from a service profile rather than a person's; reboot survival; a ten-minute SSE
soak; the layout at 760px; `cloudflared` with the binary present; whisper end-to-end; WSL2 in
any form; and every `HasPrefix(p, "/")` site in §4.2 except the one the dispatch meets first.
