# Install and first run

After this page you have the Clawdline daemon running on your machine, the console open in your
browser, and one Claude Code or Codex session showing in its list. Each step ends with a check.

The longer version, with every environment variable and the reasoning behind it, is
[getting-started.md](../getting-started.md).

## Before you start

Clawdline is pre-1.0 and has no release download. You build it from source on **macOS or Linux**.
Windows builds, but cannot list or control sessions yet ([platforms.md](platforms.md)).

| You need | Check |
| --- | --- |
| Go 1.25 or newer | `go version` |
| Node.js with npm | `npm --version` |
| tmux | `tmux -V` |
| Claude Code or Codex | `claude --version` or `codex --version` |

Nothing needs an account. Clawdline Cloud is optional and off by default.

## 1. Build

```sh
git clone https://github.com/sainteye/clawdline.git ~/code/clawdline
cd ~/code/clawdline

(cd web && npm install && npm run build)
go build -o bin/clawdline ./cmd/clawdline
```

The first command builds the console into `web/console/dist`. The second builds one binary that is
both the daemon and the command line.

**Check:** `ls web/console/dist/index.html` finds the file, and `./bin/clawdline version` prints a
version.

## 2. Start the daemon

```sh
CLAWDLINE_NEXT_WEB="$PWD/web/console/dist" ./bin/clawdline serve
```

`CLAWDLINE_NEXT_WEB` tells the daemon where the console's files are. The daemon listens on
`127.0.0.1:7727` only, keeps its state in `~/.config/clawdline-next`, and writes its log to
`logs/daemon.log` inside that directory.

**Check**, from a second terminal:

```sh
./bin/clawdline doctor
curl -s -o /dev/null -w '%{http_code}\n' http://127.0.0.1:7727/    # 200 means the console is served
```

`doctor` prints the version, port, state directory and store counts. A `501` from the second
command means the daemon was started without `CLAWDLINE_NEXT_WEB`.

## 3. Open the console

```sh
./bin/clawdline open           # this browser can read every session
./bin/clawdline open --send    # this browser can also type into sessions
```

`open` creates a device for your browser and signs it in. The key travels in the address's
fragment, which the browser never sends to a server. `--print` prints the address instead of
opening a browser — use it on a headless machine, and treat the address like a password until it
has been used.

**Check:** the console loads and shows the session list. Its interface is in Traditional Chinese,
the only language it ships so far; the other pages here give each label with its meaning.

## 4. Put a session in the list

The daemon finds sessions running in tmux (and iTerm2 on a Mac). Start one:

```sh
tmux new -s work
cd ~/code/my-app
claude            # or: codex
```

**Check:** within a few seconds the session is a row in the console with its project and state.
Nothing was installed into Claude Code or Codex: the daemon reads what they already write under
`~/.claude` and `~/.codex`, and reads their screens through tmux.

To make a directory show up in the list of places to start a session before you have ever run an
agent there:

```sh
./bin/clawdline project add ~/code/my-app ~/code/another-app
./bin/clawdline project list
```

## Next

- Keep it running after you log out: the macOS app or a `systemd --user` service,
  [platforms.md](platforms.md)
- Work with sessions: [sessions.md](sessions.md)
- Reach it from a phone: [remote-access.md](remote-access.md)
- Something did not come up: [troubleshooting.md](troubleshooting.md)

## Stop or remove it

Stop the daemon with Ctrl-C in the terminal running `serve`. Everything it
keeps is in `~/.config/clawdline-next` (or `CLAWDLINE_NEXT_DIR`); removing that directory and the
checkout removes Clawdline. If you ran `clawdline skill install`, run `clawdline skill uninstall`
first ([clawdfather-and-dispatch.md](clawdfather-and-dispatch.md)).
