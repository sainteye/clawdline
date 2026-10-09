# API audit tools

Three ways to count what the console asks of the daemon, for a before/after
number on a change to how often it asks. Nothing here writes to the daemon.

## The daemon's own count: `routes-delta.py`

Every call the daemon answers is counted by route shape and caller in the
`routes` block of `GET /v1/diagnostics` (`internal/transport/http/route_stats.go`):
count, 2xx/3xx/4xx/5xx, p50/p90/max latency over the last calls, open event
streams, and who asked — `device`, `local` (this machine's own token), `cloud`
(a Cloud viewer, answered in process), `task`, `anonymous`. A key is a shape
such as `GET /v1/sessions/:id/info`; no id, name or query is recorded.

The counters run from the daemon's start and are never reset, so a window is
two readings subtracted. `/v1/diagnostics` takes this machine's own token:

```sh
tok=$(cat "$CLAWDLINE_NEXT_DIR/local-token")      # the daemon's state directory
curl -sS -H "Authorization: Bearer $tok" http://127.0.0.1:7727/v1/diagnostics > before.json
# … use the console, or run measure.mjs, for as long as the window should be …
curl -sS -H "Authorization: Bearer $tok" http://127.0.0.1:7727/v1/diagnostics > after.json
python3 tools/api-audit/routes-delta.py before.json after.json
```

It refuses two readings from different daemon starts (`since` differs). The
diagnostics reads themselves are counted too, under `local`.

## A browser's view: `measure.mjs`

Headless Chrome (over the DevTools protocol) opens the console through a
device link and records every `/v1/` request in four scenarios: the list on a
phone, a session's detail on a phone, both side by side on a wide screen, and a
send (the POST is answered by the tool itself, so nothing reaches the session).

```sh
node tools/api-audit/measure.mjs out/ link.txt      # the link in a file
pbpaste | node tools/api-audit/measure.mjs out/       # or on stdin
```

The link signs the browser in, so it carries a key: it is read from a file or
stdin, never from the command line, and never printed. Each scenario writes
`out/<name>.json` with every request's route shape (ids taken out, query names
only), method, status, time, bytes and event-stream messages. `DUR` (default
180000 ms) and `SENDDUR` (90000 ms) set how long each watches; `CHROME` names the
browser binary when it is not at the macOS default. Needs Node 22 or later.

## The log's view: `parse-log.py`

```sh
python3 tools/api-audit/parse-log.py "$CLAWDLINE_NEXT_DIR/logs" [logstats.json]
```

Reads `daemon.log` and its rotated `daemon.2*.log` files in the directory given
and counts Cloud acknowledgements, Cloud reads (with p50/p90/max), Cloud
commands, failed terminal scripting calls and inventory publications, per hour
where it matters, plus the 40 commonest line shapes with ids and numbers taken
out. Prints a summary and writes the whole of it to the JSON file.
