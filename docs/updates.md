# Updates

Clawdline installed with `install.sh` is a **release install**: it learns of new signed releases by
itself and can install them, and goes back to the release it was running when a new one does not
start. This page first answers the four questions most people have, then describes release
installs in full, and last, how a development machine running a source build is compared and caught
up.

## In short

- **How you know there is a new release.** The Settings page's **Clawdline version** panel shows the
  running and the latest version; when a newer release is out, the session list also shows a banner
  that links there. In a terminal, `clawdline update` says the same and exits 10 when a newer
  release is available.
- **How to install it.** Press **Update now** in that panel, or run `clawdline update --apply`.
  Clawdline restarts for a few seconds; the sessions running in tmux keep running.
- **What happens when it fails.** If the new release does not come up within 60 seconds, Clawdline
  switches back to the release that was running and starts it again, by itself; there is nothing to
  do. Automatic updates will not try that version again. A failure before the restart changes
  nothing at all.
- **Automatic updates.** Off by default, per machine. Turn them on with the **Automatic updates** switch in
  the same panel, in a console signed in on that machine, or with
  `clawdline setting set update_auto_apply true`. They install only **stable** releases, never a
  pre-release, and wait until no assistant session is working
  ([Automatic updates](#automatic-updates)).

## Release-day procedure for an installed Mac

The macOS LaunchAgent uses `<install-root>/service/clawdline`, a stable real
path selected atomically from the installed release. TCC records an unbundled
daemon's Automation permission against that path, so the version directory
can change without changing the path shown to TCC. Service installations keep
the existing App bundle when its native shell revision is unchanged; the
daemon serves the current web console. A native shell change replaces the App
and needs an authorized Developer ID Application signature to preserve its
WebKit keychain identity. The first move from the old signing identity may
ask once more. Acceptance requires checking two successive builds with both
Automation and WebCrypto enabled; a passing unit test does not establish
macOS permission persistence.

The homepage's one-line installer is for the first installation. Once this Mac is a release
install, use Clawdline's built-in updater for subsequent releases. Publishing a GitHub release or
deploying the hosted console does not require quitting the local app or restarting its service.

1. Keep the installed app and service running while the new release is built, signed, reviewed,
   published, and checked. Do not run the installer again, install a locally built app over it,
   quit the app, kill its process, or restart its LaunchAgent to prepare for a release.
2. After the signed release is public, let the installed Clawdline discover it. Use **Update now** in
   the local console, or enable **Automatic updates** and let its idle-session rule choose when to apply it.
   Do not substitute `clawdline setup`, a manual bundle replacement, or a service restart for this
   update path. When a person wants to experience the prompt, verify that the button appears and
   leave the install action for that person to initiate.
3. Read the updater's result. Confirm `apply.state: healthy`, the running version and served
   `BUILD.json`, `GET /` returning 200, and that existing sessions remain available. If it reports
   `failed` or `rolled_back`, preserve the previous running release and record the error code;
   investigate before trying again.
4. On macOS, a healthy daemon update can leave the app bundle staged while the app remains open.
   `apply.staged_app` records that state. The app puts the staged bundle in place by itself
   (`shell/darwin/StagedAppWatcher.swift`): it reads `GET /v1/update/fresh` 3 s after launching
   and whenever the app returns to the foreground. Its 30 s staged-bundle poll reads cached
   `GET /v1/update`. If a fresh request fails, it reads the cached staged-bundle state. When a
   bundle is staged for a version other than its own it restarts into it —
   at once within a minute of launching, otherwise only while it is not in front and its input bar
   is closed. A restart the person did not ask for reopens behind other apps (`open -g`), and a
   window that was on screen comes back. One version is tried at most once every 30 minutes, so a
   failing swap cannot become an app that keeps restarting. **Restart app now** in the update banner
   and Settings panel does the same at once. On 2026-10-15, before this, the installed app stayed
   at 1.0.1 through four releases: closing the window does not quit a menu bar app, and a quick
   quit-and-reopen started the old bundle again. Report the running daemon version and the app
   bundle version separately until they agree. An app older than this watcher needs one ordinary
   quit and reopen after its first update; a newly served web page cannot add a native action to an
   already running old shell. Do not force the app to quit merely to clear `staged_app`.

This procedure keeps the existing console available through the release approval and download
period. The updater itself performs the brief service restart in step 5 below, with its health
check and rollback; a release operator does not start that restart independently.

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
`failed`, 3 when it has not settled within 25 minutes (the download window plus the pending deadline). The route takes this machine's own
token or a device that may send; anything else is refused 403. It answers 202 once the
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
   200 and the served `BUILD.json`, read with the local token, to name the new release's version and
   commit (a release candidate and its final release share a commit; only the version tells them
   apart).
6. **`healthy`**: the update is done. Releases besides `current` and the two newest others are
   removed. On macOS the staged app bundle replaces the installed one now if the app is not
   running; otherwise `apply.staged_app` names it and the daemon swaps it in within a few seconds of the
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
  service starts the old release. On macOS, launchd waits 30 s between restarts of a crashing
  service, so the supervisor's 60 s health wait runs out first and the rollback is recorded as
  `health_timeout`; on Linux it is `boot_guard`. Either way the previous release runs again.
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

Off by default, and per machine. Turn it on with the **Automatic updates** switch in the Settings page's
**Clawdline version** panel, in a console signed in on that machine, or from a terminal:

```sh
clawdline setting set update_auto_apply true
```

When on, after each check the daemon installs a newer **stable** release by itself — never a
pre-release, never a version that rolled back before — and only when no assistant session it
started is working. Concretely: an assistant session seen working, wherever it runs; a tmux session
(or one the daemon opened) whose state it cannot tell; or a tmux reading that did not finish. An
iTerm2 session it cannot read does not hold the update back, because a restart never touches it —
on a Mac whose iTerm2 does not answer, that rule would otherwise wait for good. A busy machine logs
`update <version> is waiting: a session is busy` and looks again every 5 minutes
(`release.auto_apply_retry_seconds`), so the update starts once the sessions are idle rather than
at the next check hours later.

## Dev builds: testing a commit on a release install

A machine installed from a release can run a commit of this repository in place of its release, to
test it, and go back to the release afterwards. The two take turns: one daemon, one port, one store.
Decided on 2026-10-15, so that a person tests the dev build on the machine they use every day and
returns to the stable release when they are done.

```sh
clawdline dev install [--rev <commit>]     # inside a clawdline checkout
```

- **One entry point.** The same command works from a stable release, another
  release-based dev build, or a Linux source deployment. A release-based dev keeps
  the stable version base. A Linux source deployment uses the existing service
  layout through `tools/deploy-linux-user.sh --dev --rev <commit>` and stamps
  `v0.0.0-dev.<short commit>`; it remains a source install rather than pretending
  to be a signed release. Omit `--port` on that path: the service configuration
  owns the port. An unmanaged checkout is refused instead of guessing a service.
  Source dev builds skip the complete Go tests, vet and web checks, then verify
  the actual served stamp and console before accepting the switch. The existing
  source rollback restores the previous `current` if startup fails. Returning
  from a source dev follows the source deployment procedure, not the signed
  release updater. Full CI remains visible; public Release still requires it.

- **What it installs.** The commit is built in a disposable worktree by
  `tools/release/build.sh --channel dev --host-only` — this machine's os/arch only, unsigned,
  carrying the tmux of the running release (`--tmux-installed`) — and installed as
  `releases/v<x.y.z>-dev.<short commit>/`, where `v<x.y.z>` is the release it goes over. The
  switch is the updater's own (`updater.BeginLocal`, then `Execute`): store snapshot, supervisor,
  health check, rollback when it does not come up. The checkout's HEAD does not move.
- **Who can start it.** Only the CLI on this machine. The unsigned manifest is a local file the
  CLI hands the updater; no route takes one, so Cloud and a phone cannot install a dev build.
  `BeginLocal` refuses a version that is not `-dev.` (`not_a_dev_build`).
- **The way back.** `v1.0.7-dev.4c41b18` sorts before `v1.0.7`, and a dev build follows the stable
  channel (`ChannelFor`), so the release it went over reads as available: the ordinary signed
  update installs it. Automatic updates never take a dev build away (`dev_build_running`); the
  person chooses when to go back.
- **Past the release or short of it.** Versions cannot say which, so GET `/v1/update` carries `dev`
  (`UpdateDevStanding`): `git rev-list --left-right --count <release>...<dev>` in the first Project
  repository on this machine that holds both commits — `ahead`, `behind`, `diverged`, `same`, or
  `unknown` with a reason when none does. It does not fetch, and it is computed once per pair of
  commits (`update_dev_standing.go`). The panel shows it beside the running fact, and the press is
  named for what it does: **Back to v1.0.7** when the dev build is past the release or beside it,
  **Update to v1.0.7** when the release is newer. **Release this version** is not offered for a
  dev build that is behind the release or is its commit.
- **The session list's line.** While a dev build runs, the top of the session list carries one
  30 px line that cannot be dismissed: `DEV`, the commit, its standing, and the same presses as
  words (`UpdateBanner.tsx`). A newer release on an ordinary install takes the same line, with
  `NEW`, a press that installs it, the release notes and a close button; so does the Cloud list's
  notice for each machine. Installing a dev build is not announced as a finished update: the line
  already says what runs.
- **Releasing it.** Beside the way back, the machine's own console shows **Release this version**.
  It posts `/v1/update/dev-release`, which finds the Project whose repository holds the running
  commit, fetches `main`, refuses a commit that is not on `origin/main`
  (`dev_commit_not_on_main`; `dev_project_not_found` when no Project holds it), and opens a Root
  Assignment whose brief is [releasing.md](releasing.md) for exactly that commit. That Root stops
  before the `release` environment's approval and before `gate.sh --publish`, which stay the
  person's, and notifies them. The request id is derived from the commit, so a second press
  answers the Root already opened.
- **The store.** The switch to a dev build snapshots the store like any update. Store migrations
  are additive by rule, so the release reads a store a dev build has touched; a dev build whose
  change is not additive must not be installed this way.

## The console's update panel

The Settings page's **Clawdline version** panel (`web/console/src/machine/UpdatePanel.tsx`, its logic
in `update-model.ts`) shares one update reading with the session list's banner. While the
console is visible, it reads when first opened and every ten minutes. These reads ask
`GET /v1/update/fresh`, which contacts the update source within a ten-second request bound;
ordinary progress polling keeps using the cached `GET /v1/update`. A hidden console stops its
periodic reads. Returning from hidden to visible, or from a blurred window to focus, immediately
reads `/v1/update/fresh` even if the previous answer is under ten minutes old. Visibility and focus
events from the same return share one request; repeated focus events without a preceding blur
keep the ten-minute freshness rule. A page restored from the browser's back-forward cache also
reads immediately. During a return check the previous update prompt and install action are hidden;
a response from before the page was backgrounded cannot restore them. A network return checks a
stale reading and retries a recent failed reading. The banner disappears when a fresh answer says the
release is current, unknown, or otherwise no longer available, and when the read fails.
The panel shows the running and latest version (a short commit for a source build), a **Release notes** link to
`latest.notes_url`, when the last check ran, and the channel.

- On a release install with `update_available`, **Update now** posts `/v1/update/apply` (over Cloud,
  the word `update-apply`), then reads `/v1/update` every two seconds until `apply.state` settles.
  The seconds in which the daemon restarts and no read answers show as restarting, not as a
  failure; when the new daemon serves a different console, the page reloads itself.
- `rolled_back` and `failed` show `apply.error.code` and `detail` and say the previous release still
  runs. The session-list banner animates while an update is checking or moving, keeps the completed
  result visible after a page reload until dismissed, and shows `staged_app` beside that result: the
  daemon has updated, while the open Mac app keeps its old bundle until it restarts into the new
  one by itself, the person presses **Restart app now**, or it next quits.
- **Automatic updates** writes `update_auto_apply` through `/v1/settings`. Over Cloud it is shown but disabled,
  with a sentence saying to change it on the machine: the settings route takes only the machine's
  own token.
- `source_deploy` and `source_checkout` show no button, only how that machine updates and that
  `clawdline setup --adopt` moves a Linux source deploy onto releases.
- An older daemon — no `install_kind`, or a `501 not_implemented` for the POST — gets the earlier
  quiet line or the needs-update sentence, never "Could not load".

On a release install with `update_available`, the session list also shows a per-version banner.
Its **Update now** button rechecks the signed release source, starts the update directly, and shows
progress and the final result in the same place. A withdrawn release or an unavailable source
cannot be installed from an old banner. Dismissing the banner is remembered per version in that
browser. A newer available release replaces the previous update's completion message and exposes
its install action immediately, without first dismissing that message. Dismissing the newer
offer does not bring the older completion back; a later release can still appear. Closing a
result remembers only the result being shown, not a newer offer hidden behind it. Settings
retains the full version check, release notes, update history, and update controls.

A running dev build keeps a banner visible on both the machine's console and the Cloud
**All machines** list, including Linux source deployments and source checkouts. It shows
the dev commit even when there is no newer build. Source builds do not offer signed-release
return or release actions; their update status includes the running daemon version so the
console can recognize a dev build. The Cloud banner names the machine and opens its console.

The Cloud console's **All machines** Session list also checks each paired online machine for a
fresh signed release when the row opens or the page returns from hidden, blur, or the back-forward
cache. Its ten-minute checks are fresh; progress polling reads cached status. If a fresh client
request fails, cached running and apply facts remain visible but latest becomes unknown, so an
old cached release is never offered. It names the machine and available version in a banner, including when this
page is shown inside the Mac app's Cloud tab. Its button switches to that machine's console, where
the person can choose **Update now**; the banner itself never starts an update. An offline, unpaired,
unanswered, or rolled-back machine cannot produce this prompt. The local Mac app and its Cloud tab
remain open while the release is prepared and while the prompt is shown.

On a release install, the fresh route rereads the signed manifest. A 404 clears a withdrawn release;
an unreachable or invalid source produces `unknown` instead of showing a cached version as
available. Concurrent background and foreground checks are serialized, so an older result cannot
overwrite the new one. Overlapping checks share one source read; sequential foreground returns
still check again. A failed console client request falls back to cached running and apply details,
while treating latest as unknown. Over Cloud this route uses the distinct `update-fresh` word; a daemon
predating that word refuses it and the console suppresses the prompt. The request depends on the
release host answering; an unreachable host cannot establish whether a release was withdrawn.
The remaining cross-platform acceptance evidence is tracked in
[the update acceptance record](records/app-update-acceptance-2026-10-12.md).

## The CLI

```sh
clawdline update           # running vs latest, and the state
clawdline update --json    # the route's body
clawdline update --help    # every option
```

It asks the running daemon; when no daemon answers, it reads both versions itself. Exit status: 0
for `current` or `ahead`, 10 for `update_available` or `differs`, 3 for `unknown`. A failed check
is one line in words with its code last; after a rollback it says that automatic updates will not
try that version again, and `update --apply` that ended in a rollback says so in one sentence and
points at `clawdline update --json` for the details.

## Development machines

A machine running a source build — a checkout, or a source deploy made by
`tools/deploy-linux-user.sh` — is not updated by releases. It compares itself with the hosted
console instead.

The hosted console at app.clawdline.com moves ahead as features land. A machine left on an older
daemon shows "Could not load" beside console features it predates, and until 2026-10-04 nothing said the
machine was behind: that day `https://app.clawdline.com/BUILD.json` named `4b7c3f8d…` while a
machine's daemon served `08b4277b…`.

There are three kinds of install, and `GET /v1/update` names which one answers in `install_kind`:

| `install_kind` | The daemon runs from | Compared with |
|---|---|---|
| `release` | `<root>/releases/<version>/clawdline` through `current`, unpacked from a signed release | the release channel's signed manifest ([Release installs](#release-installs)) |
| `source_deploy` | `<root>/releases/<commit>/` put there by `tools/deploy-linux-user.sh` | the hosted console's `BUILD.json` |
| `source_checkout` | a binary built in a checkout, or anywhere else | the hosted console's `BUILD.json` |

A release install never reads the hosted `BUILD.json`: the console's commit says nothing about
which release this machine should run.

### What "latest" and "running" mean for a source build

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

### The states

| State | Means |
|---|---|
| `current` | Running and latest are the same commit. |
| `update_available` | The commits differ and latest was committed later. |
| `ahead` | The commits differ and running was committed later (a local build of unreleased work). |
| `differs` | The commits differ and a commit time is missing on one side, so neither is known to be later. |
| `unknown` | No comparison could be made; `reason` says why (check off, latest never read, own commit unknown). |

### How a source build is checked

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

### Source builds

`clawdline update --apply` on a source build is the development path described in
[Catching up a development machine now](#catching-up-a-development-machine-now): a source checkout
keeps deploying commits, not releases.
