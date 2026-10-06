# Updates: is this machine behind the cloud?

The hosted console at app.clawdline.com moves ahead as features land. A machine left on an older
daemon shows "讀取失敗" beside console features it predates, and until 2026-10-04 nothing said the
machine was behind: that day `https://app.clawdline.com/BUILD.json` named `4b7c3f8d…` while a
machine's daemon served `08b4277b…`.

This page says what the daemon and the CLI report, how a machine installed from a release updates
itself and rolls back, and how a development machine running a source build catches up.

There are three kinds of install, and `GET /v1/update` names which one answers in `install_kind`:

| `install_kind` | The daemon runs from | Compared with |
|---|---|---|
| `release` | `<root>/releases/<version>/clawdline` through `current`, unpacked from a signed release | the release channel's signed manifest ([Release installs](#release-installs)) |
| `source_deploy` | `<root>/releases/<commit>/` put there by `tools/deploy-linux-user.sh` | the hosted console's `BUILD.json` |
| `source_checkout` | a binary built in a checkout, or anywhere else | the hosted console's `BUILD.json` |

A release install never reads the hosted `BUILD.json`: the console's commit says nothing about
which release this machine should run.

## What "latest" and "running" mean for a source build

- **Latest** is the `BUILD.json` the hosted console serves: `<DefaultAppOrigin>/BUILD.json`, where
  `DefaultAppOrigin` is `https://app.clawdline.com` (`internal/adapters/cloud/settings.go`). Set
  `CLAWDLINE_NEXT_UPDATE_URL` to ask another `BUILD.json`; set `CLAWDLINE_NEXT_UPDATE_CHECK=off`
  to turn the check off.
- **Running** is the `BUILD.json` in the web dist this daemon serves (`CLAWDLINE_NEXT_WEB`). When
  there is none, it is the binary's own full `vcs.revision`, with no commit time.

Every `BUILD.json` writer records the commit as `stamp` and its committer time (RFC3339 UTC) as
`committed_at`: `tools/build-linux-user-release.sh`, `tools/package-macos.sh` (into the bundle's
`Contents/Resources/web`), and the hosted console's build steps in
[hosted-console.md](hosted-console.md). A `BUILD.json` written before 2026-10-04 has only `stamp`;
readers accept it.

## The states

| State | Means |
|---|---|
| `current` | Running and latest are the same commit. |
| `update_available` | The commits differ and latest was committed later. |
| `ahead` | The commits differ and running was committed later (a local build of unreleased work). |
| `differs` | The commits differ and a commit time is missing on one side, so neither is known to be later. |
| `unknown` | No comparison could be made; `reason` says why (check off, latest never read, own commit unknown). |

## The check

The daemon reads latest when it starts and again every 1800 s, each read within 10 s and at most
4096 bytes. A request never waits on the network: it is answered from the last read. A failed read
keeps the last good latest and says what failed in `error`. The bounds are the capacity rows
`update.refresh_seconds`, `update.fetch_timeout_seconds` and `update.build_body_bytes`
([limits.md](limits.md)).

`GET /v1/update`, with the same local authentication as its sibling read routes, answers
`UpdateStatus` (`api/v1/system.schema.json`):

```json
{"state": "differs",
 "running": {"stamp": "08b4277b…"},
 "latest":  {"stamp": "4b7c3f8d…", "committed_at": "2026-10-04T02:31:05Z"},
 "checked_at": "2026-10-04T12:40:00Z",
 "source_url": "https://app.clawdline.com/BUILD.json"}
```

This is the shape a release built before `committed_at` existed answers: its BUILD.json has
only a stamp, so the two commits differ and neither is known to be later. Once both sides carry
`committed_at` the state is `update_available` or `ahead`. `reason` appears with `unknown`; `error` while the most recent read failed. Clawdline Cloud
carries the route as the parameterless machine read `update`, so a phone asks the same question
the local console does.

## The console's notice

