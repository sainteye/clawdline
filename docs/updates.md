# Updates: is this machine behind the cloud?

The hosted console at app.clawdline.com moves ahead as features land. A machine left on an older
daemon shows "讀取失敗" beside console features it predates, and until 2026-10-04 nothing said the
machine was behind: that day `https://app.clawdline.com/BUILD.json` named `4b7c3f8d…` while a
machine's daemon served `08b4277b…`.

This page says what the daemon and the CLI now report, how a development machine catches up today,
and — clearly marked — the design for other people's machines, which is **not built**.

## What "latest" and "running" mean

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

## Design for other people's machines — NOT BUILT

Nothing in this section exists yet. It is the shape the next cycle should build, so that a person
who installed a release learns of a new one and can let it install itself.

1. **A signed release manifest per channel** (`stable`, `beta`), published beside the hosted
   console. It lists the version, the commit and its `committed_at`, and one artifact per OS and
   architecture (Linux amd64/arm64 tarball, macOS arm64 app bundle, Windows amd64) with its URL,
   size and sha256. The manifest is signed with a release key whose public half is compiled into
   the binary; an unsigned or wrongly signed manifest is ignored and reported, never followed.
2. **Verify, then swap.** The daemon downloads the artifact for its own OS and architecture into
   its staging directory, checks size and sha256 against the signed manifest, and only then
   installs it. On Linux the new release is unpacked under `releases/<commit>` and the `current`
   symlink is switched atomically, as `tools/deploy-linux-user.sh` does today. On macOS the whole
   bundle is replaced, never patched in place, following `tools/package-macos.sh`'s rule that a
   running bundle is never changed: the new bundle is staged beside the old and swapped while the
   app relaunches.
3. **Health check and rollback.** After the restart, `GET /` must answer 200 and the served
   `BUILD.json` must name the new commit within the deploy health window; otherwise the previous
   release is restored and the failure is reported on the update route.
4. **Opt-in auto-apply.** By default a new release is only reported (the route, the CLI and the
   console notice). A person turns on automatic installation per machine; even then it waits for
   no running assistant work to be interrupted, or for a person to choose a time.

The development path above stays: a source checkout keeps deploying commits, not releases.
