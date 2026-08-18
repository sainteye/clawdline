# Making a project speak devstack

A working guide for whoever does this next — including an agent asked to "set this project up
like the others". [devstack.md](devstack.md) is the contract; this page is how to satisfy it, and
what goes wrong on the way.

Everything below was learned by breaking a live site with it. The **Field notes** section is the
part you cannot derive from the specification, and it is why this page exists.

---

## The shortest thing that works

Four lines in `.devstack.json` at the repository root, no new software:

```jsonc
{ "version": 1, "name": "myapp",
  "up": "make dev",
  "processes": [ { "name": "api", "port": 8002 }, { "name": "web", "port": 3001 } ] }
```

That is a real adoption. The ports get probed, the panel shows the project, and clicking a port
opens it. Stop here unless restarting one piece at a time is worth more to you than an hour.

---

## The full shape

Four files, and only the first is the contract:

| file | what it is |
|---|---|
| `.devstack.json` | **the contract** — names commands, nothing else |
| `process-compose.yaml` | the supervisor's config ([worked example](examples/process-compose.yaml)) |
| `deploy/stack-status.py` | translates the supervisor's dialect into the state document |
| `deploy/stack-restart.sh` | restarts one process and **waits until it is actually up** |

Plus adapter targets in the Makefile. The point of the split: `stack-status.py` is the only file
that knows process-compose exists. Swapping supervisors touches it and the YAML, and nothing that
reads the stack notices.

### `.devstack.json`

```jsonc
{
  "version": 1,
  "name": "myapp",
  "status":  "make stack-status",
  "up":      "make stack-up",
  "down":    "make stack-down",
  "restart": "make stack-restart P={process}",
  "logs":    "make stack-logs P={process} N={lines}",
  "attach":  "make stack-attach"
}
```

**Do not put ports or hostnames in here.** They already live in the Makefile, and a second copy
does not fail loudly — the service starts fine and talks to the wrong place, while both sides'
logs look normal.

### The Makefile adapter

```make
STACK_SOCKET  ?= $(HOME)/.local/state/devstack/myapp.sock
STACK_LOG_DIR ?= logs/stack
PROCESS_COMPOSE ?= process-compose
# Give each project its own, or cloudflared picks from 20241 upwards by whoever starts first.
TUNNEL_METRICS_PORT ?= 20251
export PATH := $(HOME)/.local/bin:$(PATH)   # process-compose is not in Homebrew core

# The YAML knows these names and nothing else. Values have one source: above.
STACK_ENV := MYAPP_API_PORT=$(API_PORT) \
             MYAPP_WEB_PORT=$(WEB_PORT) \
             MYAPP_PUBLIC_URL=$(PUBLIC_URL) \
             MYAPP_WEB_DIST_DIR=$(WEB_DIST_DIR) \
             MYAPP_TUNNEL_CONFIG=$(TUNNEL_CONFIG) \
             MYAPP_TUNNEL_METRICS_PORT=$(TUNNEL_METRICS_PORT) \
             MYAPP_STACK_SOCKET=$(STACK_SOCKET) \
             MYAPP_STACK_LOG_DIR=$(STACK_LOG_DIR) \
             PROCESS_COMPOSE=$(PROCESS_COMPOSE)

stack-up:
	@mkdir -p $(STACK_LOG_DIR) $(dir $(STACK_SOCKET))
	@$(STACK_ENV) $(PROCESS_COMPOSE) up -D -u $(STACK_SOCKET) -f process-compose.yaml

stack-down:
	@$(STACK_ENV) $(PROCESS_COMPOSE) down -u $(STACK_SOCKET) 2>/dev/null || true

stack-restart:
	@if [ -z "$(P)" ]; then \
	  $(STACK_ENV) $(PROCESS_COMPOSE) down -u $(STACK_SOCKET) 2>/dev/null || true; \
	  $(MAKE) --no-print-directory stack-up; \
	else \
	  $(STACK_ENV) bash deploy/stack-restart.sh $(P); \
	fi

stack-status:
	@$(STACK_ENV) python3 deploy/stack-status.py

stack-attach:
	@$(PROCESS_COMPOSE) attach -u $(STACK_SOCKET)

stack-logs:
	@tail -n $(or $(N),100) $(STACK_LOG_DIR)/$(or $(P),process-compose).log
```

Keep whatever guards the old single-command target had. They exist because somebody was bitten.

---

## Field notes

Ten things that cost real downtime to learn. An agent doing this work should read all of them
before writing any file.

### 1. `pkill -f` will take down someone else's server

`pkill -f "astro preview"` matches the whole command line of **every** process on the machine,
including the one currently serving production. This happened twice in one day, the second time
during the cleanup step of a test that had otherwise gone perfectly.

Kill only PIDs you read out of the supervisor's own listing, or use
`process-compose down -u <your own socket>` with the socket spelled out.

### 2. A restart does not re-run the build

`process restart web` restarts exactly that process. `depends_on` gates **startup**, not restart,
so the build it depends on does not run again — the PID changes and the bytes do not.

So `stack-restart.sh` sequences it by hand, and that sequence *is* the guarantee worth having:

```
build → check exit code → restart only if it passed → otherwise leave the running one alone
```

