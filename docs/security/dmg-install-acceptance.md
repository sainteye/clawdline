# DMG installation acceptance

The download banner is a candidate until the public DMG passes this procedure.
Do not publish the README download link based on a synthetic fixture pass.
The target is `Clawdline-v2.1.2-macos-arm64.dmg`, Apple Silicon, macOS 13 or newer:

`https://github.com/sainteye/clawdline/releases/download/v2.1.2/Clawdline-v2.1.2-macos-arm64.dmg`

## What first launch does

The native shell reads `CLAWDLINE_NEXT_PORT`, then a configured service port,
then 7727 (`shell/darwin/main.swift`, `let port`).
`ServiceMode.load()` reads `<state>/service.json`; with no service record,
`startBundledDaemon()` starts `Contents/MacOS/clawdline serve` and sets
`CLAWDLINE_NEXT_WEB` to `Contents/Resources/web`.
`BundledDaemon.start()` launches that executable directly, with its inherited
environment (`shell/darwin/Daemon.swift`). There is no CLI setup prerequisite
on this path. A service installation intentionally follows a different path:
the shell displays the service and does not start another daemon.

These source paths establish the intended startup interface. Actual DMG and
native-window acceptance below establish whether a shipped build implements it.

## Repeatable command-line acceptance

Run on an authorized Apple Silicon macOS 13+ machine with Python 3, `hdiutil`,
`codesign`, `xcrun`, `spctl`, `ditto`, `sandbox-exec`, `ps`, and `lsof` available.
The process must be allowed to mount disk images and apply a child sandbox.
Use an existing task-owned temporary parent and a new evidence directory:

```sh
bash tools/release/dmg-install-smoke.sh \
  --dmg /absolute/task/artifacts/Clawdline-v2.1.2-macos-arm64.dmg \
  --version v2.1.2 --commit <full-release-commit> \
  --work-dir /absolute/task/work \
  --evidence-dir /absolute/task/artifacts/dmg-acceptance
```

The default `--mode release` requires image integrity, a read-only mount,
`Clawdline Next.app`, an `/Applications` symlink, bundle version and macOS
minimum, app and DMG signatures, stapled app and DMG tickets, and Gatekeeper
assessment of both. It copies the bundle into a disposable `Applications`
directory, checks the bundled daemon's version/commit/platform, records the
shell revision, and launches the copied native shell executable. It never
runs `setup`, `open`, AppleScript, launchd commands, or a UI click script.

The native shell must remain alive and own the listener. `/v1/health` must
identify `clawdline-go` with `ok: true`; `/` must return HTTP 200 and HTML;
`/BUILD.json` must match both the expected full commit and version. A free
loopback port is selected and 7727 is explicitly refused. The readiness
deadline is configurable with `--timeout`; polling waits for the condition.

The test uses an isolated HOME, Claude/Codex directories, state, temporary
directory, tmux socket namespace, and disabled external Session discovery and
iTerm integration. A child sandbox denies reads/writes of the original home
outside the owned temporary tree and denies execution of AppleScript. This
also covers Foundation APIs that resolve the OS-account home independently
of HOME. If this sandbox cannot be applied, acceptance fails before starting
the shell; do not remove the isolation to obtain a pass.

Only the PID returned by launch and its verified direct bundled-daemon child
can be stopped. Cleanup detaches the owned mount and deletes only the owned
temporary tree. A cleanup failure makes acceptance fail and retains the tree
for inspection. Logs and `result.json` remain in the evidence directory;
`commands.jsonl` retains command statuses and both output streams, including
Apple trust diagnostics. Retained evidence can contain absolute machine paths;
review it before publishing. Never publish private conversation content.

## Harness controls and platform limits

```sh
bash tools/release/test-dmg-install-smoke.sh
bash tools/release/test-dmg-install-smoke.sh --portable
```

The first command creates real disposable DMGs. A synthetic executable bundle
starts a real HTTP daemon through its shell, then tests missing App, corrupt
image, wrong bundle version, wrong daemon commit, unsigned release, wrong
console stamp, HTTP failure, shell exit and wrong Applications shortcut refusal.
Every arm must clean its mount and state. No signing key is used.
`tools/check.sh` runs it on macOS when release tooling is affected and in full
checks; a non-macOS host explicitly skips this platform integration.

The portable variant doubles only platform operations (mount, read-only
filesystem flag, signing, sandbox application, copying and parent-PID query).
It still executes bundle processes, HTTP requests, comparisons, refusal and
cleanup paths. It can demonstrate harness behavior in a restricted agent
sandbox, but proves no DMG integrity, mount behavior, Apple trust, process
parent query, native-shell behavior or sandbox enforcement. A portable pass
does not replace the first command or formal release mode.

The development sandbox used for the initial implementation rejected
`hdiutil create` with "Device not configured", `sandbox-exec` with
"Operation not permitted", and the `ps` subprocess with a permission error.
No native App was launched in that child environment.

Root validation on 2026-10-22 used an authorized macOS environment. The real
image harness passed its positive arm, all nine refusal controls, and cleanup.
The official v2.1.2 App archive at source
`239adc8ad4020f31509bff017bd4e5a3ff913197` was then placed unchanged in a
local fixture image. From a disposable Applications directory it started its
bundled daemon without a CLI installation step; health, console HTTP 200,
authenticated BUILD.json, version, commit and process ownership all passed.
The owned mount, processes and state were cleaned. BUILD.json uses only the
local token created in the disposable state; that token is not written to
the evidence log. The synthetic HTTP fixture enforces the same authentication.

That image used an ad hoc container signature. This demonstrates real-image
handling and isolated official-App startup, not notarization of the final
DMG or Finder/fresh-account acceptance. Those checks remain required below.

## Visible installation and fresh-account acceptance

The root checks the published asset URL, downloads that exact asset, records
its SHA-256 and expected release commit, then runs release mode above.
After that, on a clean authorized account or machine, use Finder to mount
the downloaded quarantined DMG, drag the App to Applications, and open it
without running a CLI installer first. Check the first-launch prompts,
native window, loaded console, version and normal exit. Check Chrome against
the daemon separately. Record OS/hardware, quarantine and Gatekeeper results,
screenshots, and whether any privacy permission was requested or granted.

Launching a native executable from a terminal is not Finder/LaunchServices
quarantine acceptance. A same-account isolated HOME is not a fresh OS user
or a fresh TCC database. These UI and permission boundaries require their own evidence before claiming a
fresh-account experience. A supplementary DMG whose App is byte-identical to
an already-published release may use the real-image release-mode harness for
packaging acceptance, as specified in [the release procedure](../releasing.md).
That exception cannot be used for changed App code. No Intel build is claimed.

## v2.1.2 supplemental DMG acceptance

The signed DMG from packaging commit
`8db66b50c011ed8b4ed499b85d64b0e193a77795` carries the unchanged App from
published source `239adc8ad4020f31509bff017bd4e5a3ff913197`.
Its SHA-256 is
`2c7c2240ce78aa045eb5479c33dea104528d7ca7fc3881262dedaa742cdd0822`.
The release-mode harness passed integrity, actual read-only mounting,
Developer ID signatures, stapled tickets, Gatekeeper, copying, isolated
native-shell startup, daemon and console version/commit checks, and cleanup.
The separate gate verified the original archives, source and delivery SBOMs,
GitHub attestations, exact App equivalence and privacy scans.

Native UI attachment returned `cgWindowNotFound` for Finder and Chrome.
Manual Finder dragging, browser quarantine and fresh-account/TCC prompts were
not exercised. They are unverified, not inferred from the automatic pass.
