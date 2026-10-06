# Troubleshooting

This page takes you from a symptom to its cause and the fix, and tells you how to confirm the fix
worked. Start with the three commands below; most problems name themselves there.

## First, ask the machine

```sh
clawdline doctor                                              # version, port, state directory, store counts
curl -s http://127.0.0.1:7727/v1/health                             # {"ok":true,"served_by":"clawdline-go",…}
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:7727/     # 200 means the console is served
```

- `served_by` tells this daemon apart from anything else answering on the port.
- **Health says the daemon is alive, not that it shows a console.** After a restart, check `/` as
  well: a daemon run from a checkout without `CLAWDLINE_NEXT_WEB` is healthy and has no page. An
  installed release serves the console next to its binary and needs no setting.
- `clawdline` here is the installed command. Run from a checkout, it is `./bin/clawdline`.
- Everything the daemon does goes to `logs/daemon.log` in the state directory
  (`~/.config/clawdline-next` unless `CLAWDLINE_NEXT_DIR` says otherwise). The line under
  `listening` says `console: served from …`, `console: NONE` or `console: BROKEN`.
- If you run the command line with a different `CLAWDLINE_NEXT_PORT` or `CLAWDLINE_NEXT_DIR` than
  the daemon, give it the same values: that is how it finds the daemon and its local token.

## The daemon and the console

| You see | It means | Do this |
| --- | --- | --- |
| `501 no_web_root` on `/` | A daemon run from a checkout: `CLAWDLINE_NEXT_WEB` is not set, and no console sits beside the binary | Restart `serve` with `CLAWDLINE_NEXT_WEB="$PWD/web/console/dist"` |
| `500 no_document` on `/` | `CLAWDLINE_NEXT_WEB` has no `index.html`: the console was not built, or is being rebuilt | `(cd web && npm install && npm run build)` |
| The daemon exits with status 3 | Another process holds the port | `logs/daemon.log` names it: pid, command, since when, and whether it is a Clawdline daemon. Stop that process by its pid, or start this one with another `CLAWDLINE_NEXT_PORT` |
| `the daemon did not answer` from the command line | Nothing is listening on that port, or the command line and the daemon use different ports | Start `serve`, or set the same `CLAWDLINE_NEXT_PORT` for both |
| `501 not_implemented` on a `/v1/` route, or **需要更新** (*needs update*) beside a console feature | This machine's daemon is older than the console and does not have that route; the body names it | Update the machine ([updates.md](../updates.md)); the console's **前往設定更新** (*go to Settings to update*) link goes there |
| `502 upstream_unreachable` | `CLAWDLINE_NEXT_UPSTREAM_PORT` is set and nothing listens there | Unset it |

## Installing and updating

The installer and `clawdline setup` stop with a code and a sentence that says what to do. Running
the installer again after the fix is safe: it picks up from what is already in place.

| Code | It means | Do this |
| --- | --- | --- |
| `tmux_missing` | tmux is not installed | Run the install command it prints, then the installer again |
| `platform_unsupported` | Not macOS or Linux on x86-64 or ARM64 | See [platforms.md](platforms.md); Windows runs from source |
| `release_not_verified` | The download does not match the release's signature or checksum | Run it again. If it repeats, do not work around it; report it with the output |
| `port_held_by_other_daemon` | Something else already answers on the port, often a `clawdline serve` you started | Stop it as the message says, or install with `--port N` |
| `source_deploy_needs_adopt` | This Linux machine runs a deploy from a checkout | Keep it, or install with `--adopt` to move it onto releases |
| `no_user_service_manager` | Linux: this login has no `systemd --user` (you came in through `su` or `sudo -u`) | Log in as that user over SSH or a desktop session, or have an administrator run the `loginctl enable-linger` line it prints |
| `service_failed` | The LaunchAgent or systemd unit did not start | `logs/daemon.log`, then `journalctl --user -u clawdline-next` (Linux) or `launchctl print gui/$(id -u)/<label>` (macOS) |
| `health_check_failed` | The service started, but the console did not answer as the release just installed | `logs/daemon.log` says why; fix it and run the installer again |
| `session_check_failed` | The daemon could not open and close a tmux session | `tmux -V` as the same user; `--no-session-check` skips it on a machine meant to have none |
| `channel_mismatch` | You asked for a channel other than the one this machine follows | Pass `--channel` again to switch |
| `current_not_a_release`, `not_installed` | The command needs an installed release and this machine has none | Install it first |

An update that does not come up healthy rolls back by itself; the console's Settings page and
`clawdline update` give the reason ([updates.md](../updates.md)).

## Sessions

| You see | It means | Do this |
| --- | --- | --- |
| A session is missing from the list | It is not running inside tmux (or iTerm2 on a Mac), or tmux is not on `PATH` nor in `/opt/homebrew/bin`, `/usr/local/bin`, `/usr/bin` or `/opt/local/bin` | Run the agent inside tmux; `tmux -V` must work for the user running the daemon |
| The list is empty on Windows | Windows has no session discovery yet | See [platforms.md](platforms.md) |
| You can read a session but cannot type into it | The browser's device is read-only (made before `open` granted send) | Run `clawdline open` again, or see [remote-access.md](remote-access.md) for a paired device |
| A state reads as unknown, not idle | The screen or transcript could not be read, and Clawdline does not guess | Open the session's screen; the state returns when the evidence does |
| The Status Line's right edge says `方案額度 未知`, or a fresh Linux box never shows Claude's `5h`/`7d` | Claude Code hands those percentages to the stdin of `statusLine.command` and to nothing else, so with no status line configured nothing ever writes `~/.claude/statusline-cache/rate-limits.json` | Configure a status line that writes that file ([usage.md](usage.md)). **Session 資訊** names the missing file. Codex's weekly window is unaffected |

## Clawdline Cloud

| You see | It means | Do this |
| --- | --- | --- |
| The Cloud line stays off | `clawdline cloud status` gives the reason. A broken Cloud setting never stops the daemon, it only keeps the line down | Fix what it names. After `cloud on`, restart `serve` |
| A paired phone can read but not act | Commands are off on this machine | `clawdline cloud commands on` ([remote-access.md](remote-access.md)) |

## Projects across machines

| You see | It means | Do this |
| --- | --- | --- |
| `mirror_source_mismatch` | Another machine already owns that project's settings here | Choose that machine as the source, or run `project import --replace-source` to hand it over |
| `project_mirrored` when changing an icon | The project is mirrored from another machine | Change it on the source, or make it this machine's own again ([projects.md](projects.md)) |
| `clone_failed` | The receiving machine could not reach the remote with its own credentials | Clone it by hand into the named directory, then sync again |

## Still stuck

- `/v1/diagnostics` (this machine's own token) carries the console state and each platform
  capability with the reason it is unavailable.
- The design of each part, and why it fails the way it does: [docs/README.md](../README.md).
- Report a problem at https://github.com/sainteye/clawdline/issues with the `doctor` output and the
  relevant lines of `logs/daemon.log`. Read them first: they can contain paths and project names.