The Settings page reads `/v1/update` once when the console mounts and then no more often than
every ten minutes (`web/console/src/machine/UpdateNotice.tsx`). Only `update_available` and
`differs` show anything: one quiet line naming the running and latest stamps (eight characters
each) and that `clawdline update --apply` brings this machine up to date. `current`, `ahead`,
`unknown`, and a read that is refused or fails — a daemon predating the route answers 404, an
older one over Cloud `unknown_command` — show nothing.

## The CLI

```sh
clawdline update           # running vs latest, and the state
clawdline update --json    # the route's body
```

It asks the running daemon; when no daemon answers, it reads both stamps itself. Exit status: 0
for `current` or `ahead`, 10 for `update_available` or `differs`, 3 for `unknown`.

### Catching up a development machine now

```sh
clawdline update --apply [--force]
```

On Linux, inside a source checkout, it runs `git fetch origin main`, requires the latest stamp to
be `origin/main` or an ancestor of it, and runs `tools/deploy-linux-user.sh --rev <stamp>`. The
deploy builds that commit in an ephemeral checkout, so the shared checkout's HEAD does not move,
then switches, restarts, verifies and rolls back as [linux.md](linux.md) describes. It refuses
when the state is `current` or `ahead` unless `--force` is given, and refuses outside a source
checkout.

On macOS it does not apply anything: it prints the commands — a disposable worktree at the stamp
and `tools/package-macos.sh` run from it — and exits non-zero.

## Release installs

A machine set up from a release (`install_kind: release`) learns of new releases from a signed
manifest and can install them itself. The manifest, its signatures and the version rules are in
`internal/adapters/release/` and are built by `tools/release/`; the install layout is in
`internal/adapters/install/`.

### The check

The daemon reads `<base>/manifest.json` and `<base>/manifest.sig.json` when it starts and again
every 6 hours plus up to 30 minutes of random jitter, each read within 30 s. The manifest is
accepted only when a release key compiled into the binary signed it (`release.Open`); anything
else is reported in `error` and never followed. The base is:

- `CLAWDLINE_NEXT_RELEASE_URL` when set (a local release server; a named version is read from the
  sibling path whose last segment is the version);
- otherwise, on the `stable` channel, `https://github.com/sainteye/clawdline/releases/latest/download`;
- on the `beta` channel, the newest release tag the GitHub Releases API lists, pre-releases included.

The channel is `CLAWDLINE_NEXT_RELEASE_CHANNEL` when set, else `beta` when the running version has a
pre-release suffix (`v0.10.0-beta.1`) and `stable` otherwise. A host that has published nothing
answers 404: the state is `unknown` with `reason: "no_release_published"`.

`GET /v1/update` then answers, besides the fields above:

```json
{"state": "update_available",
 "install_kind": "release",
 "channel": "stable",
 "running": {"version": "v0.10.0", "stamp": "08b4277b…"},
 "latest":  {"version": "v0.11.0", "stamp": "4b7c3f8d…", "committed_at": "2026-10-04T02:31:05Z",
             "notes_url": "https://github.com/sainteye/clawdline/releases/tag/v0.11.0"},
 "auto_apply": false,
 "apply": {"state": "idle"}}
```

States compare versions, not commit times: `update_available` when latest is a newer version,
`current` when it is the same, `ahead` when it is older.

### Applying a release

```sh
clawdline update --apply                       # the channel's newest release
clawdline update --apply --version v0.11.0     # that release, when it is newer
clawdline update --apply --version v0.10.0 --force   # that release even when it is older
```

The CLI asks its own daemon (`POST /v1/update/apply`, body `{"version", "force"}`) and follows
`GET /v1/update` until the update settles: exit 0 when it is `healthy`, 1 when it `rolled_back` or
`failed`, 3 when it has not settled within 25 minutes (the download window plus the pending deadline). The route takes this Mac's own
machine token or a device that may send; anything else is refused 403. It answers 202 once the
update has started, 409 with a code when it is refused, 502 when the release host could not be
read.

The update runs in these steps; `apply.state` names the one it is in:

1. **Refusals**, answered at once: the daemon must be a release install running as a service, no
   update may be pending or holding `update.lock`, and the release must be newer than the running
   one (or `--force` with `--version`), at least its `min_version`, and carry this OS and
   architecture.