A failed build must not stop the process that is serving the last good one. The naive
`stop; start` ordering takes the site down and then discovers the build is broken.

### 3. A build writes over the site that is being served

`next start` does not lock its output directory, and `astro build` empties `outDir` before
writing. A second build into the same place swaps chunks under the running server: visitors get
`ChunkLoadError` or a 404 across the whole site, from a deploy nobody made.

Both the build **and** the server need the directory from the environment. Giving it to the build
only is worse than giving it to neither: the build goes to the new directory and the server keeps
serving whatever was left in the old one, silently.

Also add the whole family to `.gitignore` — `/.next-*/`, `dist-*/` — not the two names you happen
to be using. Somebody will add a third.

### 4. `-- --port` can be silently ignored

If `package.json` already pins a port, `npm run preview -- --port 4324` sends astro two `--port`
flags. Its parser checks `typeof flags.port === "number"`, an array is not a number, **both are
discarded** and it falls back to 4321. No warning. Call the binary directly
(`node_modules/.bin/astro preview --port …`) when a script pins something you need to override.

### 5. `astro preview` binds `[::1]` only

A probe against `127.0.0.1` returns nothing at all. Use `localhost`, and match whatever the tunnel
ingress uses.

### 6. Use `exec` + `curl` probes, not `http_get`

`http_get`'s `port` is an integer field, and ports must arrive by `${...}` substitution. Only
string fields are guaranteed to expand.

### 7. process-compose buffers captured output

A long-running process's log file sits at exactly 8192 bytes until the buffer fills — output
appears when the process exits, which for a server is never. Fine for a build; useless for a
watcher whose entire job is to print one important line a month. Let such a process write its own
file and give it **no** `log_location`, or the declaration truncates that file on every restart.

### 8. `make -n` still runs recipes containing `$(MAKE)`

GNU make special-cases those lines: they execute even under `--dry-run`. A `stop` target written
as `$(MAKE) stack-down` therefore takes the site down when someone runs `make -n stop` to see what
it *would* do. Spell the command out instead of delegating.

### 9. `$var）` is a different variable

In a UTF-8 locale, bash reads the bytes of a full-width bracket as part of the name, so
`"exit=$code）"` looks up `code）`. Under `set -u` the script dies there — and the error message
you carefully wrote below it never prints. **Always `${var}`.**

### 10. Unix socket paths cap at ~104 characters

Past that, macOS returns `connect: invalid argument`, which reads like a protocol error rather
than a path-length error. Keep sockets in `~/.local/state/devstack/<name>.sock`.

**Bonus:** `restarts` and `process_start_time` in process-compose's JSON do not change on a manual
restart. Do not compute uptime from them.

---

## Keeping a tunnel connected

If the stack fronts a Cloudflare tunnel, two mechanisms are needed and neither is sufficient
alone.

**A liveness probe** on `/ready` (which turns 503 at zero connections), with
`availability: restart: always`. That covers a tunnel that died and stayed dead — after a laptop
sleeps, for instance. Set `failure_threshold: 3`, not 1: Cloudflare load-balances across every
connector registered for a hostname, so a flapping tunnel sends some requests to the old one and
some to the new, and with different databases behind them that reads to a user as the application
forgetting things at random.

**A watcher on the machine's own address**, because the probe cannot see an IP change. When the
address changes, the old TCP connections are dead but the local end does not know until
retransmits time out — minutes during which `/ready` answers 200 and the site is unreachable. The
symptom is the worst kind: the address does not load, and every local service is healthy with
clean logs.

```bash
# deploy/tunnel-watch.sh — the shape of it
current () {                       # the default route's interface AND its address:
  iface=$(route -n get default 2>/dev/null | awk '/interface:/{print $2}')
  [ -n "$iface" ] || { echo ""; return; }      # Wi-Fi → Ethernet can keep the IP and
  addr=$(ipconfig getifaddr "$iface" 2>/dev/null)   # still be a new set of connections
  [ -n "$addr" ] || { echo ""; return; }
  echo "${iface}:${addr}"
}
last=$(current)
while true; do
  sleep 10
  now=$(current)
  [ -z "$now" ] && { last=""; continue; }   # no network: wait, do not reconnect into nothing
  if [ "$now" != "$last" ]; then
    "$PC" process restart tunnel -u "$SOCK" && last="$now"   # keep `last` on failure, retry
  fi
done
```

---

## Before you say it works

Run these. Every one of them has failed for somebody.

1. `process-compose up -D` returns immediately, and the daemon's **PPID is 1** — it must not
   belong to the terminal, the editor, or the agent session that started it.
2. `make stack-status` prints the state document, and every process's state is right.
3. `make stack-restart P=api` — measure it. Seconds, exit 0, and **every other PID unchanged**.
4. `make stack-restart P=web` — the build PID changed. If it did not, see field note 2.
5. **Break the build on purpose** and restart that process: exit non-zero, the error printed, and
   the serving process's **PID unchanged**. Simulate it with a fake build command in a throwaway
   config rather than breaking real source.
6. Nothing is left behind: sockets gone, ports free, and every other project on the machine still
   answering.

Test with a shadow stack — different ports, different build directories, **no tunnel** — never
against the live one.
