# Troubleshooting

This page takes you from a symptom to its cause and the fix, and tells you how to confirm the fix
worked. Start with the three commands below; most problems name themselves there.

## First, ask the machine

```sh
./bin/clawdline doctor                                              # version, port, state directory, store counts
curl -s http://127.0.0.1:7727/v1/health                             # {"ok":true,"served_by":"clawdline-go",…}
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:7727/     # 200 means the console is served
```

- `served_by` tells this daemon apart from anything else answering on the port.
- **Health says the daemon is alive, not that it shows a console.** After a restart, check `/` as
  well: a daemon started without `CLAWDLINE_NEXT_WEB` is healthy and has no page.
- Everything the daemon does goes to `logs/daemon.log` in the state directory
  (`~/.config/clawdline-next` unless `CLAWDLINE_NEXT_DIR` says otherwise). The line under
  `listening` says `console: served from …`, `console: NONE` or `console: BROKEN`.
- If you run the command line with a different `CLAWDLINE_NEXT_PORT` or `CLAWDLINE_NEXT_DIR` than
  the daemon, give it the same values: that is how it finds the daemon and its local token.

## The daemon and the console

| You see | It means | Do this |
| --- | --- | --- |
| `501 no_web_root` on `/` | `CLAWDLINE_NEXT_WEB` is not set | Restart `serve` with `CLAWDLINE_NEXT_WEB="$PWD/web/console/dist"` |
| `500 no_document` on `/` | `CLAWDLINE_NEXT_WEB` has no `index.html`: the console was not built, or is being rebuilt | `(cd web && npm install && npm run build)` |
| The daemon exits with status 3 | Another process holds the port | `logs/daemon.log` names it: pid, command, since when, and whether it is a Clawdline daemon. Stop that process by its pid, or start this one with another `CLAWDLINE_NEXT_PORT` |
| `the daemon did not answer` from the command line | Nothing is listening on that port, or the command line and the daemon use different ports | Start `serve`, or set the same `CLAWDLINE_NEXT_PORT` for both |
| `501 not_implemented` on a `/v1/` route | This daemon has not taken that route over yet; the body names it | Nothing is wrong with your setup |
| `502 upstream_unreachable` | `CLAWDLINE_NEXT_UPSTREAM_PORT` is set and nothing listens there | Unset it |

## Sessions

| You see | It means | Do this |
| --- | --- | --- |
| A session is missing from the list | It is not running inside tmux (or iTerm2 on a Mac), or tmux is not on `PATH` nor in `/opt/homebrew/bin`, `/usr/local/bin`, `/usr/bin` or `/opt/local/bin` | Run the agent inside tmux; `tmux -V` must work for the user running the daemon |
| The list is empty on Windows | Windows has no session discovery yet | See [platforms.md](platforms.md) |
| You can read a session but cannot type into it | The browser was opened without write permission | Run `./bin/clawdline open --send`, or see [remote-access.md](remote-access.md) for a paired device |
| A state reads as unknown, not idle | The screen or transcript could not be read, and Clawdline does not guess | Open the session's screen; the state returns when the evidence does |

## Clawdline Cloud

| You see | It means | Do this |
| --- | --- | --- |
| The Cloud line stays off | `./bin/clawdline cloud status` gives the reason. A broken Cloud setting never stops the daemon, it only keeps the line down | Fix what it names. After `cloud on`, restart `serve` |
| A paired phone can read but not act | Commands are off on this machine | `./bin/clawdline cloud commands on` ([remote-access.md](remote-access.md)) |

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