2. **`downloading`** into `<root>/staging`, at most 512 MiB and 15 minutes.
3. **`verifying`**: size and sha256 against the signed manifest, then a safe unpack into
   `releases/<version>` (no absolute paths, no `..`, no link leaving the release, no hard links or
   devices, at most 20000 entries and 2 GiB), then a smoke run of the new binary's
   `version --json`, which must name the manifest's version and commit. On a Mac with the app
   installed, the release's app bundle is downloaded and unpacked beside it.
4. **`staged`**: the store is copied with SQLite's `VACUUM INTO` to
   `<state>/backups/<from>-<to>.sqlite3` (the two newest are kept), and `<state>/update/pending.json`
   records the update.
5. **`restarting`**: a supervisor starts outside the daemon's service, so restarting the daemon does
   not kill it — `systemd-run --user --unit <unit>-update` on Linux, a one-shot LaunchAgent
   `<label>.update` on macOS — and runs `clawdline update finish` from the old release. It points
   `current` at the new release, restarts the service, and waits up to 60 s for `GET /` to answer
   200 and the served `BUILD.json` to name the new commit.
6. **`healthy`**: the update is done. Releases besides `current` and the two newest others are
   removed. On macOS the staged app bundle replaces the installed one now if the app is not
   running; otherwise `apply.staged_app` names it and the daemon swaps it in within a minute of the
   app quitting. The replaced bundle is kept under `releases/<from>/app`.
7. **`rolled_back`**: the new release did not come up. `current` points at the old release again,
   the service is restarted, and, when the manifest says `non_additive_migration`, the old release
   restores the store snapshot before it opens the store. The version is remembered in
   `<state>/update/failed.json` so auto-apply does not try it again; `--apply` still may.

A failure before step 5 changes nothing the running daemon uses and is recorded as `failed`.

### When something dies halfway

- **The supervisor is killed.** Its job restarts it (at most 5 starts for one update); it reads
  `pending.json` and continues the step it was in. Past the limit it rolls back with
  `supervisor_gave_up`.
- **The new release keeps crashing.** Each start of the new release while the update is pending is
  counted. At its fourth start, or once it starts more than 10 minutes after the update was
  handed over, its boot guard switches `current` back, records `boot_guard`, and exits so the
  service starts the old release.
- **The download is interrupted.** `update.lock` is released; one left by a process that died is
  taken over after 30 minutes.

### Errors

`apply.error.code` and the route's refusals carry one of these, or a release manifest code from
`internal/adapters/release/release.go`:

| Code | Means |
|---|---|
| `update_in_progress` | another update holds the lock or is pending |
| `not_a_release_install` | the daemon runs from a source build; see below for its path |
| `not_installed_as_service` | the daemon was not installed as a service, so `<state>/service.json` is missing |
| `already_current` / `version_not_newer` | nothing newer to install (use `--version … --force`) |
| `version_below_min` | the release needs a newer running version first |
| `version_failed_before` | auto-apply skips a version that rolled back |
| `no_release_published` | the release host has no manifest |
| `release_unreachable` | the release host could not be read |
| `download_failed` / `archive_unsafe` / `smoke_run_failed` | the artifact was not fit to install |
| `snapshot_failed` / `supervisor_failed` | the update could not be handed over; nothing changed |
| `switch_failed` / `health_timeout` / `boot_guard` / `supervisor_gave_up` | the new release did not come up; rolled back |
| `app_swap_deferred` | healthy, but the staged app bundle could not be swapped in yet |
| `update_state_unreadable` | a file under `<state>/update/` could not be read; the state is unknown, not idle |

### Automatic updates

Off by default, and per machine:

```sh
clawdline setting set update_auto_apply true
```

When on, after each check the daemon installs a newer **stable** release by itself — never a
pre-release, never a version that rolled back before — and only when no assistant session it
started is working. A session whose state it cannot tell counts as working; a busy machine waits
for the next check.

## Source builds

`clawdline update --apply` on a source build is the development path described in
[Catching up a development machine now](#catching-up-a-development-machine-now): a source checkout
keeps deploying commits, not releases.
