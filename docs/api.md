# Asking Clawdline questions over HTTP

One reading of what every terminal on this Mac is doing already exists — it is what the bar, the
menu bar and the island are drawn from. This is that reading with an HTTP surface on it, so a
browser, a phone through a tunnel, or a script in another window can ask the same questions.

**Read [`docs/remote.md`](remote.md) first if you are the one switching this on.** That page is
about what it costs; this one assumes the decision is made and you are writing the client.

Everything here was run against a live server while this page was being written, and the replies
are pasted rather than composed. Sending is switched off on that machine, so the `POST` examples
answer `write_disabled` — which is what a correct client has to handle anyway.

---

## The token

```console
$ TOKEN=$(cat ~/.config/clawdline/remote-token)
```

That file exists so that you can do exactly that. It is written when the server first starts, mode
`0600`, holding 256 random bits URL-safe-base64'd, and the app keeps only its SHA-256 — the file is
the only copy of the plaintext. The device it belongs to is called `This Mac`.

**A local script reading it is the intended path, not a loophole.** `0600` in your home directory is
the same trust boundary a Unix socket would give you: anything running as you can have it, nothing
else can. If you would have been happy connecting to `/var/run/something.sock`, you should be happy
with this, and for the same reason. What it is *not* is a defence against something already running
as you — that limit is written down plainly in
[the threat model](remote.md#what-this-defends-against-and-what-it-does-not) rather than glossed
over here.

Send it as a bearer token:

```console
$ curl -s http://127.0.0.1:7717/v1/sessions -H "Authorization: Bearer $TOKEN"
```

There is one alternative and it exists for one reason. **The browser's `EventSource` cannot set
headers**, at all, so a page holding a token in a variable cannot open the event stream with it. A
paired browser therefore gets an `HttpOnly; SameSite=Strict` cookie — set by `/v1/auth/pair/confirm`,
`/v1/auth/adopt`, `/v1/auth/password`, or by loading `/?t=<token>` — and everything that is not a
browser uses the header. If you are writing a script, use the header and ignore the cookie entirely.

---

## Rules the protocol keeps

These do not change without the number in the path changing.

- **`/v1` is in the path.** `GET /v1/health` also reports `"protocol": 1`, but that field is for a
  person asking *what am I talking to*; a client that speaks `/v1` never has to look at it.
- **JSON, and one error envelope.** Every failure is
  `{"error":{"code":…,"message":…,"request_id":…}}` with a status to match. `code` is the part you
  are allowed to branch on. `message` is for a person and may be reworded. `request_id` is a fresh
  UUID per reply, for matching a complaint to a log line.
- **Every time is an integer of Unix seconds.** No milliseconds, no strings, no time zones.
- **Every id is a string**, including the ones that look like numbers, and including the ones that
  look like UUIDs. Do not parse them.
- **Capabilities are explicit** — `read`, `send`, `admin` — rather than a level. Reading discloses a
  repository name; sending is remote code execution. They are not two points on one scale.
- **`Idempotency-Key` is required on every call that changes a session**, and required rather than
  merely honoured. Phones change networks mid-request and clients retry; typing the same instruction
  into somebody's agent twice is not something that can be taken back. (The `/v1/auth/*` routes are
  the exception, and the reason is [below](#post-v1auth).)

One thing about the wire itself, because a client author will meet it on the first connection:
**every reply closes it.** `Connection: close`, no keep-alive, one request per socket — the event
stream being the one that stays open, which is its whole job.

---

## The routes

| | route | needs | capability |
|---|---|---|---|
| `GET` | `/v1/health` | — | — |
| `GET` | `/v1/sessions` | token | `read` |
| `POST` | `/v1/sessions/refresh` | token | `read` |
| `GET` | `/v1/sessions/:id` | token | `read` |
| `GET` | `/v1/sessions/:id/transcript` | token | `read` |
| `GET` | `/v1/artifacts/images/:artifactId` | token | `read` |
| `GET` | `/v1/sessions/:id/agents/:agentId` | token | `read` |
| `GET` | `/v1/sessions/:id/shells/:shellId` | token | `read` |
| `GET` | `/v1/sessions/:id/links` | token | `read` |
| `GET` | `/v1/sessions/:id/info` | token | `read` |
| `GET` | `/v1/sessions/:id/skills` | token | `read` |
| `GET` | `/v1/sessions/:id/git` | token | `read` |
| `GET` | `/v1/projects` | token | `read` |
| `GET` | `/v1/places` | token | `read` |
| `GET` | `/v1/places/:id/sessions` | token | `read` |
| `GET` | `/v1/places/:id/sessions/:assistant` | token | `read` |
| `GET` | `/v1/events` | token | `read` |
| `POST` | `/v1/places/:id/start` | token + key | `send` **and** the write switch |
| `POST` | `/v1/places/:id/start/:assistant` | token + key | `send` **and** the write switch |
| `POST` | `/v1/places/:id/start/:assistant/:model` | token + key | `send` **and** the write switch |
| `POST` | `/v1/places/:id/resume/:session` | token + key | `send` **and** the write switch |
| `POST` | `/v1/places/:id/resume/:assistant/:session` | token + key | `send` **and** the write switch |
| `POST` | `/v1/sessions/:id/send` | token + key | `send` **and** the write switch |
| `POST` | `/v1/sessions/:id/title` | token + key | `send` **and** the write switch |
| `POST` | `/v1/sessions/:id/key` | token + key | `send` **and** the write switch |
| `POST` | `/v1/sessions/:id/focus` | token + key | `send` **and** the write switch |
| `POST` | `/v1/sessions/:id/end` | token + key | `send` **and** the write switch |
| `POST` | `/v1/sessions/:id/shells/:shellId/kill` | token + key | `send` **and** the write switch |
| `POST` | `/v1/voice` | token + key | `send` **and** the write switch |
| `POST` | `/v1/auth/pair` | — | — |
| `POST` | `/v1/auth/pair/confirm` | — | — |
| `POST` | `/v1/auth/password` | — | — |
| `POST` | `/v1/auth/adopt` | a token in the body | — |
| `POST` | `/v1/auth/logout` | — | — |
| `POST` | `/v1/orchestrator/handoffs` | orchestrator token | — |
| `POST` | `/v1/orchestrator/tasks` | orchestrator token | — |
| `POST` | `/v1/orchestrator/detached-tasks` | orchestrator token | — |
| `POST` | `/v1/orchestrator/root-assignments` | orchestrator token + key | — |
| `GET` | `/v1/orchestrator/root-assignments` | orchestrator token | — |
| `GET` | `/v1/orchestrator/root-assignments/:id` | orchestrator token | — |
| `POST` | `/v1/orchestrator/notify` | orchestrator token | — |
| `GET` | `/v1/orchestrator/tasks` | orchestrator token, **or** token | `read` |
| `GET` | `/v1/orchestrator/tasks/:id` | orchestrator token, **or** token | `read` |
| `POST` | `/v1/orchestrator/maintenance/restart` | orchestrator token | — |
| `GET` | `/v1/orchestrator/maintenance/restart` | orchestrator token | — |
| `DELETE` | `/v1/orchestrator/maintenance/restart` | orchestrator token | — |
| `GET` | `/v1/orchestrator/completions` | orchestrator token | — |
| `POST` | `/v1/orchestrator/completions/reconcile` | orchestrator token | — |
| `POST` | `/v1/orchestrator/tasks/:id/notify` | that task's secret | — |
| `POST` | `/v1/orchestrator/tasks/:id/complete` | that task's secret | — |
| `POST` | `/v1/orchestrator/tasks/:id/completion/ack` | orchestrator token | — |
| `POST` | `/v1/orchestrator/tasks/:id/landing` | task secret **or** orchestrator token for pending/abandoned; **orchestrator token only for landed** | — |
| `POST` | `/v1/orchestrator/tasks/:id/progress` | that task's secret | — |
| `GET` | `/v1/orchestrator/tasks/:id/inflight` | that task's secret | — |
| `POST` | `/v1/orchestrator/tasks/:id/respawn` | orchestrator token | — |
| `POST` | `/v1/orchestrator/tasks/:id/cancel` | orchestrator token, **or** token + key | `send` **and** the write switch |
| `GET` | `/v1/orchestrator/assistants` | orchestrator token, **or** token | `read` |
| `GET` | `/v1/orchestrator/landings` | orchestrator token, **or** token | `read` |
| `GET` | `/v1/orchestrator/storage` | orchestrator token, **or** token | `read` |
| `GET` | `/v1/orchestrator/inflight` | orchestrator token, **or** token | `read` |
| `GET` | `/v1/orchestrator/usage` | orchestrator token, **or** token | `read` |
| `GET` | `/v1/orchestrator/usage.csv` | orchestrator token, **or** token | `read` |
| `GET` | `/v1/orchestrator/usage/analytics` | orchestrator token, **or** token | `read` |
| `GET` | `/v1/orchestrator/usage/analytics.csv` | orchestrator token, **or** token | `read` |
| `GET` | `/v1/orchestrator/usage/analytics.json` | orchestrator token, **or** token | `read` |
| `GET` | `/v1/orchestrator/sessions` | orchestrator token | — |
| `GET` | `/v1/orchestrator/whoami` | orchestrator token | — |
| `POST` | `/v1/orchestrator/messages` | orchestrator token + key | — |
| `POST` | `/v1/orchestrator/sessions/:id/complete` | orchestrator token | — |
| `POST` | `/v1/orchestrator/sessions/:id/state` | orchestrator token | — |
| `POST` | `/v1/orchestrator/sessions/:id/closure` | orchestrator token | — |
| `POST` | `/v1/orchestrator/coordinator/register` | orchestrator token | — |
| `POST` | `/v1/orchestrator/coordinator/rebind` | orchestrator token | — |
| `GET` | `/v1/orchestrator/coordinator` | orchestrator token | — |
| `GET` | `/v1/orchestrator/coordinator/bearings` | orchestrator token, **or** token | `read` |
| `GET` | `/v1/orchestrator/waits` | orchestrator token, **or** token | `read` |
| `POST` | `/v1/orchestrator/waits` | orchestrator token | — |
| `POST` | `/v1/orchestrator/waits/:id/release` | orchestrator token | — |
| `POST` | `/v1/orchestrator/waits/:id/cancel` | orchestrator token | — |
| `GET` | `/v1/orchestrator/schedules` | orchestrator token, **or** token | `read` |
| `GET` | `/v1/orchestrator/schedules/:id` | orchestrator token, **or** token | `read` |
| `POST` | `/v1/orchestrator/schedules` | token + key | `send` **and** the write switch |
| `PATCH` | `/v1/orchestrator/schedules/:id` | token + key | `send` **and** the write switch |
| `DELETE` | `/v1/orchestrator/schedules/:id` | token + key | `send` **and** the write switch |
| `POST` | `/v1/orchestrator/schedules/:id/run` | orchestrator token | — |
| `GET` | `/`, `/index.html`, `/manifest.webmanifest` | — | — |
| `GET` | `/favicon.ico`, `/icon-<size>.png` | — | — |

Every approved device has `read`. `send` is granted to all of them together by
Settings → Remote → **Let a paired device write into a session**, and taken back from all of them together — there is
no per-device grant. `admin` is defined, and the local `This Mac` device holds it, and **no route
requires it today**; it is there so that adding one later does not mean handing out a capability
nobody asked for.

**The `/v1/orchestrator/*` rows name two credentials that are not device tokens, and neither is a
capability.** The *orchestrator token* is `~/.config/clawdline/orchestrator-token`, mode `0600`,
minted alongside the one above and **never served over HTTP** — it proves "a process running as
this user on this Mac", which is a different claim from "a device somebody paired", and it is the
only thing that may dispatch or hand a line of work to a new tab. No device token does either: not
with `send`, not with `admin`, not over a tunnel. The *task secret* is narrower still — one task,
made by whoever dispatched it. It can report that task finished and, within the narrow limits
below, send that task's timely content notification. What each one can and cannot do is
[`docs/orchestrator.md`](orchestrator.md#what-it-costs-before-anything-else); this page is the
wire.

### `GET /v1/health`

The route to ask before you have anything to ask with. A client has to be able to find out what it
is talking to, and whether it is allowed in, before it can act on either.

```console
$ curl -s http://127.0.0.1:7717/v1/health
{"ok":true,"version":"0.5.0","build":1787096354,"instance":"9af84fc1-…","protocol":1,"write":false,"auth":false,"password":false,"authed":false}
```

| field | |
|---|---|
| `version` | the app's, for a person |
| `build` | which build, as opposed to which release — the executable's modification time. `version` is the same string for every build of a release, so a long-lived page watching only that could never tell it had fallen behind. Compare it to what you saw first; if it moved, the app was rebuilt under you |
| `instance` | one UUID for this listener process. Health and the SSE `hello` carry the same value; a replacement changes it even when release and build strings do not |
| `protocol` | this document's; bumped when a client would have to change |
| `write` | is the second switch on — **draw the UI from this**, because saying "you may not" once is kinder than a button that fails when pressed |
| `auth` | has anybody paired a device or set a password. The local token does not count |
| `password` | is there a password to offer at all — separate from `auth`, so a page can decide whether to draw that door rather than offering it blind and letting somebody learn from a 401 that it was never set |
| `authed` | did *this* request carry a credential that works |

### `GET /v1/sessions`

Everything the bar knows, as of one reading.

```console
$ curl -s http://127.0.0.1:7717/v1/sessions -H "Authorization: Bearer $TOKEN" \
    | jq '{at, scan, sessions: [.sessions[] | {id, label, state, work_state, cwd}]}'
{
  "at": 1787049596,
  "scan": {"epoch":"65d8fbbc-…","generation":42,"complete":true,"emptyAuthoritative":false},
  "sessions": [
    {
      "id": "35D87610-E7F4-4A9A-95A0-11947CF5115C",
      "label": "設計基本問題和股票相關聊天內容",
      "state": "idle",
      "work_state": "unknown",
      "cwd": "/Users/you/code/cairn"
    },
    {
      "id": "B3ACDE0D-DE72-4E58-A99A-AB845A539C90",
      "label": "評估動態島實現機制",
      "state": "working",
      "work_state": "working",
      "cwd": "/Users/you/code/clawdline"
    },
    {
      "id": "27439AEE-3736-4AC3-BF80-CE63280B5CCD",
      "label": "IG 設定指引改進",
      "state": "idle",
      "work_state": "milestone_complete",
      "cwd": "/Users/you/code/atrium"
    }
  ]
}
```

Five fields per session are picked out there so the reply fits on this page; the whole object is
[below](#the-session-object). `at` is when the reply was built, not when the reading was taken.
`scan.generation` orders published session content after terminal/process scans are reconciled;
it is not a receipt for a caller's requested scan. `scan.epoch` is one UUID for the SessionWatch
process lifetime, so generation 42 after replacement cannot be mistaken for generation 42 before
it. `scan.completed.sequence` advances once when
each inventory read actually finishes, including an unchanged or incomplete read, and
`scan.completed.complete` reports whether every source in that exact read was usable.
`scan.complete` describes the currently published inventory. `scan.emptyAuthoritative` is narrower:
it is true only when an empty list came from a complete inventory or when the complete process
list independently proved every previously known assistant tty had exited. A second HTTP read of
the same generation is the same observation, not confirmation that an empty list is real.

**This route is the paired-device API and it does not accept the orchestrator token.** A caller
holding `~/.config/clawdline/orchestrator-token` and nothing else gets `401 unauthorized` here, so
an agent looking for the session ids a coordination wait names wants
[`GET /v1/orchestrator/sessions`](#get-v1orchestratorsessions) instead.

### `POST /v1/sessions/refresh`

Ask the Mac to take a new terminal/process reading. This is the local-only action behind the retry
button shown when a session is known to be waiting but its question was not captured. It is
deliberately distinct from the web client's ordinary snapshot/reconnect refresh and is not exposed
by the Cloud transport.
It is read-level: a token with `read` is enough, the remote-write switch is not consulted, and no
`Idempotency-Key` is required because nothing is typed into or changed in a session.

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/sessions/refresh \
    -H "Authorization: Bearer $TOKEN"
{"accepted":true,"coalesced":false,"ok":true,"scan":{"completed":{"sequence":42}},"state":"accepted","throttled":false}
```

The response acknowledges admission; it is not the completed reading.
`scan.completed.sequence` is the coherent completion baseline immediately before this request.
Exactly one of `accepted`, `coalesced`, and `throttled` is true, matching `state`: `accepted`
started a reading, `coalesced` joined an existing read or already-scheduled debt, and `throttled`
scheduled one follow-up behind the completed-read floor. Repeated requests in the same floor buy at
most that one follow-up, so a read-only device cannot keep terminal automation at 100% duty cycle.

The client keeps the retry busy until a `sessions` event carries a safe-integer
`scan.completed.sequence` greater than its acknowledged baseline. It reports success only when
that receipt's `complete` is true; an incomplete/failed scan is visible and immediately retryable.
An unrelated session-content generation cannot complete the operation. Lower, unsafe or
unversioned completion evidence cannot overwrite a newer receipt, and `scan.generation` continues
to order session frames independently. A bounded client timeout also makes the action retryable
and reports a request failure rather than claiming that the event stream was empty.

### `GET /v1/sessions/:id`

The same object, alone, under `session`. `404 not_found` if that id is not currently on screen —
which includes a session that has since been closed, since ids come from the terminal and are not
kept after the tab is gone.

```console
$ curl -s http://127.0.0.1:7717/v1/sessions/27439AEE-3736-4AC3-BF80-CE63280B5CCD \
    -H "Authorization: Bearer $TOKEN"
{"session":{"id":"27439AEE-3736-4AC3-BF80-CE63280B5CCD","isClaude":true,"state":"idle","work_state":"unknown","icon":{"accent":"#5CBBA1","cells":[["#2F6B5E","#EEF6F4","#EEF6F4","#EEF6F4","#EEF6F4","#EEF6F4","#2F6B5E"],["#2F6B5E","#EEF6F4","#2F6B5E","#2F6B5E","#2F6B5E","#EEF6F4","#2F6B5E"],["#2F6B5E","#EEF6F4","#2F6B5E","#EEF6F4","#2F6B5E","#EEF6F4","#2F6B5E"],["#2F6B5E","#EEF6F4","#2F6B5E","#2F6B5E","#2F6B5E","#EEF6F4","#2F6B5E"]]},"tty":"ttys006","backend":"iterm","label":"IG 設定指引改進","sessionId":"841cbb8d-58b1-4765-9a71-bcdba19bcfef","cwd":"/Users/you/code/atrium"}}
```

Key order is not stable between replies — it comes out of a dictionary. Read by name.

### `GET /v1/sessions/:id/transcript?limit=200`

What was actually said, out of the assistant's own record — Claude Code's
`~/.claude/projects/…` transcript, or Codex's `~/.codex/sessions/YYYY/MM/DD/rollout-….jsonl`.
Which of the two is decided by what is running in that session and is not a parameter; the shape
that comes back is the same either way, which is the point of reading them at all.

```console
$ curl -s "http://127.0.0.1:7717/v1/sessions/B3ACDE0D-DE72-4E58-A99A-AB845A539C90/transcript?limit=1" \
    -H "Authorization: Bearer $TOKEN"
{"signature":"38603256-1787049580","entries":[{"text":"請幫我在網頁加入 favicon","at":1787049580,"role":"user"}]}
```

`limit` is the number of entries from the **end**, clamped to 1…1000, and anything unparseable
falls back to 200 — `?limit=0` gives one entry and `?limit=9999` gives a thousand.

This route stands in the same queue as [`/v1/sessions/:id/info`](#get-v1sessionsidinfo) but is
**never** refused by its limit, deliberately: it is the cheapest of the three, a page refetches it
about once a second while a session works, and a refusal there would replace a conversation
somebody is reading with an error. So a client polling this does not have to handle `429 busy` —
it can still be delayed behind slower readings, but it will be answered.

`signature` is the transcript file's size and modification time joined by a dash. It is a cheap way
to ask *would fetching this again tell me anything new*, and nothing else — do not try to read
meaning into either half.

**A session with no transcript is not an error.** An empty `entries` and an empty `signature` come
back with `200`, because a session that has not spoken yet and a session that could not be found are
different things and only the second is a `404`. A shell that is not running an assistant answers
the same way, and so does a session whose record could not be matched to it.

Codex file-edit entries carry a `fileChanges` array in addition to their summary `text`. Each row
always has `path` and `kind`; `unifiedDiff`, `content`, and `movePath` are present only when Codex
recorded them. Clients that understand the field can render an inline patch, while older clients
continue to show the summary text.

For a strictly decoded version-2 session message, its `role: "message"` entry also carries an
`artifacts` array. Each row contains only `id`, `media_type` (`image/png`), `byte_count`, `width`,
`height`, and absolute Unix-seconds `expires_at`. There is no source path, filename, URL or raw
image data in the transcript response.

### `GET /v1/artifacts/images/:artifactId`

Read the bytes for one artifact reference from the same authenticated origin as the transcript.
The id is the opaque lowercase UUID published in a message entry; arbitrary path segments and
percent-encoded path tricks are not filenames and return the unknown-id response.

| result | status | response |
|---|---:|---|
| live | 200 | PNG bytes, `Content-Type: image/png`, `Cache-Control: private, no-store` |
| known but expired, deleted or byte-missing | 410 | `artifact_expired` error envelope |
| unknown or malformed id | 404 | `artifact_not_found` error envelope |

Authentication is the ordinary `read` token/cookie gate and the existing Host / cross-site
checks still run. There is no public artifact URL and no remote fetch proxy.

### `GET /v1/sessions/:id/agents/:agentId?limit=200`

One of that session's background agents: the row `GET /v1/sessions` already showed for it, and the
conversation behind that row.

An agent has no screen. Claude Code gives each one its own transcript beside the session's —
`~/.claude/projects/…/<session>/subagents/agent-<id>.jsonl` — and nothing else in this API can
reach it, which is why the session list can say *three agents are out* and could never say what
any of them did.

```console
$ curl -s "http://127.0.0.1:7717/v1/sessions/1FECB67D-9344-4728-8F09-5844C3BE658E/agents/a44b12139eff09dd4?limit=1" \
    -H "Authorization: Bearer $TOKEN"
{"agent":{"id":"a44b12139eff09dd4","what":"Map the web page sections","type":"general-purpose",
          "state":"done","depth":1,"at":1787510342,"tokens":80649,"tools":9,"seconds":70.5},
 "signature":"24101-1787510342",
 "entries":[{"role":"assistant","text":"…","at":1787510342}]}
```

`entries` and `signature` are exactly what `…/transcript` returns and mean the same things —
same shape, same `limit`, same "ask again only if the signature moved".

`agent` is the same object the session carries in its own `agents` array, so a client that
followed a link has the description, the state and the cost without a second request. It is
**absent** when the agent's files are on disk but it has dropped out of the session's list —
about half an hour after it last wrote anything. The transcript still reads.

**Claude Code only.** A Codex session has no `subagents` directory, so every agent id under one
is a `404` — the same answer as an id that was never this session's, and as an id shaped like a
path. The id is checked before it is used to name a file.

### `GET /v1/sessions/:id/shells/:shellId?bytes=65536`

One of that session's background commands: the row `GET /v1/sessions` already showed for it, and
what it has printed.

**Text, not entries.** Everything else here that can be opened is a conversation and has turns;
this is a command's stdout, so it comes back as the bytes in the order they were written — the
tail of the file Claude Code names in the tool result, which is the same file `/bashes` reads on
the Mac. `bytes` is how much of the end to send: 64 KB by default, 1 KB to 1 MB.

```console
$ curl -s "http://127.0.0.1:7717/v1/sessions/09BB6254-A0C9-43D0-B7ED-0A67E6B293FD/shells/b9b2ki73h" \
    -H "Authorization: Bearer $TOKEN"
{"shell":{"id":"b9b2ki73h","at":1787645469,"doing":"[3/100] Compiling something.rs"},
 "text":"[1/100] Compiling something.rs\n[2/100] Compiling something.rs\n",
 "ended":false,"at":1787645478,"signature":"186-1787645478"}
```

`ended` is the fact the bytes do not carry, and the one worth polling for: the session list only
ever carries commands that are still running, so a client watching one land has nothing else that
would tell it. `signature` moves when the file does — ask again, and redraw only when it changed.

`shell` is the same object the session carries in its own `shells` array, so a client that
followed a link has the command line and the description without a second request. It is
**absent** once the command has ended and dropped off that list, which is also when a client
should stop asking.

**Claude Code only**, and an id shaped like a path is a `404` — the same answer as an id that was
never this session's. The id is checked before it is used to name a file.

### `GET /v1/projects`

The directories this Mac already knows it works in, taken from the icon registry that
[`docs/project-status.md`](project-status.md) describes. This is what a "start a session in…" menu
is built from.

```console
$ curl -s http://127.0.0.1:7717/v1/projects -H "Authorization: Bearer $TOKEN" | jq -c '.projects[1]'
{"label":"notebook","icon":{"accent":"#B9CFDF","cells":[["#7DA4BF","#7DA4BF","#7DA4BF","#7DA4BF","#7DA4BF"],["#B9CFDF","#B9CFDF","#B9CFDF","#B9CFDF","#B9CFDF"],["#B9CFDF",null,"#B9CFDF",null,"#B9CFDF"],["#7DA4BF",null,"#7DA4BF",null,"#7DA4BF"]]},"path":"/Users/you/code/astro"}
```

`path` and `label` always; `icon` only when the registry has one. Sorted by path.

### `GET /v1/sessions/:id/links`

Every address this project has, gathered from the files other tools already write.

```console
$ curl -s -H "Authorization: Bearer $TOKEN" .../v1/sessions/$ID/links
{"links":[
  {"label":"ci","url":"https://github.com/you/repo/actions/runs/123","kind":"deploy","state":"running","local":false,
   "startedAt":1787396170,"typicalSeconds":800},
  {"label":"web","url":"http://127.0.0.1:3000","kind":"server","state":"ok","local":true},
  {"label":"backlog","url":"/v1/sessions/27439AEE-…/artifacts/backlog","kind":"artifact","state":"","local":false},
  {"label":"milestone","url":"/v1/sessions/27439AEE-…/artifacts/milestone","kind":"artifact","state":"running","status":"3/8","local":false,"why":"2 waiting on you"}
]}
```

| field | |
|---|---|
| `kind` | `site` · `deploy` · `server` · `artifact` — for choosing an icon |
| `state` | that thing's own health where it has one, else empty. **Worth drawing**: a server that is down is worth knowing before it is tapped |
| `local` | the address only resolves on the Mac's own network. A phone on mobile data cannot open it, and saying so beats a link that times out |
| `startedAt` · `typicalSeconds` | present for a running deploy, in seconds; elapsed against typical is the estimated progress shown in the compact status line |

**A route rather than a field on the session.** The session list goes out on the event stream
whenever anything moves, and gathering these costs a `git` invocation plus a handful of file
reads per project — free when a menu is opened, a subprocess per session per second on the stream.

Nothing here is invented: the health endpoint comes from the icon registry, the run from the
deploy status, the servers from the project's own `status` command, and the backlog or milestone
page from whatever produced it. An untrusted dev stack stays silent rather than being probed.
Project artifacts use the authenticated routes below, so a paired phone can open them without the
server disclosing an absolute filesystem path.

### `GET /v1/sessions/:id/artifacts/:kind`

`kind` is exactly `backlog` or `milestone`. The server resolves the corresponding path from that
project's status file, then serves it only when it is a regular `.html` file inside the session's
working directory. Symlink escapes, caller-supplied paths, non-HTML files and files over 2 MiB are
refused. Responses are `private, no-store`, `noindex`, and carry a CSP that disables scripts,
forms, embedding and external resources.

### `GET /v1/sessions/:id/info`

One card about a session and the assistant behind it — what the status line at the bottom of a
Claude Code terminal says, for somebody who is not at that terminal.

**This is the expensive one**, and it is answered off the queue every other request is read on:
gathering it runs `lsof`, reads the whole transcript, asks iTerm2 for the visible screen over an
Apple event, and shells out to `git status`. Eight of these and `/v1/places` may be in hand at
once; the ninth is `429 busy`. That number is a patience bound rather than a promise about how
long the wait is — a single card can sit inside a fifteen-second Apple event timeout, and while
it does, the eighth in line waits minutes.

```console
$ curl -s -H "Authorization: Bearer $TOKEN" .../v1/sessions/$ID/info
{"info":{
  "session":{"id":"27439AEE-…","assistant":"claude","sessionId":"841cbb8d-…","model":"claude-fable-5",
             "cwd":"/Users/you/code/atrium","startedAt":1787390000,"seconds":5580},
  "permission":{"current":"auto","options":["auto","manual","acceptEdits","plan"]},
  "usage":{"input":4821,"output":38210,"cacheRead":2984120,"cacheWrite":214880,"total":3242031,
           "model":"claude-fable-5","costUsd":7.38},
  "limits":{"windows":[{"name":"5h","usedPercent":100,"resetsAt":1787417400,"hit":true}],"at":1787416917},
  "files":{"branch":"main","head":"d5c61e9f91c46a77","ahead":2,"behind":0,
           "staged":1,"unstaged":4,"untracked":2,"conflict":0},
  "deploy":[{"label":"ci","url":"https://github.com/you/repo/actions/runs/123","kind":"deploy","state":"running","local":false,
              "startedAt":1787396170,"typicalSeconds":800}]
}}
```

| field | |
|---|---|
| `session` | `id` and `assistant` always; `sessionId` when the current process can be bound to its exact Claude transcript or Codex rollout; `model` when a transcript has named one — the **last** model the transcript names, so a session that switched mid-way shows what it is on now; `cwd`, `startedAt` and `seconds` (its age, as of this answer) when the process could be found |
| `permission` | Claude Code's current permission mode and the Shift-Tab cycle order. `current` is `auto`, `manual`, `acceptEdits`, `plan`, or `unknown`; `manual` specifically means the screen was readable and showed no mode line, while `unknown` means the screen capture was absent or empty. **Absent for Codex sessions**, which do not have this mode cycle |
| `usage` | the transcript's token totals — `input`, `output`, `cacheRead`, `cacheWrite`, `total` — with `model` and, for Claude, `costUsd`. Claude Code's own `total_cost_usd` replaces the list-price estimate when its session cache has it — **on this route only**: the task records under `/v1/orchestrator/tasks` still publish the estimate, so the same session can be quoted two different figures. **Absent** when no transcript has been found, which is not the same as zero |
| `context` | the current conversation against its model window: `usedPercent`, plus `usedTokens`, plus `windowTokens` **only when the window is a stated fact rather than a guess**. Codex records all three together; Claude combines the last parent assistant turn's transcript usage with its cached window, falling back to a model-window table when that cache is absent — and a table row is a guess, so it moves the percentage but never appears as `windowTokens`. **Absent** when no source supplies a window, and when one does but neither the newest parent turn nor the cache supplies a used figure. This is per-turn context, not cumulative `usage` |
| `limits` | `windows`: each `name` (`5h`, `7d` — the status line's names), `usedPercent`, `resetsAt`, and `hit` when the provider refused the last request on it; `at` is when the record it came from was written. **An empty `windows` means nobody said**, and a client must draw that as unknown rather than as 0% |
| `files` | the working tree **counted**, not listed: `branch` (empty when detached), `head`, `ahead`, `behind`, `staged`, `unstaged`, `untracked`, `conflict`. A partially added file is under both `staged` and `unstaged`, as `git status` lists it. **Absent** when the directory is not a repository or `git` did not answer in time — and those are the same answer on purpose, because a card that said *clean* about a tree it could not read would be wrong in the direction that matters. The files themselves are `/git` |
| `deploy` | the `deploy` and `ci` rows of `/links`, unchanged, so a `state` means here what it means there |

**Where the plan numbers come from.** Codex writes `rate_limits.primary` — a percentage, a window
length and a reset — onto every `token_count` event of its rollout, and the newest one is the
answer. Claude Code hands its status line the same kind of numbers on stdin and writes none of
them into the transcript; what does reach the file is a `quotaLimits` block on the turn a window
ran out. So a Claude session's windows are read from two places and laid over each other: the
file [claude-bestiary](https://github.com/sainteye/claude-bestiary)'s `statusline.py` keeps for
exactly this reader — `rate-limits.json` in its cache directory, the last `rate_limits` it was
handed, with `at` — and, over that, a refusal in the transcript that has not yet reset (`hit`,
100%). A cached window whose reset has passed is dropped rather than shown. Without that status
line installed, or with no Claude Code session open to keep it current, the file goes stale and
the windows go back to *unknown* — which is the word for it.

**Context is not token spend.** Codex's same `token_count` event carries
`last_token_usage.total_tokens` and `model_context_window`; their ratio is the optional `context`
object. **Claude Code states its window nowhere a reader can reach** — not in the transcript, not
in `~/.claude/sessions/<pid>.json`; it hands `context_window` to the status-line command on stdin
and to nothing else. So the window comes from a file that command writes, `session-<session-id>.json`
in the same directory as `rate-limits.json` above (`status_dir`, defaulting to
`~/.claude/statusline-cache`), shaped as
`{"context_window":{"context_window_size":…,"total_input_tokens":…,"used_percentage":…},
"cost":{"total_cost_usd":…}}`.

That stable window size is combined with the last non-sidechain assistant turn's current
input/cache usage — the size never moves during a session, while the transcript is always
current, so the reading is live and needs no staleness rule. `<synthetic>` turns are stepped
over: Claude Code writes one when the provider refuses, with an all-zero `usage` that would
otherwise read as an empty context at the exact moment the window is full. Before the first
assistant turn the cached totals stand alone. Without the file, known Claude models fall back to a
prefix-matched window table and unknown models stay absent — that table is a guess and is
published as one, moving `usedPercent` without ever appearing as `windowTokens`. The same file's
`total_cost_usd`, when present, is preferred over the computed `usage` price. Cumulative token
totals remain under `usage`; the compact row shows `context.usedPercent`, because that is what
determines whether this conversation is about to compact.

**A route rather than a field on the session**, for the reason `/links` gives and one more: on
top of that route's `git`, this one reads the transcript, which can be fifty megabytes. Free when
a card is opened; not something to do on every beat of the stream. Everything here is read and
nothing is written — the `git` runs with `GIT_OPTIONAL_LOCKS=0` and a deadline, and nothing is
asked of GitHub that `/links` did not already ask.

### `GET /v1/orchestrator/assistants`

What this Mac can say about each assistant's own **account-level** quota — a machine-level fact,
not a per-session one. Every Claude Code or Codex session on this Mac shares the same five-hour or
weekly window, so this answers "does claude or codex have anything left" once, rather than by
opening a session and asking `/v1/sessions/:id/info`, which needs one already running and is the
expensive route besides.

**Deliberately not on that queue.** Both providers are one read of at most a handful of small
local files, 5-second cached — cheap enough that `Orchestrator.dispatch()` calls it synchronously
at its own gate, on every dispatch. See
[docs/orchestrator.md's "An assistant with no quota left"](orchestrator.md#an-assistant-with-no-quota-left)
for that gate and `409 assistant_exhausted`.

```console
$ curl -s -H "Authorization: Bearer $TOKEN" .../v1/orchestrator/assistants
{
  "at": 1787745138,
  "assistants": [
    {"id":"claude","label":"Claude Code","installed":true,"logged_in":null,"plan":null,
     "availability":"ok","source":"observed","observed_at":1787745100,"age_seconds":38,
     "stale":false,"resets_at":null,"detail":"5h 4%, 7d 69%",
     "windows":[{"name":"5h","usedPercent":4,"resetsAt":1787761200,"hit":false},
                {"name":"7d","usedPercent":69,"resetsAt":1787860800,"hit":false}]},
    {"id":"codex","label":"Codex","installed":true,"logged_in":null,"plan":null,
     "availability":"exhausted","source":"observed","observed_at":1787743430,"age_seconds":1708,
     "stale":false,"resets_at":1788272000,
     "detail":"7d 100%; resets in 6d2h; premium credits exhausted",
     "windows":[{"name":"7d","usedPercent":100,"resetsAt":1788272000,"hit":true}]}
  ]
}
```

| field | |
|---|---|
| `availability` | one of `ok` · `low` · `exhausted` · `unknown` — **always present, and the one field a client must read.** No remaining-percentage number is ever computed here: both providers answer "what the account last said", which can be minutes or days old, and a number would claim a precision neither can promise |
| `source` | `observed` (a file the provider itself wrote), `probed` (its own identity command, login state only — see below), or `self_reported`, which nothing in v1 produces |
| `observed_at` | the signal's **own** time, not when this request read it. `null` exactly when `availability` is `unknown` and nothing usable has been seen |
| `age_seconds` | `at` minus `observed_at`, an integer number of seconds — the identical formula `409 workspace_busy`'s `age_seconds` and every `claims_overlap` warning already use. `null` alongside a `null` `observed_at` |
| `stale` | whether this reading is old enough that a fresher one should be preferred once available, without being old enough to discard. See the aging rule below |
| `resets_at` | the tightest live window's reset, when known |
| `detail` | one sentence a client with no UI of its own can print as-is |
| `windows` | the exact shape `/v1/sessions/:id/info`'s `limits.windows` already uses — `name`, `usedPercent`, `resetsAt`, `hit`. **An empty array means nobody said**, drawn as unknown rather than 0%, same rule as there |
| `logged_in`, `plan` | reserved fields. Always `null` in this version — the identity probe that would run `claude auth status`/`codex login status` and fill them in is not implemented in this build, only the pure parsers for their output (`AssistantQuota.parseClaudeAuthStatus`/`parseCodexLoginStatus`) with nothing left to call them. Once a probe exists, `logged_in` would be a bool and `plan` a string such as `"max"` or `"prolite"`, or still `null` if the provider did not say |

**Aging is not one TTL.** Because a provider's own `used_percent` only rises within one window, an
old reading is a floor rather than an estimate, so each `availability` ages differently:
`exhausted` does not expire on its own — it holds until `resets_at` itself has passed, then becomes
`unknown` rather than `ok`, because the new window has no reading of its own yet. `low` keeps
reading `low` however old, but is marked `stale`. `ok` decays to `unknown` past its own staleness
window (5% of the tightest window's length, clamped 15 minutes–6 hours — a convention, not a
measurement, and documented as such in the source). `unknown` is already the floor.

**Codex's one extra rule.** Once its primary bucket is full, the next `token_count` record often
answers from an unnamed credits bucket instead — `rate_limits.limit_id` turns from `"codex"` to
`"premium"`, `primary`/`secondary` both `null`. That shape alone proves nothing (a fresh rollout
with no usage yet looks the same); paired with a last named window that was already at 95% or
higher, it is `exhausted` too, and `detail` says so in words (`"; premium credits exhausted"`)
rather than only in the number.

**The two biggest honest gaps.** Claude Code has no free way to answer this before the first
window is hit — no CLI subcommand, and the file this route reads
(`~/.claude/statusline-cache/rate-limits.json`) exists only on a Mac running
[claude-bestiary](https://github.com/sainteye/claude-bestiary)'s status line, and only once a
session has actually rendered it; see [`docs/compatibility.md`](compatibility.md). And neither
provider can say how much of the *current* task a task would spend — `low` is the honest ceiling on
what this can tell a caller planning ahead.

### `GET /v1/sessions/:id/skills`

The file-backed skills a Claude Code session can invoke.

```console
$ curl -s -H "Authorization: Bearer $TOKEN" .../v1/sessions/$ID/skills
{"skills":[
  {"name":"code-review","description":"Review the current diff for correctness","source":"personal"},
  {"name":"deploy","description":"Ship the current branch","source":"project"}
]}
```

| field | |
|---|---|
| `name` | the command without its leading `/` |
| `description` | one line, the skill's own |
| `source` | `project` · `personal` · `plugin` · `admin` · `system` — for grouping, not for precedence, which has already been applied |

This is the `SKILL.md` files that session's working directory can reach, after the same precedence
a typed command would get: a personal skill replaces a project one of the same name, and a plugin
skill keeps its namespace and so can collide with neither. Claude Code has no read-only way to ask
a running session for its own slash menu, so this is read off disk rather than asked for — which
means it covers the file-backed half and not whatever a plugin adds at runtime.

**A Codex session answers `{"skills":[]}`.** Draw the menu from what came back, not from the
session's `assistant`, and a Codex session that starts answering with a list will need no change.

**Metadata only, and deliberately.** Neither a local path nor the body of a `SKILL.md` is in the
reply. A skill body may contain dynamic commands, and a menu that had to be executed to be read
would make every autocomplete a side effect. A session whose skills cannot be determined answers
`{"skills":[]}` rather than an error — an empty menu is a true statement about what can be offered.

### `GET /v1/sessions/:id/git`

The branch and changed files in that session's project, read when a client asks rather than sent
with every session-list update.

```console
$ curl -s -H "Authorization: Bearer $TOKEN" .../v1/sessions/$ID/git
{"git":{"branch":"main","head":"d5c61e9f91c46a77","ahead":2,"behind":0,"clean":false,"files":[
  {"path":"Sources/Foo.swift","from":null,"staged":false,"unstaged":true,"kind":"modified","additions":12,"deletions":3},
  {"path":"Sources/New.swift","from":"Sources/Old.swift","staged":true,"unstaged":false,"kind":"renamed","additions":1,"deletions":1}
]}}
```

| field | |
|---|---|
| `branch`, `head` | Git's branch name and full HEAD object id; `head` is empty for an unborn branch |
| `ahead`, `behind` | distance from the configured upstream, or zero when there is none |
| `clean` | true when `files` is empty |
| `kind` | `modified` · `added` · `deleted` · `renamed` · `untracked` · `conflict` |
| `from` | the old path for a rename, otherwise null |
| `staged`, `unstaged` | the two columns of porcelain v2's `XY` state; both may be true |
| `additions`, `deletions` | staged and unstaged numstat totals; null for binary or untracked files |

The route runs status and both numstat views with `GIT_OPTIONAL_LOCKS=0` and a timeout. It never
changes the worktree or index. A session outside a Git repository answers `404 not_a_repo` in the
standard [error envelope](#the-error-envelope).

### `GET /v1/places`

**Where a new session may be started**, which is a different list from `/v1/projects` and exists
for a different reason. `/v1/projects` is "directories somebody drew an icon for"; this is
"directories an assistant has actually been run in, and that are still there".

Both assistants' records go in and the list says nothing about which of them has been run where.
A directory is a directory: a folder you have only ever opened Claude Code in is a perfectly good
place to open Codex.

This is answered off the same queue as [`/v1/sessions/:id/info`](#get-v1sessionsidinfo) and shares
its limit of eight, so it too can come back `429 busy`.

```console
$ curl -s http://127.0.0.1:7717/v1/places -H "Authorization: Bearer $TOKEN" \
    | jq '{at, places: [.places[] | {id, label, path, at}][:3]}'
{
  "at": 1787067658,
  "places": [
    {
      "id": "24f9bac626da56ea",
      "label": "atrium",
      "path": "/Users/you/code/atrium",
      "at": 1787067059
    },
    {
      "id": "3b9e26c1587facfd",
      "label": "clawdline",
      "path": "/Users/you/code/clawdline",
      "at": 1787066824
    },
    {
      "id": "470885724e5330e1",
      "label": "cairn",
      "path": "/Users/you/code/cairn",
      "at": 1787065275
    }
  ]
}
```

Alongside `places`, the reply carries **`assistants`** — what this Mac will actually start, as
`{"id","label"}` rows in the order they should be offered:

```json
{"assistants":[{"id":"claude","label":"Claude Code"},{"id":"codex","label":"Codex"}]}
```

It is the Mac's list rather than a list a client bakes in, because whether Codex is installed is
something only that end can answer — and a button for an assistant that is not there opens a tab
that says `command not found`. The test is the home directory (`~/.claude`, `~/.codex`) rather
than the binary on `PATH`: an app launched from Finder inherits no login shell, so `PATH` is not a
question it can ask, and a directory full of sessions is better proof anyway. A Mac with neither
gets the whole list, because answering "nothing" there would leave no way to start the first one.

`icon` is left out there so the reply fits on this page; it is on every row and it is the same
shape as a session's.

| field | |
|---|---|
| `id` | opaque, stable between launches, and **the only thing you ever send back** |
| `label` | the project's name — the icon registry's when it has one, the folder's otherwise |
| `path` | so a person can tell two projects with the same name apart. Not something to build with |
| `at` | when that directory was last worked in. The list is sorted by it, newest first |
| `icon` | the project's mark, the same shape and meaning as a session's — the registry's when it has one, and a stable creature drawn from the path when it does not. Draw it the way the session list draws one, and still treat it as optional |

**Where the list comes from**, in the order it is assembled:

- **`~/.claude/projects/`** — Claude Code's own record of where it has run. One folder per project,
  named after the working directory with every character that is not an ASCII letter or digit
  turned into a dash. That map is many-to-one and **the name is never read backwards**:
  `-Users-me-code-cairn-frontend` is `cairn/frontend` and `cairn-frontend` equally, and a space, a
  dot and an underscore all arrive as the same dash. The real path is read out of the transcripts
  inside, which record it, and every candidate found there is checked back against the folder's
  name before it is believed — because a transcript quotes other people's directories, and one on
  the machine this was written on had a completely unrelated `cwd` sitting in its last hundred
  kilobytes inside something that had been pasted in.
- **The sessions clawdline can already see**, from `GET /v1/sessions`. These are directories a
  session is open in right now, which covers the one case the record above cannot: a project
  opened seconds ago, before anything was written down.

**What it excludes**, and each of these is a rule rather than a filter that happens to fire:

- a directory that is not on the disk any more — checked at listing *and* again at starting;
- a project folder that cannot prove which directory it stands for (an empty one, or one whose
  transcripts have been trimmed past the point where they say);
- any path that is not absolute, or that has a control character in it. A directory really can be
  called `a<LF>b` on macOS, and this list is built out of the filesystem rather than out of
  anything anybody typed;
- everything past the fortieth, newest first.

It is not a filesystem browser and will not become one. There is no way to ask it about a directory
it did not already offer.

### `GET /v1/places/:id/sessions`, `GET /v1/places/:id/sessions/:assistant`

**What one assistant has already recorded in one place**, so a client can offer to carry one of
them on instead of starting something new. Reading, not starting — it discloses the titles of
conversations held in a directory whose name this token could already see in `/v1/places`.
Leaving `:assistant` out is the original Claude route; name `claude` or `codex` to select one.

```console
$ curl -s http://127.0.0.1:7717/v1/places/3b9e26c1587facfd/sessions \
    -H "Authorization: Bearer $TOKEN" | jq '{at, place, assistant, sessions: .sessions[:2]}'
{
  "at": 1787067658,
  "place": "3b9e26c1587facfd",
  "assistant": "claude",
  "sessions": [
    {
      "id": "105344fb-c769-4b37-b766-403b410897eb",
      "title": "Planner.swift and POST /v1/intents",
      "at": 1787067059,
      "live": true
    },
    {
      "id": "bbf8dae0-2e51-4a7c-9d63-1c0f8b4a7e92",
      "title": "Make dictation reusable outside the composer",
      "at": 1787061204,
      "live": false
    }
  ]
}
```

Two hundred at most, newest first, and `id` is the conversation's own — which is also what its
transcript is named, and the only part of a row that goes back to the server.

**`more` says the two hundred was a cap and not the end.** The server asks its own reader for one
more than it sends, so this is a fact rather than a guess, and a client that draws the list has
something true to say at the bottom of it. There is no cursor to follow it with: a project's whole
history arrives in one reply, which is what lets a client's filter box search all of it instead of
only what is on screen. `more` is therefore a limit worth telling somebody about, not a page to
turn — say so rather than ending the list in silence.

**Only conversations somebody had.** Half of what is in a project folder is not one, and both
exclusions are read off a field rather than guessed at from the contents:

| left out | how it is known | why |
|---|---|---|
| sessions this Mac dispatched | the first turn *begins* with Clawdline's own briefing line | the app opened it, typed one instruction into it and closed it when the work came back. It is plumbing, not a conversation |
| `-p` and SDK runs | Claude Code's own `entrypoint: "sdk-cli"` or `promptSource: "sdk"` on that turn | `claude -p "what is 2+2"` writes a transcript like everything else; it was never an interactive session |

This is not a nicety. In this repository's own project folder, of a hundred and one transcripts,
fifty-two were dispatched children and eleven were `-p` probes — so a list capped at forty was one
whose cap fell in the middle of the plumbing, with most of the real conversations not on screen at
all. A client's filter box can only narrow what it was sent.

The briefing test is on the **first turn, and as a prefix**. A conversation that merely mentions
those words — one held *about* this feature does, at length — is still a conversation.

**`title` is read, never invented.** For Claude it is the name somebody renamed the conversation
to, the one Claude Code gave it, or the opening of the first thing a person typed. For Codex it is
the persisted `name` from app-server's supported `thread/list`, falling back to that method's
first-message `preview`. A record with neither is left out rather than listed as an untitled row
somebody has to guess at.

**`live` means something is writing to that transcript right now.** Resuming one of those would put
a second process on the same file, so a client is told which they are rather than left to find out.
It is a fact about the instant it was read.

**It may not be instant on a large project.** Claude names require reading transcripts, which can
run to tens of megabytes; Codex starts a short-lived app-server and asks its indexed `thread/list`.
Show a waiting line.

`assistant` in the reply says which index was read. A place with no conversations for that
assistant answers `{"sessions": []}`, which is ordinary. Codex listing includes interactive CLI
threads for that exact cwd, newest first; it does not expose exec runs or subagents. An id that is
not on `/v1/places`, or an assistant outside the closed `claude`/`codex` list, is `404 not_found`.

### `POST /v1/places/:id/resume/:session`, `POST /v1/places/:id/resume/:assistant/:session`

Opens a terminal tab in that place and picks that conversation back up in it — `claude --resume
<id>` or `codex resume <id>`. Leaving the assistant out selects Claude for compatibility.
Everything about *where* and *whether* is
[`…/start`](#post-v1placesidstart-post-v1placesidstartassistant) unchanged; this adds one literal
flag and one id.

```console
$ curl -s -X POST \
    http://127.0.0.1:7717/v1/places/3b9e26c1587facfd/resume/105344fb-c769-4b37-b766-403b410897eb \
    -H "Authorization: Bearer $TOKEN" -H 'Idempotency-Key: 6f1c9d3a-41b4' -d '{}'
{"ok":true,"id":"…","backend":"iterm","assistant":"claude","place":"…","cwd":"…","session":"105344fb-c769-4b37-b766-403b410897eb","at":…}
```

**There is no request body here either**, and the conversation is a path segment for exactly the
reason the assistant is one above. It is checked twice before it becomes part of a command line:
once for shape — a lowercase UUID as Claude Code writes them, and nothing else — and once against
one of two exact Mac-owned records. The ordinary case is the selected assistant's project listing
at that moment. A terminal schedule run is the narrow exception: its id must be the proven
child-conversation id returned by that schedule's detail response, and the retained task must also
match the selected project and assistant. This does not put dispatched children back into the
general project-history picker. An id neither source handed out is `404 not_found`, never a string
on a command line.

The shape check is exact rather than merely shell-safe. This is especially important for Claude:
`--resume` takes an **optional** value, so a value the CLI cannot read as an id becomes a search
term and opens its own picker in a tab nobody is sitting at. The same closed UUID rule is applied
to Codex before its `resume` subcommand is assembled.

The title shown in the selected history row is carried onto the new terminal immediately, before
the next inventory pass can rediscover its transcript or rollout. It is a display hint, not proof
of conversation identity: metadata writes such as renaming still resolve the process-bound record
the terminal actually opened. The hint is normalized and capped at 80 characters, is checked
against the observed conversation as soon as that record exists, and is discarded if the terminal
is reused for another assistant process. The schedule-only exception uses its retained task title.

The refusals are `…/start`'s, plus `not_found` for a conversation that is not on the listing — a
transcript deleted since you last looked, an id from another project, or one that was never real.
**Resuming a `live` conversation is not refused**, because the Mac cannot always tell which tab has
a transcript open; a client that has been told `live` should go to that session instead of asking
for this.

### `POST /v1/places/:id/start`, `POST /v1/places/:id/start/:assistant`, `POST /v1/places/:id/start/:assistant/:model`

Opens a terminal tab in that place and runs an assistant in it. Without the last two segments that
is `claude` on whatever model `claude` opens on, which is what this route did before there was
anything else to run — an existing client keeps working and does not have to know Codex exists.

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/places/3b9e26c1587facfd/start \
    -H "Authorization: Bearer $TOKEN" -H 'Idempotency-Key: 6f1c9d3a-41b2' -d '{}'
{"error":{"code":"write_disabled","message":"Sending is switched off. Settings → Remote turns it on, and it is off by default because typing into a session runs code on this Mac.","request_id":"2fd356e8-bef8-4f54-a312-851c0cfa8045"}}
```

With the switch on: `{"ok":true,"id":"…","backend":"iterm","assistant":"claude","model":"","place":"…","cwd":"…","at":…}`.

To open Codex instead, name it:

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/places/3b9e26c1587facfd/start/codex \
    -H "Authorization: Bearer $TOKEN" -H 'Idempotency-Key: 6f1c9d3a-41b3' -d '{}'
{"ok":true,"id":"…","backend":"iterm","assistant":"codex","model":"","place":"…","cwd":"…","at":…}
```

A fourth segment says how big the session should be. It is one of **`haiku`, `sonnet`, `opus`** and
nothing else:

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/places/3b9e26c1587facfd/start/claude/opus \
    -H "Authorization: Bearer $TOKEN" -H 'Idempotency-Key: 6f1c9d3a-41b4' -d '{}'
{"ok":true,"id":"…","backend":"iterm","assistant":"claude","model":"opus","place":"…","cwd":"…","at":…}
```

`model` is echoed back the way `assistant` is, and is `""` when the path named none. **A name that
is not one of the three is `404 not_found` with `"No model named that"`**, decided before the place
is looked up and never quietly turned into the default: a `200` for a session running on a model
nobody asked for is the one wrong answer nothing on screen would show. That is a shorter list than
the model names a dispatched task may carry — those may be a dated build like
`claude-opus-5-20260201`, and this is the list of *sizes* a person or a draft picks from.

**There is no request body.** Not "the body is optional" — it is not read, and there is no field
anywhere on this route that a directory or a command could be written into. The `id` in the path is
resolved against a list the server builds at that moment and the server's own copy of the path is
what gets used, so an id nobody was handed is `404 not_found` and never a directory.

**The assistant is a path segment for the same reason, and so is the model.** The assistant is
matched against the two names in `/v1/places`' `assistants` and nothing else — `…/start/emacs` is
`404 not_found` with `"No assistant named that"`, decided before the place is even looked up — and
what runs is a literal picked out of a closed list, never a string that reaches a shell. The model
is the same idea one segment further along: three names, matched exactly, and the only thing either
of them can add to the command line is `--model <one of three>`. Picking a recorded conversation
back up is the second named action this said it would be if it were ever wanted —
[`POST /v1/places/:id/resume/:assistant/:session`](#post-v1placesidresumesession-post-v1placesidresumeassistantsession),
with its own literal — and not a field on this one.

Three refusals are specific to this route and worth branching on:

| `code` | status | |
|---|---|---|
| `not_found` | 404 | that id is not on the list — including a directory that has been deleted since you last looked |
| `terminal_closed` | 409 | the terminal is not running, and **this will not launch it for you**. Somebody has to open it on the Mac |
| `terminal_unsupported` | 409 | the terminal named in Settings is not one this can drive directly. iTerm2 can be driven; everything else is reached through tmux, and without a tmux server there is nothing to do. It is refused by name rather than quietly opening iTerm2 instead |

Both of the `409`s carry **`app`** inside the `error` object — the terminal's name as macOS spells
it, so a page can write its own sentence around it instead of showing the English one:

```json
{"error":{"code":"terminal_closed","app":"Ghostty","message":"Ghostty is not running, and this will not launch it for you. Open it on the Mac and try again.","request_id":"…"}}
```

**Nothing is brought to the front.** The tab is made and written into and the Mac's window order is
left alone, because whoever pressed this is holding a phone and whoever is at the Mac is in the
middle of something else. The one exception is a terminal with no window open at all, where making
a window is unavoidably making a window.

#### Between "started" and the session appearing

`id` is the terminal's own id, in the same space as every `id` in `/v1/sessions` — but **the session
is not in the list yet when this answers**, and `GET /v1/sessions/:id` will `404` for a moment. That
is expected and is not an error. Nothing here invents a placeholder row: a session that does not
exist yet and a session that does are different things, and only one of them has a state, a title
and a transcript.

What a client should do:

1. Keep the `id` from the reply.
2. Watch the event stream, or poll `GET /v1/sessions`, for a session with that `id`. A reading is
   nudged as soon as the tab opens, so it normally lands within a second or two — but a shell has
   to start and `claude` has to be running before `ps` can see it, and on a cold start that can be
   longer.
3. `assistant` is absent (and `isClaude` is `false`) until the assistant is actually up, so wait
   for the id, not for the flag.
4. Give up after about fifteen seconds and say the tab was opened but has not reported in. Do not
   retry the start — the tab exists, and a second one is not what anybody wanted. (Retrying the
   *same* `Idempotency-Key` within ten minutes is safe and answers with the stored reply; that is
   what the header is for.)

### `POST /v1/sessions/:id/send`

Types the text into that session **and presses Return**. It is a prompt, not a draft.

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/sessions/$ID/send \
    -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
    -H 'Idempotency-Key: 8f0c1e2a-3b4d' \
    -d '{"text":"run the tests\n"}'
{"error":{"message":"Sending is switched off. Settings → Remote turns it on, and it is off by default because typing into a session runs code on this Mac.","request_id":"595e3d72-1a89-42bd-ad99-f98a9bdcefbe","code":"write_disabled"}}
```

With the switch on it answers `{"ok":true,"at":<unix seconds>}`. `text` must be a non-empty string;
anything else is `400 bad_request`.

The Clawdfather controls panel's `deep_status_audit` action uses this exact route after a visible
second-press confirmation. It sends the stable audit instruction to the exact Session carrying the
authenticated `session.coordinator` projection. This remains a user-attributed Session message:
the ordinary device token, `send` capability, remote write switch and `Idempotency-Key` all remain
required. The browser neither receives the orchestrator token nor calls a new machine-mutation
route. A successful HTTP receipt means only that the audit request was sent; it does not mean the
multi-session audit finished.

Authentication, the write-origin check, session lookup, body validation and the idempotency
reservation all happen on the server's serial state queue. The terminal handoff then moves to a
separate serial command queue shared with `/key`, `/focus`, `/end`, `/start`, `/resume`, background
shell kill and automatic child close, plus manual/timer schedule dispatch, serialized promotion and
terminal-bearing orchestrator task/handoff/wait delivery. That one admission domain is bounded at
eight operations globally and two per terminal session; coordination fan-out reserves every known
recipient, and nested inline work inherits or adds its real terminal channel. Health replies, SSE heartbeats
and slow-read completions therefore continue while iTerm2 is waiting on an Apple event. A concurrent
request with the same in-flight `Idempotency-Key` joins the first one and both connections receive
the same final response. This is an in-flight same-key guarantee, not terminal exactly-once: after
an Apple Event timeout, late execution is unknown and a new key may execute again. A ninth admitted
operation is `429 busy` and is not filed under its key.

Terminal failures are `502 terminal_io_failed`. Failure kind is bound to the operation that returned
it; a later global circuit sample cannot relabel an unrelated `ps` or backend failure. If an iTerm Apple Event times out or its list
response is malformed, the circuit fails closed with `502 iterm_attention_required`, `app: "iTerm2"` and
`action: "answer_dialog"`. Do not retry in a loop and do not click anything automatically: a person
must inspect the Mac. Automation resumes after a later well-formed list response proves that iTerm2
is accepting Apple events again; an incomplete `ps` scan does not arm the circuit.

### `POST /v1/sessions/:id/title`

Gives one live session a local display title. It is trimmed, folded to one line and limited to 200
characters.

**The name belongs to that conversation, not to the tab it is in.** A terminal outlives what runs
in it — leaving `claude` and starting it again keeps the same session id — so the stored name is
matched back by Claude's hook session id where there is one, and otherwise by the start time of the
assistant process that was in the tab when the name was chosen. The next conversation in the same
tab is a different conversation and gets the automatic label, which is also what keeps a person's
old name from covering the task title of a session this app opens for a dispatch.

An empty or whitespace-only `title` clears the local choice, and the label falls back to whatever
the automatic sources say *now*: the task this session was dispatched for, the Codex thread name, or
the terminal's own title. Clearing also drops Clawdline's own memory of a Codex thread's name, so
the label goes back to the automatic one rather than to the name that was just cleared. **What it
cannot do is un-name the thread.** `thread/name/set` has no undo and Clawdline does not know what
Codex would have called it, so the name a person typed stays in Codex's own metadata: `codex resume`
still lists the thread under it, and a later reading of that metadata puts it back on the label. The
terminal's own title is never changed by this local step.

**For Claude, the newer of the two human names wins.** Naming a session here does not stop a person
from typing `/rename` straight into that terminal afterward — and when they do, the name they just
typed is the more recent one, so it is what the label shows from then on. Clawdline notices this by
comparing the transcript's own `customTitle` against what it was at the moment this route ran, not by
timestamp: an old rename in a transcript that gets named here today must not look newer than it is.
Naming it here again, or clearing it, makes this route's name current again. This applies only to
Claude — Codex has no `/rename` — and only when a transcript already existed to compare against; a
name set before Claude Code has written its first byte has nothing to compare against and simply
wins, the way it always did.

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/sessions/$ID/title \
    -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
    -H 'Idempotency-Key: 97707bb0-0274' \
    -d '{"title":"Release room"}'
{"ok":true,"title":"Release room","display_title":"Release room","local_applied":true,"downstream":"synced","downstream_synced":true}
```

`local_applied` says whether the durable Clawdline name is durable: it is the result of writing
this Mac's config, not a constant. `false` means the name is in use on every surface right now and
will be gone when the app restarts — a full disk or an unwritable `~/.config/clawdline` — and it is
answered `200` rather than `500` because the name really did take effect. `downstream` separately
says what happened to the assistant's own name: `synced` for an idle Claude `/rename`, `queued` for
a Codex thread update, `busy` when Claude was anything other than idle — working, asking a question,
**on a screen this Mac could not read**, or showing a menu when the screen was read again just
before typing — `unavailable` when no Codex thread could be identified, `failed` when the terminal
handoff failed, and `local_only` for a clear or a non-assistant shell.
`busy` is therefore "not typed into", not "seen to be working". The reading it starts from is the
session list's, up to twenty seconds old while the app is in the background; a session that reading
calls idle is then captured once more before anything is typed, because a slash command sent to a
menu is not typed at all — the picker discards it and acts on the Return that follows, confirming
whichever row is highlighted. `downstream_synced` is true only after a synchronous downstream
handoff succeeded. Busy Claude sessions are deliberately not queued: a local title is durable,
while replaying a slash command after an app restart would need a second durable command protocol.

The body must contain a string `title`; a different type or an overlong normalized title is `400
bad_request`, and an unknown session is `404 not_found`. Like `/send`, this is an authenticated,
idempotency-keyed write and is refused while remote writing is switched off.

### `POST /v1/sessions/:id/key`

Answers a menu with a single keystroke. `{"key":"1"}`…`{"key":"9"}`, `{"key":"tab"}`,
`{"key":"shift+tab"}`, or `{"key":"submit"}`. Back-tab (`ESC [ Z`) goes as one terminal sequence
and is used to cycle Claude Code's permission mode. Anything else is `400 bad_request`, and the
allowlist is checked before anything goes looking for a terminal.

`submit` is the one that is a name rather than a key, and it is valid only on a menu whose `submit`
field is present. The button a multi-select draws under its rows has no keystroke of its own, so
the Mac walks the highlight onto it and presses Return there — reading the screen back at each
step, and stopping with the dialog untouched if the highlight will not land. A backend failure is
`502 terminal_io_failed`; an operation-bound iTerm modal refusal is
`502 iterm_attention_required`.

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/sessions/$ID/key \
    -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
    -H 'Idempotency-Key: 41c0a7e5-9b23' \
    -d '{"key":"2"}'
{"ok":true}
```

**This, and not `/send`, is how a question gets answered.** Claude Code's picker discards a
bracketed paste and then acts on the Return that follows it, so text posted to a session showing a
menu does not type anything — it confirms whichever row is highlighted. Measured: with the caret on
the third option, sending the word "Tea" answered "Water", silently. `/send` refuses outright with
`409 showing_a_menu` when it finds one, and points here.

The number to send is the option's own `n` from [`menu`](#the-session-object) above. A retried
`Idempotency-Key` answers with the stored reply rather than pressing again, which matters more here
than anywhere else: the same digit arriving twice answers a question and then types a stray
character into whatever replaced it.

### `POST /v1/sessions/:id/focus`

Brings that session's window to the front. No body.
It enters the same bounded terminal broker as the other mutations. A focus Apple Event that is
refused by the open iTerm circuit answers `502 iterm_attention_required`; focus never reports 200
after discarding a backend failure.

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/sessions/$ID/focus \
    -H "Authorization: Bearer $TOKEN" -H 'Idempotency-Key: 3d9b7c14-55e2' -d '{}'
{"error":{"code":"write_disabled","request_id":"7f4e2df2-ab8e-4c22-9954-4a803d1d247a","message":"Sending is switched off. Settings → Remote turns it on, and it is off by default because typing into a session runs code on this Mac."}}
```

It only moves a window, and it is behind the same gate as sending anyway — one switch, so there is
never a question about which of them a device has.

### `POST /v1/sessions/:id/shells/:shellId/kill`

Stop one of that session's background commands. **The second route here that destroys
something**, after `/end`, and behind the same gate — a build somebody is forty minutes into dies
on a mis-tap, and nothing brings it back.

What makes it defensible is that the app proves which process it is signalling before it signals.
Three facts have to line up: the id is one this session announced as a background command,
something is still holding its output file open, and **that holder is a child of this session's
Claude Code process**. The signal goes to that process's group — a shell one-liner is a shell and
whatever it is currently running — checked against Claude Code's own group, because signalling
that would end the session along with the command. `SIGTERM` first; `SIGKILL` five seconds later
only if the file is still held.

```console
$ curl -s -X POST "http://127.0.0.1:7717/v1/sessions/$ID/shells/bao9i2a93/kill" \
    -H "Authorization: Bearer $TOKEN" -H "Idempotency-Key: $(uuidgen)" -d '{}'
{"ok":true}
```

`404 not_found` is a command that is not running — finished, never this session's, or an id shaped
like a path. **`409 unidentified` is the one worth handling**: the process could not be tied to
this session, so *nothing was signalled*. It is what a machine that will not answer `lsof` or `ps`
looks like, and the answer to it is to stop the command on the Mac with `/tasks`, never to retry.

Nothing has to be told to Claude Code afterwards. It is waiting on that process, so it sees the
exit, writes `[exited with code 144]` under the output and posts its own notification — the same
thing that happens when a command is stopped from the Mac.

### `POST /v1/sessions/:id/end`

Ends the session and closes the terminal tab it occupied.

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/sessions/$ID/end \
    -H "Authorization: Bearer $TOKEN" -H 'Idempotency-Key: 3d9b7c14-55e2' -d '{}'
{"ok":true}
```

**The close gate.** At the moment of the press the broker computes `lost_if_closed` — the live
descendant tasks this close would cancel and the open waits whose waiters it would strand. When
that list is non-empty and the body does not carry `"accept_loss": true`, the route refuses:

```json
{"error":{"code":"would_lose_work","message":"…","lost":[
  {"task":"…","title":"review the patch","state":"briefed"},
  {"wait":"…","release_condition":"the docs are committed","waiters":1}]}}
```

Show the list, then repeat the same request with `accept_loss: true`. A close with nothing at
stake is unchanged — one press, empty body. This is deliberately a gate at the close and not a
list column: a label read earlier does not stop a close (docs/session-states.md#lost_if_closed).

**The proven close.** A caller may additionally send `expected_closeability_version`, copied
verbatim from the opaque `closeability.version` on the Session row it drew
([closeability](session-closeability.md)). The broker recomputes the projection at its end of the
same press and proceeds only when the state is still `safe` *and* the version still compares
equal. Anything else is refused:

```json
{"error":{"code":"close_not_proven","message":"…","closeability":{
  "state":"blocked","reasons":[{"code":"pending_landing_owned","kind":"obligation",
    "subject_kind":"task","subject_id":"…","mover":{"kind":"session","self":true,
    "person_needed":false}}],"version":"cl1_…","observed_at":1788005803,
  "activity_generation":42,"obligation_generation":91,"session_generation":63,
  "provenance":["broker"],"attestation_id":null,
  "source":{"provenance":"session_watch","freshness":"current",
    "observed_at":1788005790,"max_age_seconds":45}}}}
```

Two rules matter more than the mechanism. **Omitting the field changes nothing** — every client
written before this existed keeps exactly the `lost_if_closed` contract above, which is why the
field is an opt-in rather than a new precondition. And **`accept_loss` is not an answer to this
gate**: it is the human override for a positive victim list somebody was shown, and a stale,
ambiguous, unattested or superseded projection produces no such list. Accepting a loss nobody can
enumerate is not consent, so a request carrying both is still refused `close_not_proven`.

**Not a capability of its own.** A device that may type into a session can already send `/exit` and
then `exit`; this is the same power with the two steps joined and named. What it adds is that the
second step lands on a tab that has already left the session list — which is exactly why doing it
by hand from a phone was impossible. The assistant is asked to leave through its own word, `/quit`
for Codex and `/exit` for Claude Code, because each refuses the other's.

**It takes the session's children with it.** Every child tab of an
[orchestrator](orchestrator.md) task this session dispatched is closed *before* this one is — a
task is matched to its root through the hook note on the tty of the tab about to go, so after the
fact there is nothing left to match. A task still running is cancelled with its tab; one that
already finished keeps its record and loses only the tab, because `success` is a fact about work
that happened and the tab was being held for a reader who is leaving. **The cascade goes deepest
first**, and it is gathered from the finished children as well as the live ones — though in a live
tree the walk goes one level and stops, because a child dispatches nothing and there is nothing
below it. The order and the finished-parent sweep are kept for a stored record an older build left
behind, where a task below a task can still be found. Closing a tab by hand cascades to nothing:
only this route does. The audit log carries `orchestrator.cancel` for the first kind and
`orchestrator.close` for the second, both with `why=root_ended`.

`502 internal` carries what actually failed; the session is left as it was found.

### `POST /v1/voice`

Reads a recording back as text. It types nothing and sends nothing: the transcript goes to whoever
asked for it, and putting it into a session is [`/send`](#post-v1sessionsidsend)'s job — which is
what lets the page drop the words into the box and leave somebody a chance to fix them first.

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/voice \
    -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
    -H 'Idempotency-Key: 3f9a1c04-77e2' \
    -d '{"audio":"'"$(base64 < clip.raw | tr -d '\n')"'","rate":16000}'
{"text":"change the retry to exponential backoff","ms":1640}
```

`audio` is base64 of little-endian 16-bit mono PCM. **`rate` is required, and `16000` is the only
value it may have** — checked, never resampled, and with no default. A default would be this route
assuming what kind of bytes it had been handed, and nothing in the bytes can check the assumption
afterwards: 48 kHz read as 16 kHz answers `200` with a transcript of somebody talking three times
too fast. That is worse than an error, because an error says which request went wrong and a bad
transcript does not.

**An empty `text` with a `200` is an answer, not a failure.** It means nothing was heard, and a
client that draws it as an error apologises for a microphone that worked.

| | when |
|---|---|
| `400 bad_request` | no `Idempotency-Key`; no `audio`, or not base64; `rate` missing or not `16000`; under 0.25 s; over 300 s |
| `401 unauthorized` | no token, or one this Mac does not know |
| `403 write_disabled`, `403 forbidden` | the write switch is off; or this device may read and not send |
| `429 busy` | one recording is being read and one is waiting. The third is refused on the spot rather than made to queue |
| `503 no_whisper` | nothing here to transcribe with — `error.reason` is `no_binary` or `no_model` |

**The same gate as `/send`** — [all three of them](#writing-three-gates-in-this-order), the write
switch and `send` on the device and the key — even though nothing here writes to a session.
Transcribing spends twelve seconds of this Mac on demand, and `read` is the capability you are
meant to be able to hand out without thinking about it.

**The language is not the client's to name.** `voice_language` on this Mac decides, and what comes
back has been through `voice_vocabulary` on the way out, exactly as it is for the bar.

**`429` and `503` are the two answers not filed under the `Idempotency-Key`.** Everything else is,
refusals included, so the same key inside the ten minutes hands back the same transcript rather
than reading the same audio twice. Those two are facts about this machine at this moment — the
queue drains, whisper gets installed — and a cached one would answer *busy* long after the queue
had emptied.

The audit line is `voice.transcribe`. What the queue is protecting, and why a read-only device is
not offered a microphone at all, is [in the other page](remote.md#post-v1voice).

### `POST /v1/auth/*`

The pairing flow is [in the other page](remote.md#how-a-device-is-paired) — it is a thing a person
does, not a thing a client automates. What matters here:

- `POST /v1/auth/pair` `{"name":…}` → `{"pairing_id":…,"expires":…}`, and the code appears **on the
  Mac's screen only**. Three of these in ten minutes and the route answers `429 rate_limited`.
- `POST /v1/auth/pair/confirm` `{"pairing_id":…,"code":…}` → `{"ok":true,"token":…}` and a cookie.
  A wrong code is `403 wrong_code`, and the body carries `tries_left` — a page saying "two tries
  left" should not be counting for itself. A lapsed or exhausted pairing is `403 expired`, which is
  a different code because it is a different thing to do about it: one is try again, the other is
  start again.
- `POST /v1/auth/password` `{"password":…,"name":…}` → the same, if a password has been set. A
  correct password *mints* a device token rather than being one, so it can be changed without
  re-pairing anything and a device can be revoked without changing it.
- `POST /v1/auth/adopt` `{"token":…}` → trades a token the page is holding for the cookie the event
  stream will use. It grants nothing: a token that is not already ours is refused exactly as it
  would be anywhere else.
- `POST /v1/auth/logout` → clears the cookie **and revokes the device it authenticated as**.
  Clearing a cookie is not signing out: the token it held is still a key and the browser may still
  have it written down, so "sign out" here means what somebody handing a laptop back would expect
  it to mean.

These five are the only mutating routes that do **not** want an `Idempotency-Key`, and they are not
idempotent either. Each is its own kind of one-shot: a retried pairing is a new pairing with a new
code on the Mac's screen, and there is no request here whose repeat could quietly do something
twice.

### `POST /v1/orchestrator/handoffs`

Open a new root session and give it a continuation package. The sender first creates
`/tmp/.clawdline/handoffs/<handoff_id>/handoff.md`; the app verifies only that this is a non-empty
regular file and never reads its contents. The full package and receiving contract are
[`docs/handoff.md`](handoff.md).

```console
$ H=$(uuidgen | tr 'A-Z' 'a-z'); umask 077; mkdir -p /tmp/.clawdline/handoffs/$H
$ # write a non-empty handoff.md, then:
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/handoffs \
    -H "X-Clawdline-Orchestrator: $(cat ~/.config/clawdline/orchestrator-token)" \
    -H 'Content-Type: application/json' \
    -d "{\"handoff_id\":\"$H\",\"project_dir\":\"$PWD\",\"assistant\":\"codex\"}"
{"ok":true,"handoff":{"id":"7c1e9b02-4d55-4a80-9c3e-1f6b2a09d431","state":"opening","projectDir":"/Users/you/code/clawdline","assistant":"codex","dir":"/tmp/.clawdline/handoffs/7c1e9b02-4d55-4a80-9c3e-1f6b2a09d431","opened":{"terminalId":"9A1F…","backend":"iterm"}}}
```

`handoff_id` is a 36-character lowercase UUID in the same `[a-f0-9-]` shape as `task_id`.
`project_dir` is required, absolute, and an existing directory. Optional `assistant` is `claude`
(the default) or `codex`; optional `model` is 1…64 lower-case letters, digits, `.`, `_`, or `-`,
and cannot begin with `-`. Optional `title` and free-form `from_session` are each at most 200
characters. `title` labels the tab; `from_session`, when it identifies a watched session, receives
one best-effort version-2 `handoff_receipt` notice. Its state is `picked_up` when the first line was
confirmed or `first_line_failed` when the tab opened but the line did not land; both are one kind
because they are outcomes of the same delivery attempt. This is intentionally the loose resolver:
a conversation id or the watched terminal's own id may identify the sender. Task `root.session_id`
does not have that terminal-id shortcut.

Opening the terminal is synchronous and is named in `opened`; composer waiting, trust confirmation,
typing, and transcript confirmation happen after this response. The durable registry stores only
`handoff_id`, `project_dir`, optional `title` and `from_session`, `created`, and `state`. Repeating an
id replays that envelope and opens no second tab, including after restart. A replay contains neither
`opened` nor `assistant`, because only the envelope is stored. Terminal envelopes and their package
directories are removed 24 hours after `created`. There is no GET, cancel, or completion route.

The route uses the same orchestrator switch and ten-minute brake as dispatch. Replays do not take a
ticket; every new call that gets past its id does, including a refusal. Errors are decided in this
order:

| `code` | status | |
|---|---|---|
| `forbidden` | 403 | the orchestrator header is missing or wrong; a device token never reaches the body |
| `bad_request` | 400 | the body is not a JSON object or has no `handoff_id` |
| `orchestrator_disabled` | 403 | the shared orchestrator switch is off |
| `bad_task` | 422 | an invalid field, project directory, package directory, or empty/missing `handoff.md` |
| `rate_limited` | 429 | the shared dispatch-and-handoff brake is full |
| `terminal_closed` / `terminal_unsupported` | 409 | the selected terminal cannot be opened; `app` names it |
| `internal` | 500 or 502 | terminal automation failed |
| `not_found` | 404 | this build has no handoff route |

### `POST /v1/orchestrator/root-assignments`

This machine-only route launches a new ordinary Root that independently owns a Feature. It is not
a task, handoff, or detached automation. The orchestrator token is required for creation and both
full-record reads. A paired device cannot call these Root Assignment routes; its ordinary
`GET /v1/sessions` read receives only the bounded nested `root_assignment` projection documented
below, including the display label but never the project path or five-field assignment envelope.

`Idempotency-Key` is required and must equal the lowercase UUID in `request_id`. The request is a
closed object—unknown keys are `bad_root_assignment`:

```json
{
  "request_id": "3bff2f2b-f7c1-4745-9563-da5c2a31e647",
  "assistant": "codex",
  "model": "default",
  "project_dir": "/Users/me/code/project",
  "label": "Durable import redesign",
  "assignment": {
    "objective": "Ship the importer redesign.",
    "scope": "Importer, migrations, tests, and operator docs.",
    "constraints": "Preserve rollback and do not restart the live app.",
    "relevant_references": "docs/importer.md and ADR-17.",
    "acceptance": "The exact candidate tree passes the importer acceptance suite."
  }
}
```

Each of the five assignment strings is non-empty and at most 8,192 UTF-8 bytes; together they are
at most 32,768 bytes. `label` is 1–200 UTF-8 bytes. The project must be an existing absolute
directory and its canonical path is stored. `assistant`, `model`, project and label are explicit;
`model:"default"` delegates the concrete provider model without omitting the choice.

Acceptance is persisted before terminal opening. The record advances through `accepted`,
`terminal_opened`, `prompt_ready`, `briefed`, then `active`. `blocked` names
`workspace_trust_required` and leaves the picker untouched with no broker timeout: the ordinary
terminal-open-to-briefing deadline is 240 seconds, but time spent waiting for a person's trust decision does
not consume it. When the picker leaves, durable `prompt_timeout_started_at` starts a fresh
240-second pre-brief window and the same record may continue to `prompt_ready`.
The current build has no automatic workspace-trust authority; a future positive policy adapter
must explicitly justify acceptance, and the broker durably records `answered_trust_menu` before
such an adapter may answer the picker so one picker is answered at most once.
`failed` is a launch or delivery failure; `inactive`
means the exact process disappeared after briefing. Timestamped receipts and the exact
terminal/tty/pid/process-start/conversation tuple are stored. There is no timeout, task secret,
`result.json`, parent, result, landing, handoff, or detached field.

A retry with the same request id and identical canonical body returns the same non-failed
assignment with `replayed:true` and never opens another tab. A failed assignment is a terminal
idempotency result: replay returns `409 request_terminated` and requires a new request id rather
than reporting `ok:true` or risking a duplicate tab. Different content is `409 request_conflict`. A restart
with only `accepted` persisted is `launch_receipt_lost`: reopening could duplicate a tab whose side
effect happened just before the crash. Reconciliation adopts only one exact process/conversation
tuple. Ambiguity is `ambiguous_identity`; incomplete inventory waits as `stale_inventory`; a
confirmed loss is `process_lost_before_briefing` or `process_lost_after_briefing`. Other typed
failures include `assistant_unavailable`, `prompt_timeout`, `delivery_unconfirmed`,
`workspace_trust_required`, `restart_identity_incomplete`, `idempotency_mismatch`, and
`persistence_failed`. Each typed `blocked`, `failed`, or `inactive` transition writes one durable
transition receipt and one audit event with assignment id, state, reason and the exact available
terminal/process/conversation identity. The receipt prevents timer beats or restart recovery from
spamming the same transition; the audit keeps a pre-brief orphan tab findable without ever
cascade-closing a genuinely briefed independent Root.

`GET /v1/orchestrator/root-assignments` returns `root_assignments`; the `/:id` form returns one
`root_assignment`. Session rows carry the bounded nested projection
`root_assignment:{id,label,state,ownership,explanation}` with
`ownership:"independent_root"`; there is no `role:"root_assignment"` field, and these roots never
acquire a task role or child indentation. Failed and inactive records remain in the machine-only
inventory and audit, not in the live Session projection.

### `POST /v1/orchestrator/tasks`

**Owned-child dispatch.** One Session asks for a task to be run by another one: normally Clawdline opens a
terminal tab in the task's directory, starts the assistant the task named, types a first message
into it, and watches for the answer. Optional `task.json` field `attach_session` instead names an
existing assistant Session from `GET /v1/orchestrator/sessions`; the same complete task is typed
there without opening a tab. The concept, the trust model and the file formats are
[`docs/orchestrator.md`](orchestrator.md); what follows is the request.

The body is two fields and neither of them is the work:

```console
$ TASK=$(uuidgen | tr 'A-Z' 'a-z'); SECRET=$(openssl rand -hex 32)
$ umask 077; mkdir -p /tmp/.clawdline/$TASK/artifacts   # …and write task.json into it
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/tasks \
    -H "X-Clawdline-Orchestrator: $(cat ~/.config/clawdline/orchestrator-token)" \
    -H 'Content-Type: application/json' \
    -d "{\"task_id\":\"$TASK\",\"secret\":\"$SECRET\"}"
{"ok":true,"task":{"id":"3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234","state":"spawning","kind":"image","title":"Project portrait","assistant":"codex","reasoning_effort":"high","projectDir":"/Users/you/code/clawdline","created":1787100000,"spawnedAt":1787100002,"dir":"/tmp/.clawdline/3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234","root":{"sessionId":"841cbb8d-58b1-4765-9a71-bcdba19bcfef","assistant":"claude","label":"clawdline main"},"child":{"terminalId":"9A1F…","backend":"iterm"}}}
```

*(Example. The orchestrator routes are newer than the server this page's other transcripts were run
against, so the replies in this section are written to the contract rather than pasted out of a
run. The shapes are the contract either way.)*

**The instructions are in a file, not in this body.** `/tmp/.clawdline/<task_id>/task.json` is
written by the caller before it asks, `0700` on the directory, and the server reads and validates it
at dispatch — the [schema is over there](orchestrator.md#taskjson--written-by-the-root-before-it-asks-for-anything).
The `task_id` has to be the same string in three places: the directory name, the file, and this
body. Two of them agreeing is not enough, because the interesting failure is a body pointing at
somebody else's directory.

Auth is the header and only the header:

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/tasks -d '{"task_id":"…","secret":"…"}'
{"error":{"code":"forbidden","message":"Dispatching needs the orchestrator token.","request_id":"c1e0b7a4-2f5d-4a19-8b0e-71c93d5ea882"}}
```

`403` rather than `401`, and the difference is not cosmetic: `401 unauthorized` is this server's
answer to *no paired device*, and pairing a device would not help here. This route is not reachable
with a device token at all. It is also **not behind the write switch** and takes **no
`Idempotency-Key`** — two exceptions with one reason between them. The write switch is a decision
about what *paired devices* may do, and the orchestrator token is not a device; and idempotency is
already carried by `task_id`, which is the caller's own identifier for the work rather than a
per-attempt header. Send the same `task_id` twice and the second call answers `200` with the
existing record, having opened nothing.

Dispatch refusals are closed and typed; a client should branch on every applicable code:

| `code` | status | |
|---|---|---|
| `forbidden` | 403 | the header is missing or wrong — or `orchestrator_enabled` is off |
| `attach_session_not_found` | 404 | no watched session has that id. The resolver sees every session this Mac watches, which is wider than what `GET /v1/orchestrator/sessions` publishes: naming a plain shell resolves and is then refused `attach_unsupported`, not `404`. No task is created |
| `attach_unsupported` | 409 | the named Session is a plain shell with no assistant to read a briefing |
| `attach_not_managed` | 409 | the Session has no task role, or its recorded launch grant covers only the earlier task's own directory rather than `/tmp/.clawdline`. Both a user-opened session and a Clawdline-opened leaf fail this check: neither can read a new follow-up task's sibling `CHILD.md`, and `--add-dir` cannot be added to a running process. Only a task role whose persisted launch-time grant covers the whole task root can be attached to |
| `attach_assistant_mismatch` | 409 | the task's `assistant` differs from the assistant resident in the named Session |
| `attach_session_occupied` | 409 | the Session already has one live Clawdline task; attached sessions are single-flight |
| `attach_session_busy` | 409 | its cached state is `waiting` and `Targets.isChoosing` confirms a menu; nothing was typed and retrying the same task body is safe |
| `attach_delivery_failed` | 502 | validation and registration succeeded but the first line could not be typed. The task record exists in `spawn_failed` |
| `bad_task` | 422 | `task.json` is missing, unparseable, or a field is out of range — including an `isolation` other than `none` or `worktree`, an invalid `isolation_base`, `model`, `reasoning_effort`, `permission_mode`, `plan`, typed `graph`, `claims`, `serialize`, or contradictory `root.poll_only`. Graph validation rejects unknown keys, duplicate ids, missing dependencies, self-edges and cycles. `reasoning_effort` is Codex-only and exactly `high` or `xhigh`; omission inherits Codex/user defaults. `claims` is 0…32 unique relative POSIX paths of 1…1024 characters with no `/` prefix or `..` component; `message` names every invalid item |
| `detached_route_required` | 422 | ordinary `/v1/orchestrator/tasks` received `root.poll_only:true`. Nothing is registered or opened. Resolve the interactive Root through `GET /v1/orchestrator/whoami` and resend with its conversation id and assistant; only unattended automation belongs on `/v1/orchestrator/detached-tasks` |
| `graph_definition_conflict` | 409 | another task already carries the same graph id with a different destination, node list, unknowns, or out-of-scope boundary. Nothing is registered or opened |
| `graph_frontier_blocked` | 409 | the task's `current_node` depends on a node without a durable completion receipt. `blocking_nodes` names each node, its derived state, and its task id when one exists |
| `graph_dependency_failed` | 409 | a dependency task failed, a review node lacks `safe_to_land`, a verification node lacks a passing verification receipt, or another dependency produced a terminal failed state. Correct or replace that node before dispatching its successor |
| `graph_node_active` | 409 | the current node already has an active task or landing obligation, or another dispatch is atomically admitting it. Its completed evidence cannot be replaced by a concurrent attempt |
| `graph_node_complete` | 409 | the current node already has durable completion evidence. Model a correction as its own node rather than making the control sheet silently regress |
| `root_session_required` | 422 | `root.session_id` is null or empty. Nothing is registered or opened; prove the current interactive Root through `GET /v1/orchestrator/whoami`, then send its process-bound conversation id |
| `root_assistant_required` | 422 | a non-null `root.session_id` was supplied without an explicit `root.assistant` of `claude` or `codex`. Nothing is registered or opened and the provisional rate ticket is refunded. New ordinary HTTP dispatch never applies the historical Claude default |
| `root_unresolved` | 422 | the supplied non-null root id resolves to no live process-bound session for `root.assistant`. Nothing is registered or opened; re-prove the same interactive Root through `GET /v1/orchestrator/whoami` and correct the tuple. Do not turn identity failure into detached work |
| `conversation_ambiguous` | 409 | more than one live process of `root.assistant` proves the same conversation id; no owner is selected and nothing is registered or opened |
| `root_identity_is_terminal` | 422 | positive active-terminal or durable-Coordinator evidence proves `root.session_id` is a physical terminal id. The evidence is collected independently of caller-declared `root.assistant`; the error returns the actual `canonical_root_session_id` and `canonical_root_assistant`. Conflicting/unknown evidence remains nullable rather than guessed. The task is not registered and the provisional dispatch-rate ticket is refunded |
| `assistant_exhausted` | 409 | the named assistant's own account-level quota reads `exhausted` — see [`GET /v1/orchestrator/assistants`](#get-v1orchestratorassistants). The error object carries `assistant`, `availability`, `source`, `observed_at`, `age_seconds`, `resets_at`, `retry_after` (`min(resets_at - now, 3600)`), and `alternatives` — every other assistant's own `id`/`availability`/`detail`, so a client can dispatch to one of those instead of retrying the same one blind. `task.json`'s `"ignore_quota": true` sends it anyway; the message names that field outright. Checked after capacity and depth, before any git subprocess — cheaper than either, and the reply's own `message` says why. This is a fact about the account, not the task: it fires whether or not the failing session sits in this Mac's own tree |
| `worktree_unavailable` | 409 | worktree isolation was requested but the repository has no commit to use as a base or the destination volume has less than 2 GB available. This is an environment refusal rather than malformed JSON |
| `workspace_busy` | 409 | a live task from another definitely identified root reserved an equal, ancestor, or descendant claim. The error object carries `blocking_task`, `title`, nullable `root_label`, Unix-second `created`, absolute `conflict_paths`, advisory `retry_after`, `age_seconds` (`now` minus the blocking task's `created`, an integer), and `root_key` (the blocking task's root tree, hashed — see below). The rejected task is not registered and does not spend dispatch rate-limit budget |
| `depth_exceeded` | 409 | **the caller is a child, and a child opens nothing.** The floor is the constant `Orchestrator.depthFloor`, not a setting: `orchestrator_max_grandchildren` still sits in every `config.json` that was ever seeded, is preserved by `save()` as any unknown key is, and is read by nothing. Not a retry — the parallel work goes to that assistant's own subagents |
| `over_capacity` | 429 | this root's slots are full (`orchestrator_max_children`), or the whole Mac's are (`orchestrator_max_children × 4`). Registered `queued` tasks count toward these limits even before a tab opens, preventing an unbounded queue. The error object carries `retry_after` in seconds, and `message` says which |
| `rate_limited` | 429 | more than ten dispatches in ten minutes, or more than one full tree's worth if that is larger |
| `not_found` | 404 | this build has no orchestrator |

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/tasks \
    -H "X-Clawdline-Orchestrator: $ORCH" -d "{\"task_id\":\"$TASK\",\"secret\":\"$SECRET\"}"
{"error":{"code":"over_capacity","retry_after":60,"message":"All 5 child slots for this session are busy; retry when one finishes.","request_id":"7b2c19d0-6e44-4a2f-9c31-0d5e8ab41f77"}}
```

A claims conflict is answered before serialization, spawning, or L1 workspace warnings:

```json
{"error":{"code":"workspace_busy",
          "message":"Another dispatch tree has reserved a path this task claims.",
          "blocking_task":"a70c5e11-3b28-4d6f-8e10-2c94b7f0d3aa",
          "title":"Edit the orchestrator","root_label":"clawdline main",
          "created":1787696800,"age_seconds":42,
          "root_key":"9f1c2e7a",
          "conflict_paths":["/Users/you/code/clawdline/Sources/Orchestrator.swift"],
          "retry_after":60,
          "request_id":"c1e0b7a4-2f5d-4a19-8b0e-71c93d5ea882"}}
```

The failed attempt writes `orchestrator.claims.blocked` to the audit log but does not count toward
the ten-minute dispatch rate limit. The blocking context is enough for the caller to choose
whether to wait, coordinate with that root, or escalate — without a follow-up GET: `age_seconds`
is computed against the answering request's own clock, and `root_key` is stable identity rather
than prose. **`root_label` is self-reported and can be stale or shared by two unrelated roots** —
two different dispatch trees both calling themselves "clawdline schedules" is a real case, not a
hypothetical — **while `root_key` is `Orchestrator.rootKeyDigest`: SHA-256 of the same canonical
root key the broker already uses for identity (a live root's session id, or `task:<id>` for a
task resolved back to itself), truncated to its first 8 hex characters.** The same tree always
hashes to the same `root_key`; two roots that share a label do not share a `root_key`.
A `200` means *registered and being opened*, not *running*. `state` is `queued` or `spawning` when
this answers and the child has typed nothing yet; watch the record, or wait to be told.

At registration, a non-null `root.session_id` is the assistant's process-bound conversation id,
never the watched terminal id. The broker resolves it against `root.assistant` and stores that
conversation id, so completion notification, grouping, per-root capacity and the root-close
cascade use one key. No matching live owner returns `422 root_unresolved`; more than one
same-assistant process proving the conversation returns `409 conversation_ambiguous`. Both tuple
fields are required for a new ordinary HTTP dispatch: omission or explicit null for
`root.assistant` returns `422 root_assistant_required` before capacity, registration or terminal
opening. Every ownership refusal above occurs before registration. An identity lookup failure is
not a mode decision: the caller repairs the same Root tuple and resends the same task id.

### `POST /v1/orchestrator/detached-tasks`

This is the separate door for unattended automation with no interactive owner. It reads the same
task directory and the same two-field request body as the owned-child route, but accepts only
`"root":{"session_id":null,"poll_only":true,…}`. The caller owns polling `GET
/v1/orchestrator/tasks/:id` and `result.json`; no completion notification, owner grouping, per-root
capacity, or root-close cascade can exist when no Root was named.

The separation is deliberate. `/v1/orchestrator/tasks` refuses poll-only with
`422 detached_route_required`, so a Root whose identity lookup failed cannot silently downgrade a
normal Child into a floating task. Conversely, this route returns `422 detached_task_required` if
the task names an owner or omits `poll_only:true`. Scheduled tasks use the broker's internal
schedule path and do not need to pretend that an interactive Session dispatched them.

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/detached-tasks \
    -H "X-Clawdline-Orchestrator: $ORCH" -H 'Content-Type: application/json' \
    -d "{\"task_id\":\"$TASK\",\"secret\":\"$SECRET\"}"
```

`"isolation":"worktree"` asks for a clean private checkout and a delivery branch named
`clawdline/task/<complete-task-id>`. Optional `isolation_base` is resolved to a commit; without it,
the base is `HEAD`. A dirty base succeeds with a warning because its uncommitted files are absent
from the isolated checkout. Unknown isolation values are refused rather than silently sharing the
tree. The checkout lives under `~/Library/Application Support/Clawdline/worktrees/`; `dir` remains
the task protocol directory under `/tmp/.clawdline`.

**What the broker reclaims when the task ends, and when.** Two directories are heavyweight,
reproducible and task-owned, and each has its own registry-internal deadline. These housekeeping
fields are persisted in the broker registry; they are deliberately absent from the public task
shape returned by dispatch and `GET /v1/orchestrator/tasks`:
`<dir>/work/` — the scratch directory a child is told to build in — falls due at
`work_cleanup_at`, and the isolated child's `<worktree.cwd>/.build` falls due at
`build_cleanup_at`. Both deadlines are set the same way from their own setting in
`~/.config/clawdline/config.json`: `orchestrator_work_grace_minutes` and
`orchestrator_build_grace_minutes`, each defaulting to `60`, each accepting `-1…1440`. A `success`
reclaims immediately whatever the setting says; `0` does the same for every terminal outcome; a
positive number is minutes of diagnostic grace, so the failing build log outlives the child that
wrote it; `-1` leaves that directory to the ordinary sweep. A directory that is already gone
settles its deadline as a success, and a removal the filesystem refuses keeps it, so the next beat
tries again. In the registry, `build_cleanup_at` is absent on every task without a worktree of its
own — a shared checkout's build output belongs to whoever is working in it — and, unlike
whole-checkout disposal, it is **not** deferred by `landing.state == pending`: a landing under
review needs the source and the delivery branch, both of which this leaves exactly as they were.

An optional `serialize` array in `task.json` makes named operations machine-global mutexes. A task
leaves `queued` only when it can acquire every name together; shared names are FIFO across roots,
and every terminal state releases all of them. While blocked, the response's task record carries
`"waiting_on":["<task-id>",…]`; the field is absent when nothing blocks it. Queue waiting does not
consume `timeout_minutes`, whose clock starts at `briefedAt`. Cancelling a queued task is immediate.
Queued tasks do count toward the dispatching session's child capacity and the machine-wide
capacity from the moment they are registered.
See [Serializing a machine-global operation](orchestrator.md#serializing-a-machine-global-operation)
for token rules, multi-name atomicity, and restart behavior.

```json
{"ok":true,"task":{"id":"3f9a21bc-…","state":"queued","waiting_on":["a70c5e11-…"]}}
```

An optional `claims` array reserves declared write paths immediately after validation, including
while a serialized task remains `queued`. At registration the broker resolves the existing
`project_dir` once, normalises `.` and empty components in each relative claim, and freezes the
joined absolute strings in the lease. It never resolves the possibly nonexistent claim target,
so its comparison key cannot change when that target is created; `/tmp` and `/private/tmp`
project-root spellings converge at the same point. Frozen keys are compared by component:
equality and ancestor/descendant relationships conflict, but `a/b` does not conflict with `a/bc`.
Conflicts between two definitely identified different roots return the `409 workspace_busy` shape
above. A same-root conflict is admitted and adds a warning beside the task:

```json
{"ok":true,"task":{"id":"3f9a21bc-…","state":"queued",
                    "claims":["Sources/Orchestrator.swift"]},
 "warnings":[{"code":"claims_overlap","task":"a70c5e11-…",
              "paths":["/Users/you/code/clawdline/Sources/Orchestrator.swift"],
              "message":"Task 3f9a21bc-… shares claimed paths with task a70c5e11-…: /Users/you/code/clawdline/Sources/Orchestrator.swift.",
              "age_seconds":42,"root_key":"9f1c2e7a"}]}
```

Every `claims_overlap` and `claims_overlap_unknown_root` warning carries the same `age_seconds`
and `root_key` the `409` above does, computed against the other task named in `task`. `root_key`
is present whenever that task's own root resolves, even inside an otherwise-unknown pair — only a
blocker whose own root cannot be resolved leaves `root_key` absent.

A named assistant that reads `low` rather than `exhausted` warns the same way rather than
refusing — see [`GET /v1/orchestrator/assistants`](#get-v1orchestratorassistants):

```json
{"ok":true,"task":{"id":"3f9a21bc-…","state":"queued"},
 "warnings":[{"code":"assistant_low","assistant":"codex",
              "message":"Codex is at 7d 92% (observed 3 minutes ago). Resets in 5d2h. A long task may not finish.",
              "availability":"low","observed_at":1787744958,"age_seconds":180,
              "resets_at":1788272000}]}
```

An optional boolean `ignore_quota` in `task.json` is the override named in the `409
assistant_exhausted` row above and in that reply's own `message`: an `exhausted` assistant
dispatched anyway with `"ignore_quota": true` warns under `assistant_exhausted` instead of
refusing with the `409`, with the same fields plus a message that says the override was honored —
and, when the account's own reset falls after this task's own `timeout_minutes`, says that too.
Absent or `false` changes nothing; only `low` and `unknown` ever dispatched quietly anyway.

The wire field has three distinct states, preserved through the registry and all GET responses:

| `claims` value | Meaning | Effect |
|---|---|---|
| one or more paths | declared write scopes | freezes and holds those lease keys |
| `[]` | explicit read-only declaration | holds no lease, never conflicts or receives claims `409`, and participates in L1 silence |
| field absent | unknown write scope | holds no lease, retains L1 directory warnings, and the dispatch reply carries a `claims_missing` warning |

`[]` gives read-only work a proactive, harmless way to say so. Silence now has only one meaning:
both sides supplied declarations whose frozen scopes do not intersect; omission says nothing.

**An absent field is answered with a warning, and only an absent one.** 60.7% of the dispatches
measured on one machine declared nothing at all; declaring costs the root about twenty output
tokens, and a collision costs a whole task — three to eighteen million on that same record. So the
reply carries

```json
{"warnings":[{"code":"claims_missing",
              "message":"This task declared no claims, so nothing reserves the paths it is about to write…"}]}
```

on the first dispatch and on the idempotent retry alike. It is never a refusal: a root that has
not worked out its write set yet must still be able to dispatch. **`"claims": []` does not warn** —
it is a positive declaration that the task writes nothing, and warning about it would teach callers
that the field is noise, which is how omission reached 60.7% in the first place.

If either task's root cannot be resolved, the dispatch is also admitted and the warning has the
same fields and message with `code: "claims_overlap_unknown_root"`. An unknown root never has the
authority to hard-block another task.

Every GET record retains the declared `claims`, including an empty array; an absent declaration
remains absent. All terminal states release non-empty leases, including a queued task that reaches
`spawn_failed` in the serialization pump. A task whose field is absent keeps the L1 directory-level
warning described below. Claims are a dispatch gate, not filesystem enforcement; the broker does
not stop a child from writing outside its declaration. Cleanup, rollback, cancellation, and revert
tasks must claim the paths they may change just like entry work does.

A serialized task holds claims throughout `queued`, and that queued interval has no independent
`timeout_minutes` clock. Its practical bound is the serialize blockers reaching a terminal state
(including timeout or cancellation), or cancellation of the queued task itself. A timeout is a
terminal state and releases claims immediately, but its child tab is deliberately left open for
inspection and may still be writing; the root's typed completion line calls out that window.

For an isolated task, relative claims under `project_dir` are discarded and a
`claims_ignored_for_worktree` warning names them: the child writes corresponding paths in its
private checkout. `serialize` remains active and independent for ports, builds, devices, and other
machine-global resources. Because isolated tasks have distinct effective cwd values, they do not
produce L1 warnings merely because their `projectDir` fields name the same repository.

It may also carry `warnings` beside `task`. Besides the same-root `claims_overlap` above, L1 adds
advisory cross-root workspace overlap:

```json
{"ok":true,"task":{"id":"3f9a21bc-…","state":"spawning"},
 "warnings":[{"code":"workspace_overlap","task":"a70c5e11-3b28-4d6f-8e10-2c94b7f0d3aa",
              "dir":"/Users/you/code/clawdline",
              "message":"Task 3f9a21bc-… overlaps active task a70c5e11-… at /Users/you/code/clawdline."}]}
```

In this example the active task's `project_dir` is `/Users/you/code`, while the new task uses its
`clawdline` descendant; `dir` and the path in `message` both name the shared writable descendant.
Each entry names an active task from another dispatch tree whose directory is equal to, an ancestor
of, or a descendant of the new task's directory. Tree identity follows `parent_task` links back to
the same root; an otherwise missing root session id remains unknown and therefore different. The
warning never blocks or delays registration; with no overlaps the entire field is absent, not an
empty array. An idempotent retry uses this same response shape and recomputes currently active
overlaps. Both identifiable roots also receive a best-effort typed line outside the request queue,
so terminal delivery cannot delay the response.

L1 omits a directory-overlap pair when both tasks have a `claims` field (including `[]`) and their
frozen claim scopes do not intersect. That pair is absent both from the dispatch `warnings` array
and from the typed lines sent to the two roots. If either task omitted `claims`, L1 warns exactly as
before. Intersecting non-empty declarations are still handled first by claims arbitration, so a
cross-root conflict between definitely identified roots remains `409 workspace_busy`.

`queued` tasks are not active for this overlap scan: they have not opened a tab or touched a file.
When a serialized task is promoted to `spawning`, the pump runs the scan against the active world
at that moment. Its dispatch response is already gone, so a newly found overlap is sent only as the
same best-effort typed line to identifiable roots; it is not added retroactively to the response.

### `GET /v1/orchestrator/sessions`

The sessions a coordination wait can name, for the credential that registers one. It requires
`X-Clawdline-Orchestrator` and takes nothing else: a paired device gets `403 forbidden`, because it
already reads the whole session list at [`GET /v1/sessions`](#get-v1sessions).

```console
$ curl -s http://127.0.0.1:7717/v1/orchestrator/sessions \
    -H "X-Clawdline-Orchestrator: $ORCH"
{"at":1787758793,
 "sessions":[
   {"id":"35D87610-E7F4-4A9A-95A0-11947CF5115C","assistant":"claude",
    "cwd":"/Users/you/code/clawdline","label":"Clawdline structured messages","state":"working",
    "work_state":"working"},
   {"id":"B3ACDE0D-DE72-4E58-A99A-AB845A539C90","assistant":"claude",
    "cwd":"/Users/you/code/clawdline","label":"the envelope work","state":"idle",
    "work_state":"milestone_complete",
    "taskId":"54ee36cb-7d69-4def-b4d2-fd2a5eb157ad"}]}
```

**This route exists because the wait route was reachable and its arguments were not.**
`POST /v1/orchestrator/waits` takes two terminal-neutral session ids and is behind the orchestrator
token; `GET /v1/sessions`, which lists those ids, is behind a device token and answers the
orchestrator token with a 401. So a root holding the dispatch credential could register a wait and
had nowhere to read the ids it takes.

| field | what it is |
|---|---|
| `id` | the terminal-neutral session id — the same value `GET /v1/sessions` calls `id`, and exactly what `owner_session_id` and `waiter_session_id` take |
| `assistant` | `claude` or `codex` |
| `cwd` | the checkout the session is working in. **Absent** when this Mac could not resolve one, rather than empty |
| `label` | one short line naming the session: a name a person typed for it, else the Clawdline task title when this app opened the tab, else what the conversation calls itself in the assistant's own records, else `⌘<window>-<tab>`. **Never the tab's title** — see [the Session object](#the-session-object) |
| `state` | `working`, `waiting`, `idle` or `unknown` — the terminal state, so a caller knows whether anybody is home |
| `work_state` | the closed, fail-closed broker projection documented on [the Session object](#the-session-object); always present |
| `taskId` | the Clawdline task this tab was opened for. **Absent** for a session a person opened themselves |
| `coordinator` | present only on the one exact process-bound Session registered as coordinator; the closed row projection is `{"label":"Clawdfather","status":"online","commands":[…]}` |

**What a caller may not learn from it.** Nothing a session said, showed or is running: no `line`
(what it is working on), no `menu` (the question a waiting session is asking), no `agents`, no
`shells`, no transcript, and in particular no `sessionId` — the assistant's own conversation id,
which is the *name of its transcript file*. This credential exists to dispatch work, not to read a
terminal, and the exposure line is deliberately one short label short of that. `label` is the one
field that is not purely structural; it is a phrase already drawn on the window title, in the
Dock's window menu and in every switcher on this Mac, and without it two sessions in one checkout
cannot be told apart — which is the case waits exist for.

**Sessions with no assistant in them are not listed.** A wait is delivered by typing a line into
the owner's session, so a plain shell prompt is an address that cannot answer. An empty
`sessions` array is an answer, not a refusal: it means nothing has been read yet, or nothing is
open. `at` is when the reply was built.

For the calling session's own id, use the exact conversation-bound
[`GET /v1/orchestrator/whoami`](#get-v1orchestratorwhoami) resolver below. The UUID after the colon
in `$ITERM_SESSION_ID` is only a cached terminal hint: after iTerm restarts and an assistant resumes,
that environment value can name a terminal which no longer exists. The live registry is
authoritative. [`GET /v1/orchestrator/waits`](#coordination-waits) names only ids already inside a
wait, which is no help to the first session that needs to wait on somebody.

### `GET /v1/orchestrator/whoami`

Resolve one assistant's exact process-bound conversation id to the terminal-neutral id currently
bound to it. This machine-only route requires `X-Clawdline-Orchestrator`; a paired device is
refused `403 forbidden`. Its query is closed and contains exactly one lowercase UUID:

```console
$ curl -sSG http://127.0.0.1:7717/v1/orchestrator/whoami \
    -H "X-Clawdline-Orchestrator: $ORCH" \
    --data-urlencode "conversation_id=01a04b4b-d7a0-7950-bdbc-268e17510cba"
{"conversation_id":"01a04b4b-d7a0-7950-bdbc-268e17510cba",
 "terminal_id":"E7C8E9B9-8DC9-4AA8-9B7A-763B95EA4202","assistant":"codex",
 "provenance":{"source":"live_session_registry","registry_generation":83,
  "registry_complete":true,"registry_observed_at":1787990400,
  "consistency":"single_snapshot_revalidated"},"at":1787990401}
```

`conversation_id` in the response echoes the exact input whose current process binding was proved;
it cannot be normalized or replaced with a different value during resolution.
`terminal_id` is what terminal-addressed routes such as `/send`, `/state`, `/complete`, waits,
`attach_session` and coordinator registration take. Task `root.session_id` is different: it takes
the process-bound conversation id directly. The resolver never matches a terminal id, label, cwd,
state, tab title, context usage or recency, and never guesses among rows. In particular,
`$ITERM_SESSION_ID` is not an input to this route and must not be used as a fallback after a miss.

The consistency boundary is one main-queue publication of the live Session registry followed by
two independent, uncached process-evidence passes before serialization. Each pass takes one fresh
process list and one batched open-file reading for the frozen target population; Claude registry
files (including exact parked-job background entries), hook notes and Codex rollout heads are
freshly read from that evidence. If a detach or rebind changes,
removes or duplicates the match between passes, the route returns `409 session_identity_stale`
with `retryable:true`; an incomplete registry returns `409 registry_stale`. It never combines a
conversation from one observation with a terminal from another, and a success body is closed to
`conversation_id`, `terminal_id`, `assistant`, `provenance` and `at`—never label, cwd, pid or tty.

| `code` | status | meaning |
|---|---:|---|
| `unauthorized` | 401 | no recognised credential was supplied |
| `forbidden` | 403 | a paired device reached this machine-only route |
| `conversation_id_required` | 400 | the closed query is missing `conversation_id`, repeats it or contains an extra key |
| `conversation_id_malformed` | 400 | the value is not one lowercase UUID |
| `conversation_not_found` | 404 | no live exact process binding has that conversation id |
| `conversation_ambiguous` | 409 | more than one live process binding claims the id; none was selected |
| `session_identity_stale` | 409 | the exact binding changed during the two-pass resolution; retry |
| `registry_stale` | 409 | the terminal registry publication is incomplete; retry after a complete scan |

If this assistant genuinely cannot establish its own provider conversation id—an older Claude
installation without the registry/hook, for example—use authenticated
`GET /v1/orchestrator/sessions` as the explicit address-book fallback for terminal-addressed
operations. Select the live assistant row deliberately; do not substitute `$ITERM_SESSION_ID`.
That fallback does not manufacture a task root: `root.session_id` remains a process-bound
conversation id, so an interactive Root that cannot prove one must stop rather than dispatch.
Only unattended automation designed from the start without a Root uses the dedicated
`POST /v1/orchestrator/detached-tasks` route and owns its polling loop.

### `POST /v1/orchestrator/messages`

Relay one assistant session's message to another without attributing it to the person. It requires
`X-Clawdline-Orchestrator` and an `Idempotency-Key`; a paired device gets `403 forbidden` even when
it has the `send` capability. The closed body is:

```console
$ curl -sS -X POST http://127.0.0.1:7717/v1/orchestrator/messages \
    -H "X-Clawdline-Orchestrator: $ORCH" \
    -H "Idempotency-Key: $(uuidgen)" \
    -H 'Content-Type: application/json' \
    -d '{"from_session":"A0939BAC-569B-4B87-9DF4-DE493EC327EA",
         "to_session":"509F54A8-356E-420D-9EAC-73D676C9580E",
         "text":"The correction is in the same round.\n\n## Status\n\nStill running.",
         "images":[{"path":"/Users/you/Desktop/current-state.png"}]}'
{"ok":true,"accepted_at":1787896806,"at":1787896806,"artifacts":[{"id":"46cb6d40-c13f-4fea-9cf0-936f86b78da4","media_type":"image/png","byte_count":18422,"width":1280,"height":720,"expires_at":1787983200}]}
```

`from_session` is either the source's exact terminal-neutral id or its process-bound conversation
id. `to_session` is the target's exact terminal-neutral id. Both must resolve to current Claude or
Codex sessions; titles, prefixes and tty names are never matched, ambiguous source identity fails
closed, and source and target must differ. `text` is 0…100000 characters and may be empty only
beside at least one image. Optional `images` contains 1…6 objects with exactly one normalized,
absolute local `path`. Clawdline never fetches a URL and never trusts an extension or media claim:
each file is bounded, decoded, normalized to PNG and copied into its owned cache before delivery.
Malformed arrays, extra fields, relative/URL/dot-segment paths, unsupported rasters, oversized
source/decoded bytes or dimensions, and storage failures are refused before any terminal send.
A target showing a menu
returns `409 target_busy`, because typing would answer the menu rather than deliver the message.

The route keeps text-only traffic on the strict version-1 `<clawdline-message>` schema. A message
with images uses strict version 2, whose only addition is the bounded `artifacts` metadata array.
Neither wire contains a path, URL or image bytes. Both transcript readers normalize either version
to `role: "message"`, preserve the body's Markdown and expose only the resolved source label and
assistant plus v2 artifact metadata. The complete type inventory and wire schema are in
[`messages.md`](messages.md).
`ok` means the terminal transport accepted one typing attempt. It is not a transcript-observed or
assistant-acknowledged receipt; the idempotency key prevents a network retry from becoming a
second prompt.

| `code` | status | meaning |
|---|---:|---|
| `unauthorized` | 401 | neither a valid machine credential nor paired-device credential was supplied |
| `forbidden` | 403 | a paired device reached the route without the machine credential |
| `bad_request` | 400 | missing idempotency key; malformed/extra fields; empty text without images; bad image count |
| `invalid_image_path` | 400 | an image is not one normalized absolute readable local file path |
| `image_too_large` | 413 | source bytes, normalized bytes, dimensions, pixels or batch bytes exceed a bound |
| `unsupported_image` | 415 | bytes do not decode and re-encode as a supported raster image |
| `artifact_storage_failed` | 500 | the owned cache could not persist the normalized image transaction |
| `source_not_found` | 404 | no unique current source matches the exact terminal/conversation id |
| `target_not_found` | 404 | the target terminal id is not a current assistant session |
| `same_session` | 409 | source and target resolve to the same terminal |
| `target_busy` | 409 | the target is showing a picker/menu |
| `delivery_failed` | 502 | terminal automation could not type the envelope |
| `encoding_failed` | 500 | the closed envelope could not be serialized |

### `POST /v1/orchestrator/sessions/:id/complete`

A root calls this immediately before its final completion response, after the work it claims is
delivered and after any repository-required verification and commit. `:id` is the terminal-neutral
id from the session index above—not a Claude session id or Codex thread id—and the closed request
body contains one bounded sentence:

```console
$ conversation_id='<this assistant process-bound conversation id>'
$ terminal_id=$(curl -fsSG http://127.0.0.1:7717/v1/orchestrator/whoami \
      -H "X-Clawdline-Orchestrator: $ORCH" \
      --data-urlencode "conversation_id=$conversation_id" | jq -er .terminal_id)
$ jq -n --arg summary "Implemented, verified and committed the requested change." \
    '{summary:$summary}' \
  | curl -sS -X POST \
      "http://127.0.0.1:7717/v1/orchestrator/sessions/$terminal_id/complete" \
      -H "X-Clawdline-Orchestrator: $ORCH" \
      -H 'Content-Type: application/json' --data-binary @-
{"ok":true,"created":true,"disposition":{"scope":"session",
 "evidence":"authenticated_session_delivery","receiptAt":1787900400,
 "title":"Implemented, verified and committed the requested change."}}
```

The app resolves the named live target itself and binds the receipt to the exact terminal,
assistant, tty, pid/process start and process-proved conversation. None of those private identity
facts is accepted from JSON or returned. The route is valid only while that root's current turn is
observably `working`; this lets a report made during the last tool call become visible when the
prompt returns to idle without allowing an idle script to mint completion later. An identical
retry is idempotent (`created:false` with the original timestamp).

This produces `milestone_complete`: one check and **delivered, awaiting approval**. Its
`disposition` has `scope:"session"` and `evidence:"authenticated_session_delivery"`. It does not
claim that every descendant, review, landing or deployment obligation in a root's graph is closed,
and it can never produce `work_complete`; two checks still require the task-scoped,
machine-authenticated Git landing receipt. A child tab is refused because its authenticated
`result.json` remains the only completion signal for that assignment.

The first observed idle after the report settles it. When the same terminal next enters `working`
or `waiting`, Clawdline consumes the receipt; current activity already outranks it in that frame,
and a later idle without another report becomes `unknown`. Restarting the app preserves this
lifecycle. A terminal reused by another process cannot borrow the receipt even before cleanup.

Typed refusals are `400 bad_request` for any body other than one string `summary` of 1…500
characters; `401 unauthorized` without a recognised credential and `403 forbidden` when a paired
device reaches this machine-only handler; `404 session_not_found` when
`:id` is not a current assistant target; `409 session_not_working` outside an active turn;
`409 session_unbound` when the complete process/conversation tuple cannot be proved; and
`409 child_session` when the target already has a matching Clawdline task receipt path. A refusal
must be reported honestly; prose does not substitute for the missing receipt.

### `POST /v1/orchestrator/sessions/:id/state`

The `self` half of the work-state provenance boundary ([docs/session-states.md](session-states.md)):
a session's bounded declaration about its own quiet state, bound to the exact current process the
same way `/complete` is, and accepted only while the declaring turn is observably `working`.

```console
$ curl -s -X POST "http://127.0.0.1:$PORT/v1/orchestrator/sessions/$SID/state" \
    -H "X-Clawdline-Orchestrator: $(cat ~/.config/clawdline/orchestrator-token)" \
    -H 'Content-Type: application/json' \
    -d '{"state":"ready","note":"fix landed; can take new work",
         "owed":{"note":"the schedules trade-off is still your call","moved_by":"the user"}}'
{"ok":true,"state":"ready","owed":{"note":"…","since":1787903000}}
```

The body may contain only `state`, `note`, `moved_by`, `person_needed`, and `owed`. `state` may
be **only** `"ready"` or `"holding"`; `"milestone_complete"`/`"work_complete"` are refused
`403 self_completion_refused` — the check states stay evidence-only — and anything else is
`400 bad_request`. A `holding` claim additionally requires `note`, `moved_by`, and
`person_needed: false` (`422 holding_needs_evidence`): holding is never a default, and a mover
who is a person or a session makes the truth a wait. Notes are one line of at most 200
characters.

The `ready`/`holding` claim follows the delivery-receipt lifecycle — settled at the first idle,
consumed when the terminal next enters `working` or `waiting`. The `owed` debt does not: it
survives turns until this route clears it with `"owed": null`, and re-declaring the same debt
note keeps the original `since`. Refusals `401/403/404/409 session_not_working/409
session_unbound` match `/complete`.

### `POST /v1/orchestrator/sessions/:id/closure`

Evidence about a Session that the broker cannot observe, and the second half of
[closeability](session-closeability.md). The broker can see tasks, landings, waits, handoffs,
outbound completions and declared debts; it cannot see which shared-tree hunks this session owns,
what it has on a local list nothing registered, what it deployed outside the repository, or
whether its direct work covers the whole of what it was asked. The machine orchestrator token
authorizes this write. The target in the path says which exact process and observed turn the
assertion is about; the credential does not prove which holder authored it. The operational caller
should therefore obtain the target Session's account first.

```console
$ curl -s -X POST "http://127.0.0.1:$PORT/v1/orchestrator/sessions/$SID/closure" \
    -H "X-Clawdline-Orchestrator: $(cat ~/.config/clawdline/orchestrator-token)" \
    -H 'Content-Type: application/json' \
    -d '{"status":"clear","activity_generation":42,
         "note":"all owned work landed; no local obligation","audit_id":"optional"}'
{"ok":true,"created":true,"attestation_id":"6f0b2d1e-…",
 "closeability":{"state":"safe","reasons":[],"version":"cl1_…", …}}
```

The body may contain only `status`, `activity_generation`, `note` and `audit_id`. `status` must
be `"clear"`; anything else is `422 closure_status_unsupported`, because a session that still owes
something says so by leaving the obligation where the broker can already see it. Identity is
resolved from the live watched process exactly as `/complete` and `/state` do and is never taken
from the body; a terminal whose process cannot be bound is `409 session_unbound`.

**`activity_generation` is what binds this to one turn, and it replaces the same-instant screen
reading.** The session writes this at the end of its turn; whether a SessionWatch beat landed on
the same millisecond is not evidence about anything, and requiring it would be a race the honest
caller loses. Name the wrong turn and the answer is `409 closure_generation_stale`, carrying the
broker's current value so the caller can retry against it. Read the current value from the
`closeability.activity_generation` field of any Session row.

The receipt is **a subject-bound attestation, not caller authentication or completion**. After the
write, the route rereads the real terminal state, inventory evidence and identity multiplicity;
only that broker merge may output `safe`, and it does so only when its own blockers are also clear;
a session that attests while the broker sees
a pending landing gets an honest `blocked` back with its word recorded beside the evidence
(`provenance: ["broker","self"]`). Re-posting the same attestation for the same turn and the same
obligation clock returns `200` with `created:false` and the same `attestation_id`. A later process
in the same terminal is issued its own attestation and never the previous one's.

### Machine coordinator identity and Bearings

Phase A1 adds one explicit, durable machine-scope coordinator identity and read-only Bearings.
Phase A2 adds only a guarded offline reconnect for that same identity. All three routes require the
exact `X-Clawdline-Orchestrator` credential: anonymous callers receive `401 unauthorized`, and a
paired device that reaches the handler receives `403 forbidden`. None of these routes accepts a
device token or task secret.

#### `POST /v1/orchestrator/coordinator/register`

The request has a closed schema with one field. `session_id` is the terminal-neutral `id` read
from [`GET /v1/orchestrator/sessions`](#get-v1orchestratorsessions), not an assistant transcript or
conversation id:

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/coordinator/register \
    -H "X-Clawdline-Orchestrator: $ORCH" -H 'Content-Type: application/json' \
    -d '{"session_id":"35D87610-E7F4-4A9A-95A0-11947CF5115C"}'
{"ok":true,"created":true,"coordinator":{"configured":true,
 "id":"e76f1e87-6de4-4f39-8cc7-c62eef96712f","scope":"machine","label":"Clawdfather",
 "registered_at":1787832000,"status":"online","lifecycle":"standby",
 "session":{"id":"35D87610-E7F4-4A9A-95A0-11947CF5115C","assistant":"codex",
            "label":"coordinate Phase A","cwd":"/Users/you/code/clawdline",
            "work_state":"working"}}}
```

The named row must currently be a Claude or Codex assistant with a complete process-bound identity:
terminal-neutral id, assistant, tty, pid and process start, plus the conversation/rollout proved by
that current process. Those binding facts are stored in
`~/.config/clawdline/coordinator.json` as version 1 using atomic replacement and mode `0600`; they
are never returned by this route. The parent is repaired to `0700` where possible. Registration
holds a machine-local exclusive `flock` on a regular, non-symlink `coordinator.json.lock` (`0600`),
force-reloads after acquiring it, holds it through creation, and reads the written record back
before reporting success. Thus two processes that cached initial absence cannot both create or
overwrite the singleton. Read-side cache validation follows the atomic file's identity and metadata,
so another process's creation or replacement invalidates cached absence. A non-regular/symlinked
store or lock fails closed.

The first registration creates the stable opaque coordinator UUID. Repeating the same identity
returns `200` with `created:false` and the same UUID. “Same process start” uses the canonical
`SessionRegistry.startTolerance`, because the production `Date() - etime` reconstruction can move
slightly between observations; terminal id, assistant, tty, pid and process-proved conversation id
must still match exactly. Drift beyond that tolerance fails closed.

Construction additionally requires a complete SessionWatch observation with its accepted-scan
timestamp. If that inventory is stale, missing, untimestamped, or its clock lies after the
registration lifecycle timestamp, an absent store returns `409 coordinator_liveness_unknown` and
writes nothing. This check precedes candidate absence, so a stale cache that happens not to contain
`session_id` cannot be relabelled `session_not_found`. Durable presence is decided first under the
same lock: an already configured exact identity remains idempotent and a different identity remains
`coordinator_exists`, even when current liveness cannot be asserted.

A different live identity returns `409 coordinator_exists` with the current safe `coordinator`
metadata and does not take over. Registration never doubles as reconnect. There remains no
unconditional replace, delete or stop operation.
`400 bad_request` means malformed JSON, an extra/missing field, or a non-string `session_id`;
`404 session_not_found` means no current assistant row has that terminal-neutral id;
`409 session_unbound` means the row exists but its complete process identity cannot be proved;
`409 coordinator_liveness_unknown` means an absent store was paired with stale, missing or unusable
Session evidence and therefore was not written;
`409 coordinator_store_invalid` means a corrupt or unknown-version durable record was preserved
rather than overwritten; and `500 coordinator_store_failed` means the atomic write did not land.

Registration changes no task parent, depth, cap, root key, owner, wait, landing, terminal `state`,
`work_state`, milestone or completion receipt. Labels, cwd, task ancestry, title words such as
“father”, and most-recent-session order are never identity inputs.

#### `POST /v1/orchestrator/coordinator/rebind`

Reconnect moves the existing stable role to one exact current Claude or Codex process; it does not
construct a coordinator. The request is a closed three-field compare-and-swap shape:

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/coordinator/rebind \
    -H "X-Clawdline-Orchestrator: $ORCH" -H 'Content-Type: application/json' \
    -d '{"expected_coordinator_id":"e76f1e87-6de4-4f39-8cc7-c62eef96712f",\
         "expected_generation":1,\
         "session_id":"8EC30E21-B00F-4806-90C1-D290FD67A87E"}'
{"ok":true,"rebound":true,"coordinator":{"configured":true,
 "id":"e76f1e87-6de4-4f39-8cc7-c62eef96712f","scope":"machine",
 "label":"Clawdfather","registered_at":1787832000,"generation":2,
 "rebound_at":1787832600,"status":"online","lifecycle":"standby",
 "session":{"id":"8EC30E21-B00F-4806-90C1-D290FD67A87E","assistant":"codex",
            "label":"coordinate Phase A2","cwd":"/Users/you/code/clawdline",
            "work_state":"ready"}}}
```

`expected_coordinator_id` and positive integer `expected_generation` must both come from the most
recent Bearings. The UUID identifies the stable role; because it deliberately survives reconnect,
only the generation is the lifecycle compare-and-swap guard. Under the `flock`, either mismatch
returns safe current metadata and refuses mutation.
`session_id` is again the terminal-neutral id, and it must resolve to exactly one live row with the
complete process-bound tuple described under registration. Duplicate rows fail with
`409 session_ambiguous`; no label, recency or partial identity is guessed.

The operation takes the same in-process gate and regular non-symlink `flock`, then force-reloads the
record. A corrupt or unsupported record is preserved. If the expected generation is current and
the candidate already is the exact binding, the call is idempotent (`rebound:false`) even if a
broader scan is stale. A stale generation is never treated as idempotent. Otherwise the
existing process-bound tuple must be absent from a complete current SessionWatch scan. A live exact
old tuple in that current timestamped scan returns `409 coordinator_online`; an incomplete or
untimestamped scan returns
`409 coordinator_liveness_unknown`. `sessionsObservedAt` is the unchanged timestamp taken when
SessionWatch accepted that completed scan—not the later HTTP read time. If no completed-scan time
exists, or it predates the current binding's construction/last reconnect, it cannot disprove the
binding. The process-local scan generation is exposed only as auxiliary provenance and is never the
sole proof, so an app restart resetting it cannot strand a legacy or current record.

A successful reconnect atomically writes and reads back the new private binding while preserving
the coordinator UUID and original `registered_at`. It increments `generation`, records
`rebound_at`, removes the optional role from the old process, and projects it only on the exact new
process. Version-1 A1 records without lifecycle fields decode as generation 1. The response and
Bearings expose only safe lifecycle metadata; tty, pid, process start and conversation id remain
private. A reconnect timestamp must be at least both the prior binding-change timestamp and the
completed scan it consumed. Clock rollback therefore fails closed instead of lowering the freshness
barrier. `409 coordinator_identity_mismatch` or `409 coordinator_generation_mismatch` asks the
caller to refresh Bearings;
`409 coordinator_not_configured` requires explicit registration; and store/unbound errors retain
the meanings above. Reconnect does not start a tab, wake a model, resume a transcript, grant
transcript access, dispatch work, mutate git, stop a process or delete anything.

The private binding history also keeps terminal completion routing safe across this move. An old
task authenticates its alias interval with its historical `root.assistant`, but the outbox resolves
and targets the replacement binding's canonical conversation id and current assistant. Both
Codex-to-Claude and Claude-to-Codex reconnects therefore bypass the stale assistant rather than
dead-lettering a valid completion or transporting to the old process.

#### `GET /v1/orchestrator/coordinator`

This returns durable presence and deterministic read-only Bearings:

```jsonc
{
  "version": 1,
  "observed_at": 1787832060,
  "store": {"status": "ready"}, // absent | ready | corrupt | unsupported
  "registration": {"state": "configured"}, // available | configured | blocked
  "coordinator": {
    "configured": true,
    "id": "e76f1e87-6de4-4f39-8cc7-c62eef96712f",
    "scope": "machine", "label": "Clawdfather",
    "registered_at": 1787832000,
    "generation": 2, "rebound_at": 1787832600,
    "status": "online",         // online | offline | unknown
    "lifecycle": "standby",    // standby | offline | unknown
    "session": {
      "id": "35D87610-E7F4-4A9A-95A0-11947CF5115C",
      "assistant": "codex", "label": "coordinate Phase A",
      "cwd": "/Users/you/code/clawdline", "work_state": "working"
    }
  },
  "bearings": {
    "observed_at": 1787832060,
    "coordinator_lifecycle": "standby",
    "work_state_counts": {
      "ready": 0, "working": 3, "waiting_you": 1, "waiting_session": 1,
      "unknown": 2, "milestone_complete": 1, "work_complete": 0
    },
    "closeability_counts": {
      "blocked": 1, "needs_attestation": 0, "safe": 1, "unknown": 1,
      "not_projected": 1
    },
    "active_task_count": 2, "pending_landing_count": 1, "open_wait_count": 1,
    "pending_landings": [{"id":"3f9a21bc-…","root_key":"9f1c2e7a",
      "ownership":{"version":1,"status":"observed_working","subject":"root",
        "task_id":"3f9a21bc-…","task_state":"success","root_key":"9f1c2e7a"}}],
    "unknown": [{"id":"…","assistant":"claude","label":"…","cwd":"…",
                       "work_state":"unknown","closeability_state":"unknown"}],
    "waiting": [{"id":"…","assistant":"codex","label":"…","cwd":"…",
                  "work_state":"waiting_session"}],
    "blocking": [{"id":"…","assistant":"claude","label":"…","cwd":"…",
                   "work_state":"working"}],
    "sources": {
      "sessions": {"observed_at":1787832060,"generation":42,
                   "provenance":"session_watch","freshness":"current"},
      "tasks": {"observed_at":1787832060,"provenance":"orchestrator_task_registry","freshness":"current"},
      "landings": {"observed_at":1787832060,"provenance":"orchestrator_landing_registry","freshness":"current"},
      "waits": {"observed_at":1787832060,"provenance":"orchestrator_coordination_wait_registry","freshness":"current"}
    }
  }
}
```

All eight `work_state_counts` keys are always present. `closeability_counts` always carries the
four closeability states plus `not_projected`, keeping absent projection distinct from doubtful
`unknown`. Bearings rows use the reduced string key `closeability_state`; full Session routes use
the object key `closeability`, so one key never changes type. Active tasks are non-terminal task records;
pending landings are task landings in `pending`; open waits count durable wait groups with at least
one unreleased waiter. `pending_landings` is the same ordered row set returned by
`GET /v1/orchestrator/landings`, including its fail-closed owner/executor projection; the count and
rows therefore come from the same registry snapshot. `unknown` selects that work state, `waiting` selects
`waiting_you`/`waiting_session`, and `blocking` selects live owner sessions with waiters. Each
named row is limited to terminal-neutral `id`, assistant, cwd, label and `work_state`. These are
independent filters, not a partition: a session that is both waiting on a peer and owns another
peer's wait appears in both `waiting` and `blocking`.

SessionWatch is observed once, then all Orchestrator-derived per-session work/ownership facts and
the task/landing/wait totals are built under one Orchestrator registry lock window. The two sources
cannot be transactional together: their distinct `observed_at`, provenance and freshness fields
state that boundary instead of advertising an all-source instant. An incomplete terminal scan marks
only the Session source `stale`; missing facts are not guessed. These coordination reads never wait
indefinitely for the main queue: one single-flight bounded SessionWatch read refreshes a cache (so
repeated reads cannot accumulate main-queue work), and an unavailable
read uses stale cached evidence or explicit `missing`. Either degraded state keeps durable registry
rows and makes ownership `unknown`; it never turns missing observation into absence.
Coordinator liveness uses that same evidence boundary. A valid binding is `online` only when an
exact process appears in a complete timestamped current inventory; it is `offline` only when such
an inventory, no older than the binding, proves the tuple absent. Stale, missing, untimestamped or
pre-binding evidence yields `status:"unknown", lifecycle:"unknown"`, retains the last safe session
metadata, and cannot authorize reconnect. An absent, corrupt or unsupported record produces
`configured:false, status:"unregistered", lifecycle:"unregistered"`; corruption never elects a
replacement. No response contains a transcript, transcript path, assistant conversation id, tty,
pid or process start.

Because those three stores spell one identical `coordinator` tuple, that tuple cannot answer
*may this caller register*. `registration.state` answers it, from the same durable read the
registration refusal is made on, in exactly one of three words:

| `registration.state` | Durable store | What a client may do |
| --- | --- | --- |
| `available` | no record | offer registration; it writes over nothing |
| `configured` | a valid record, `status` `online`, `offline` **or** `unknown` | do not offer; every value is the same durable owner |
| `blocked` | unreadable, unparseable, or a version this build does not know | do not offer, ever; `POST /v1/orchestrator/coordinator/register` refuses it `409 coordinator_store_invalid` and the bytes are preserved |

The vocabulary is closed, so a client may switch on it exhaustively — and must treat a missing
field or a fourth word as `blocked` rather than as permission. It is a projection of store health
and nothing else: no coordinator id, path, token, stored bytes or corruption text crosses with it,
and `blocked` never says which of the three failures it was. `store.status` (`absent`, `ready`,
`corrupt`, `unsupported`) stays on this machine-token surface as before.

#### `GET /v1/orchestrator/coordinator/bearings`

The device-readable half of the same answer. It accepts ordinary device auth (the orchestrator
token also works). The Clawdfather controls panel uses it for four read-only commands, and the
new-Session creation sheet reads it when opening and again immediately before it sends a
registration-only instruction. The full inspection above stays machine-token-only.

The body is a strict subset of the full inspection, built as an allowlist — a field added to the
full answer never reaches this one by omission. What survives: `version`, `observed_at`;
`registration` reduced to its one closed `state` word, re-validated on the way out so an
unrecognised value leaves as `blocked`; `coordinator` reduced to `configured`, `label`, `scope`,
`status`, `lifecycle` and the safe `session` row (`id`, `assistant`, `label`, `cwd`,
`work_state`, optional `closeability_state`); `bearings` with its
`observed_at`, `coordinator_lifecycle`, `work_state_counts`, `closeability_counts`, the three counts,
`pending_landings`, the
`unknown`/`waiting`/`blocking` rows (the same fields), and `sources` reduced to
`observed_at` and `freshness` per source. Pending landing rows, their ownership object, the three
evidence sources and each source field are separately allowlisted; no opaque nested dictionary is
copied. Those rows are already readable by the same paired-device capability at the landing GET.

Deliberately withheld, beyond Bearings' own exclusions (tty, pid, process start, conversation
ids): the durable coordinator UUID, `generation`, `registered_at` and `rebound_at` — the
compare-and-swap bookkeeping of the machine-token rebind flow, which a device cannot use —
plus `store` health, top-level source `provenance` names and the session-watch `generation`
counter. Pending-row ownership evidence retains only its fixed source names and the explicit
`observed_at`, `generation`, `provenance`, and `freshness` fields needed for comparison; repository
paths and process/transcript evidence cannot cross that projector.
`registration.state` is the one thing derived from `store` health that does cross, because it is
what a device needs in order not to ask for something the machine would refuse; it is one of
three words and discloses nothing about the store behind it.

The optional `session.coordinator` record is projected on both `GET /v1/sessions` and
`GET /v1/orchestrator/sessions` for the exact bound row only. It advertises
`status_report`, `duplicates_conflicts_ownership`, `landing_closure` and `scope_permissions` with
`enabled:true`: the panel answers them from `GET /v1/orchestrator/coordinator/bearings` above.
It also advertises `deep_status_audit` with `enabled:true`. When the exact coordinator is online
and the browser has current send/write capability, its first press previews the high-token,
multi-session request and its explicit second press uses `POST /v1/sessions/:id/send`. Offline or
without that capability, the row remains visible and says why it is unavailable.

Every command row, enabled or disabled, carries `token_effort` (`low`, `medium`, `high`, or
`unknown`) and `token_effort_basis` (`registry_read`, `unbuilt`, `spawns_session`,
`single_session_message`, `broker_only`, or `session_fanout`). These fields describe relative
expected Token work, never observed usage or money. The four Bearings reads are
`low/registry_read`; since-away, coordinate-work and quiet-watch are `unknown/unbuilt`; dispatch is
`high/spawns_session`; ask is `medium/single_session_message`; stop and reconnect are
`low/broker_only`; deep audit is `high/session_fanout`. Clients must render a missing or unknown
effort value as `unknown`, never `low`, and must show effort independently of command availability.

Every remaining command that would send, spawn or mutate stays `enabled:false` and carries two fields:
`reason`, a closed code the client says in its own language — `no_return_ledger` (since_away),
`no_command_route` (coordinate_work, ask_coordinator, quiet_watch, stop), `device_cannot_spawn`
(dispatch_independent_work) and `machine_token_only` (reconnect) — and `why`, honest English
prose kept for pages that predate the codes. A client that knows the code ignores the prose; one
that does not still shows a true sentence. Reconnect is deliberately machine-token-only; it is
not the registration-only new-Session creation helper, whose local assistant registers itself
only when no coordinator is configured and never rebinds an offline owner. That helper's switch
in the browser is enabled by `registration.state === "available"` alone.
Dispatch would start a session from a device, which is the one thing
the device/orchestrator credential split exists to prevent. The record is absent from every
ordinary row, preserving their old JSON behavior.

This first deep-audit slice is agent-driven. The instruction asks Clawdfather to snapshot sessions,
tasks, landings and waits; question relevant idle/root Sessions; wait to a bounded deadline; reread
the registries; and report four separate sections plus distinct degradation states. There is no
persistent broker audit-run/probe protocol yet, and this API does not claim one.

#### Proposed observer provenance for any future liveness action

A refusal seen from a sandbox's loopback domain is only `observer_unreachable`, never `app_down`.
Any future restart proposal must first corroborate it with both host listener/process proof and a
host-network health probe, recording each observer domain and provenance—for example
`sandbox_loopback`, `host_listener`, and `host_health`. One failed observer cannot authorize restart.
This is a proposed restart policy, not a current restart capability. Phase A2's narrow reconnect
only rebinds a provably offline role to an already live exact process; it does not restart, start,
stop, health-to-action, or execute a web command.

### Coordination waits

`POST /v1/orchestrator/waits` registers one durable cross-session file wait and delivers its
request to the owner through Clawdline. It requires `X-Clawdline-Orchestrator`:

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/waits \
    -H "X-Clawdline-Orchestrator: $ORCH" -H 'Content-Type: application/json' \
    -d '{"repository":"/Users/you/code/clawdline","paths":["Sources/Foo.swift"],
         "owner_session_id":"OWNER-TERMINAL-ID","waiter_session_id":"WAITER-TERMINAL-ID",
         "reason":"the owner has an unfinished diff","release_condition":"commit or explicit release"}'
{"ok":true,"deduplicated":false,"wait":{"id":"0d9579fb-…","repository":"/Users/you/code/clawdline",
 "paths":["Sources/Foo.swift"],"ownerSessionId":"OWNER-TERMINAL-ID",
 "releaseCondition":"commit or explicit release","createdAt":1787740810,"waiters":[…]}}
```

Both session ids are terminal-neutral session ids, and
[`GET /v1/orchestrator/sessions`](#get-v1orchestratorsessions) is where a caller holding this
credential reads them. `GET /v1/sessions` lists the same `id` values and will not answer the
orchestrator token — it is the paired-device route and returns `401 unauthorized`.
`repository` must be an absolute path other than `/`; every path must resolve below it. Clawdline stores canonical
repository-relative paths, sorts them and removes duplicates. The same owner, repository, path set,
release condition and waiter is idempotent even without an HTTP idempotency key: it returns the
existing group with `deduplicated: true` and does not type the request again after its delivery
receipt. Another waiter joins that release group and receives its own request delivery. The typed
terminal line is a version-2 `file_wait_request` notice carrying `wait_id`, `repository`, `paths`,
`waiter_session_id`, `reason`, and `release_condition`; its body keeps the full operational request.

A `waiter_session_id` this Mac cannot find is `404 waiter_not_found` — it is resolved against the
same session list `GET /v1/orchestrator/sessions` publishes, so an id that route did not name is an
id this one will not take.

Before typing a request, Clawdline checks whether the owner's cached state is `waiting` **and**
`Targets.isChoosing` confirms that a menu is on screen. If both are true, it returns
`409 owner_busy`: nothing was sent, the durable request remains undelivered, and retrying the same
registration is safe. This is narrower than the name suggests. A session that is merely working,
holds a half-typed line, or is invisible to this Mac reads as ready; invisibility deliberately
continues to delivery so an absent owner becomes `502 request_delivery_failed`, meaning a send was
attempted and did not arrive, rather than being misreported as busy.

The registry row is written before terminal delivery. If the owner cannot be reached, the route
returns `502 request_delivery_failed` with the durable `wait` in the error; retrying the same body
attempts only the missing request delivery. `GET /v1/orchestrator/waits` lists every unresolved group
under `waits`. Like task reads, it accepts either the orchestrator credential or an ordinary paired
device with read permission, because the same relationships are already present on Session rows.

After the release condition is true, the recorded owner calls the release route:

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/waits/$WAIT/release \
    -H "X-Clawdline-Orchestrator: $ORCH" -H 'Content-Type: application/json' \
    -d '{"owner_session_id":"OWNER-TERMINAL-ID","commit":"abc123","note":"tree rechecked"}'
{"ok":true,"id":"0d9579fb-…","released":2}
```

`commit` and `note` are optional; the owner id is not. Clawdline sends every active waiter a release
notice naming repository, paths, commit/note when present, and the requirement to re-check HEAD,
status and diff. On the wire it is a version-2 `file_wait_release` notice with typed `wait_id`,
`repository`, `paths`, optional `commit` and optional `note`; the body retains that Git safety
instruction. Each successful delivery is persisted. If any delivery fails, the route returns
`502 release_incomplete` with `sent` and `pending`; the group stays registered and retry sends only
to the pending recipients. It is removed only after all receive the notice. `403 wrong_owner`
means the supplied owner differs from the persisted one.

A waiter abandoning its work calls `POST /v1/orchestrator/waits/:id/cancel` with
`{"waiter_session_id":"WAITER-TERMINAL-ID"}` and the orchestrator credential. It removes only that
waiter's membership; `403 not_waiter` protects every other obligation. No route infers release from
Git state, and an owner session disappearing does not remove a wait.

### `GET /v1/orchestrator/schedules`

Lists every JSON source under `~/.config/clawdline/schedules/`. A paired device, including a
read-only device, uses this same GET route; the orchestrator token works too. Local-time
calculations are returned as epoch seconds:

```json
{
  "schedules": [{
    "id": "4d2f54ce-b4b5-4f60-8623-34011f35aa43",
    "title": "Publish the next post",
    "enabled": true,
    "next_fire": 1787880600,
    "last_missed_at": 1787707800,
    "last_run": { "task_id": "...", "state": "success", "at": 1787794200 }
  }, {
    "file": "broken.json",
    "state": "invalid",
    "error": "when must contain exactly at and days",
    "error_kind": "schema"
  }],
  "at": 1787797800
}
```

`last_missed_at` is absent until an occurrence expires outside its catch-up window. It is an
independent schedule fact, not a `last_run.state`. `last_run` is absent before the first dispatched task and may become absent again after the
200-record registry retention limit removes it. Invalid rows never expose the task template;
`error_kind` is `project_unavailable` for a temporarily missing `project_dir`, `schema` for a
validation error, or `unreadable_json`. Invalid content is audited at most once per file revision
and sends one push on first discovery when `notify_on_failure` is true (or absent, whose default is
true). The file format, shared capacity bucket and retry boundary are in
[`schedules.md`](schedules.md).

[`POST /v1/orchestrator/schedules`](#post-v1orchestratorschedules) below creates one,
[`PATCH /v1/orchestrator/schedules/:id`](#patch-v1orchestratorschedulesid) rewrites one, and
[`DELETE /v1/orchestrator/schedules/:id`](#delete-v1orchestratorschedulesid) removes it — all
three behind the write gate, which is worth saying plainly: **a paired phone can now change and
remove work that runs later, unattended**, where before a wrong time could only be fixed at the
Mac in a text editor. The Settings app can also change an existing source file's top-level
`enabled` boolean with one switch; every other field remains user-owned, editable by hand, and
rewritten wholesale by `PATCH` rather than key by key.

### `GET /v1/orchestrator/schedules/:id`

One schedule in full, under `schedule`. Everything the list row carries, plus `file`, `when` in the
file's own spelling, `close_tab`, `catch_up_hours`, `notify_on_failure`, the whole `task` template —
`project_dir`, `instructions` and all — and retained `runs`, newest first. The list deliberately
exposes none of that, which is the right amount for a row and the wrong amount for the only screen
where somebody can check what a schedule actually does. Same door as the list: `read` is enough,
and the orchestrator token works too. `404 not_found` for an id that is unknown, invalid, or not an
id at all.

```json
{"schedule":{"id":"4d2f54ce-…","title":"Publish the next post","enabled":true,
             "file":"4d2f54ce-….json","next_fire":1787880600,
             "when":{"at":"09:30","days":["mon","wed","fri"]},
             "close_tab":"on_success","catch_up_hours":6,"notify_on_failure":true,
             "task":{"assistant":"codex","project_dir":"/Users/me/code/blog",
                     "instructions":"Publish the next ready post."},
             "runs":[{"task_id":"8ef0…","state":"success","assistant":"codex",
                      "project_dir":"/Users/me/code/blog","created":1787794200,
                      "finished_at":1787794322,"summary":"Published post 42.",
                      "terminal_id":"9A1F…","session_id":"105344fb-c769-4b37-b766-403b410897eb"}]}}
```

Every run remains visible while its task record is retained. `terminal_id` names the tab Clawdline
opened and lets a client go to it if it is still on `/v1/sessions`. `session_id` is stricter: it is
present only for terminal work whose transcript or rollout still exists and is proved to belong to
that exact task. A run that was dispatched into a standing session instead of a tab of its own
carries `attached: true` and `attach_session`, the terminal-neutral id it was typed into — the
same pair the task record calls `attached` and `attachSession`, spelled the way the rest of a run
record is spelled. It may be sent to the place resume route above; a run without it is readable but
not resumable. `runs_may_be_truncated: true` appears when the machine-wide registry has reached its
200-record retention boundary, because older occurrences may already have lost their schedule
association.

### `POST /v1/orchestrator/schedules`

Makes a schedule file. `PATCH` and `DELETE` below are the other two ways a file gets written; this
is the only one that brings a schedule into existence.

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/schedules \
    -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
    -H 'Idempotency-Key: 3f9a1c04-77e2' \
    -d '{"title":"Publish the next post","at":"09:30","days":["mon","wed","fri"],
         "place_id":"3f2a91c47e0b5d68","assistant":"codex",
         "instructions":"Read the checklist and publish the next ready post."}'
{"ok":true,"schedule":{"id":"4d2f54ce-…","title":"Publish the next post","enabled":true,"next_fire":1787880600},"dispatch_enabled":true}
```

Required: `title`, `at` as `HH:MM` in the Mac's local time, `days` as `"daily"` or a non-empty
weekday array, `place_id`, `assistant`, `instructions`. Optional: `enabled` (default `true`),
`close_tab`, `catch_up_hours`, `notify_on_failure`, `timeout_minutes`, `model`. An empty `model`
is left out of the file rather than written into it. `days` has no default — picking `daily` for
somebody would be choosing how often their work runs — while `enabled` does, because a schedule
somebody has just asked for is on.

**`dispatch_enabled` sits beside the schedule, not inside it.** It is not a field of the file — it
is this Mac's `Settings → Remote → Agent tasks → Let a session hand work to another`, read at the
moment the schedule was made. `false` means exactly this: *the file is written, it is valid, and
nothing will run it until that switch is on.* Making one is deliberately **not** gated on it —
writing a file is not dispatching, and refusing would be this route deciding what somebody may
arrange for later — so the answer is still `200` with `ok: true` and the schedule is really there.
It is absent from a refusal, because a refusal made nothing to say it about, and it is not on
`PATCH` either: an edit is a change to a file that already exists, and the create is where somebody
is told what they have just arranged.

**The write gate, not the orchestrator token** — [all three gates](#writing-three-gates-in-this-order),
like `/send` and `/v1/voice`. The orchestrator token is a `0600` file on this Mac, which is what
makes it a proof of being local, and a phone cannot have one; this route is for the phone. Sending
the orchestrator header in place of a device token is refused as a device that may not send.

**A `place_id`, never a path.** It is an id from [`/v1/places`](#get-v1places), resolved against
that list on the Mac. There is nowhere in the body to write a directory, and `project_dir`,
`claims`, `permission_mode` and every other task-template field are refused as unknown fields
rather than honoured. What this does add is stated plainly in
[`schedules.md`](schedules.md#making-one-without-a-text-editor): a paired phone can arrange work
that runs later with nobody watching.

| | when |
|---|---|
| `400 bad_request` | no `Idempotency-Key`; an unknown field; a `place_id` that is not on the list; or any field the [schedule parser](schedules.md#the-file) refuses — the refusal carries that parser's own sentence |
| `401 unauthorized` | no token, or one this Mac does not know |
| `403 write_disabled`, `403 forbidden` | the write switch is off; or this device may read and not send |
| `429 rate_limited` | ten schedules have been made in the last ten minutes. A sliding window of counted writes, like the dispatch brake and unlike `busy`, which is a queue with something already in it |
| `500 write_failed` | the file could not be written, or could not be read back through the parser afterwards — in which case it has been removed |

The answer is filed under the `Idempotency-Key` like every other write, **except `429` and the
two `500`s**: those are facts about this machine at this moment rather than about the request, and
a cached one would tell the retry that was supposed to work that the brake is still on long after
it let go. The audit line is `orchestrator.schedule.created`, written whichever way it goes.

Every schedule is validated by being assembled into a source file, parsed, written, and then
**read back off disk through the same parser** before the request is answered. A file this app
cannot itself parse never survives the request that made it.

**`next_fire` in the answer is the next run, and nothing sooner.** The file this route writes
carries a `created_at` of its own — Unix seconds, written by this route and nameable by nobody —
and the minute timer ignores every occurrence older than it. Without that, "09:00 daily" arranged
at one in the afternoon is inside its own six-hour catch-up window: a session would open within the
minute for a morning nobody had asked it to catch up on, and arranged after three it would instead
push that a run had been missed and show `last_missed_at` on a schedule made a minute earlier. A
file written by hand has no `created_at` and still means *as far back as anyone knows*, which is
the behaviour every schedule file has always had.

### `PATCH /v1/orchestrator/schedules/:id`

Rewrites one schedule that already exists, from the body `POST` takes.

```console
$ curl -s -X PATCH http://127.0.0.1:7717/v1/orchestrator/schedules/4d2f54ce-… \
    -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
    -H 'Idempotency-Key: 8c1b7e30-44a9' \
    -d '{"title":"Publish the next post","at":"07:05","days":"daily",
         "place_id":"3f2a91c47e0b5d68","assistant":"codex",
         "instructions":"Read the checklist and publish the next ready post."}'
{"ok":true,"schedule":{"id":"4d2f54ce-…","title":"Publish the next post","enabled":true,"next_fire":1787905500}}
```

**A save, not a patch of individual keys.** The whole file is rewritten, so a field the body may
name and leaves out goes back to the parser's default rather than staying as it was — read the
current one with [`GET /v1/orchestrator/schedules/:id`](#get-v1orchestratorschedulesid), which
exists for exactly this, and send it back with the changes in it. Required and optional fields,
the `place_id`, the gates and every refusal are the create route's, because both assemble the same
object and hand it to the same parser: an edit is not a way to write a file a create would not
write.

**The task-template fields no form has a control for are the exception, and they are carried
rather than defaulted**: `claims`, `permission_mode`, `serialize`, `isolation`, `isolation_base`,
`deliverables`, `kind`, `plan`, `model` and `reasoning_effort` are read off the file being replaced. A save that
dropped them would be an edit changing what it never put on screen — measured, `claims` and
`permission_mode` were gone after one save from either surface. `model` is the one of those a body
may still name, so the two spellings are held apart: **a body with no `model` key keeps the model
that is there, and `"model": ""` takes it off.** The page's form sends no `model` key at all,
which is why folding those together would hand a `"model": "opus"` schedule back running the
assistant's default.

**What this widens, said plainly.** A phone cannot name `permission_mode` or `claims`, cannot add
either to a schedule that has none, and cannot raise the permission of one it is editing — but it
can now rewrite the title, the times and the **first message** of a schedule that already runs
with `"permission_mode": "full"`, and those instructions run under that permission. The
alternative was a save that silently stripped the field, which is worse; the trade is real and is
written down rather than left in a commit message.

**`schedule_id`, `created_at` and `when_changed_at` are the Mac's and are not fields this request
may carry** — naming any of them is a `400` for an unknown field, like `project_dir`. The last two
matter beyond tidiness. `created_at` is what makes the minute timer ignore an occurrence older
than the schedule, so a save that restamped it would make editing a `09:00` schedule at lunchtime
open a session for this morning. A hand-written file with no `created_at` does not acquire one by
being edited.

`when_changed_at` is the other half of that, and the answer to a bug carrying `created_at` across
leaves open: a schedule made last week is a week old however its times move, so its age cannot say
whether this morning's nine o'clock is a run this Mac slept through or one this save invented a
minute ago. Measured, moving `21:00` to `09:00` at 14:00 dispatched today's nine within the minute
— while the `200` below said `next_fire` was tomorrow — and the same edit at 17:00 pushed
"Scheduled run missed its catch-up window" instead. A save that moves `at` or `days` now stamps
this instant into the file and the timer ignores every occurrence older than it; a save that moves
neither carries the old stamp across untouched, so a run that really was missed at nine is still
missed after somebody fixes the title at eleven. Retiming the file by hand writes no stamp and
behaves as it always has.

For Codex schedules, a carried `reasoning_effort` remains exactly `high` or `xhigh`; omission
continues to mean no Codex CLI override. The ordinary parser still refuses empty, non-string and
unknown values. If an edit explicitly changes the assistant from Codex to Claude, the Mac removes
that now-incompatible hidden override so the assistant change can be saved without a text editor.

| | when |
|---|---|
| `400 bad_request` | no `Idempotency-Key`; an unknown field, `schedule_id`, `created_at` and `when_changed_at` among them; a `place_id` that is not on the list; or any field the [schedule parser](schedules.md#the-file) refuses, carrying that parser's own sentence |
| `401 unauthorized` | no token, or one this Mac does not know |
| `403 write_disabled`, `403 forbidden` | the write switch is off; or this device may read and not send |
| `404 not_found` | no schedule with that id, an id that is not an id, or a source file this app cannot itself parse — see below |
| `429 rate_limited` | a save spends the same ten-in-ten-minutes ticket a create does |
| `500 write_failed` | the change could not be written, or could not be read back through the parser afterwards — in which case **the previous file has been put back** |

That last row is the one difference from `POST` worth knowing: a create that cannot be read back
deletes what it wrote, while a save that cannot be read back restores what was there. The schedule
somebody already had is not a failed save's to lose.

The `404` for a file this app cannot parse is deliberate. An edit replaces the whole file, an
invalid one has no `created_at` worth carrying, and the list gives an invalid row a `file` and no
`id` to address it by — so removing it and creating a new one is the repair. `DELETE` needs no
such understanding.

Answers are filed under the `Idempotency-Key` except `429` and the `500`s, for the same reason as
on `POST`. The audit line is `orchestrator.schedule.updated`.

### `DELETE /v1/orchestrator/schedules/:id`

Removes one schedule file.

```console
$ curl -s -X DELETE http://127.0.0.1:7717/v1/orchestrator/schedules/4d2f54ce-… \
    -H "Authorization: Bearer $TOKEN" -H 'Idempotency-Key: 5d0e2a91-6b17'
{"ok":true,"deleted":"4d2f54ce-b4b5-4f60-8623-34011f35aa43"}
```

| | when |
|---|---|
| `400 bad_request` | no `Idempotency-Key` |
| `401 unauthorized` | no token, or one this Mac does not know |
| `403 write_disabled`, `403 forbidden` | the write switch is off; or this device may read and not send |
| `404 not_found` | there was no such schedule — including an id that is not an id at all |
| `500 delete_failed` | the file is there and would not go |

**Those last two are different answers on purpose.** Reporting a file that will not go as a `404`
would tell a caller the schedule is gone while it is still on disk and still firing.

The contents are never read: a file named after a UUID that this app cannot parse is exactly the
one somebody most wants removed. A file whose *name* is not a UUID — `broken.json` — has no id to
address it by and goes from the Finder instead.

Unlike `PATCH`, this is not braked; it leaves nothing behind to sweep up and it is what somebody
reaches for when they want work to stop. Every answer but the `500` is filed under the
`Idempotency-Key`, so the retry a phone makes after changing networks replays the `200` rather
than being told there is nothing there. The audit line is `orchestrator.schedule.deleted`.

**Removing a schedule is not cancelling its task.** A task already dispatched keeps its own id,
tab and record; [`POST /v1/orchestrator/tasks/:id/cancel`](#post-v1orchestratortasksidcancel)
stops it. Neither `PATCH` nor `DELETE` is refused while a task from the schedule is live — that is
the one place they part company with `…/run` and its `409 schedule_active`, which is about
stacking a second session on a first. An occurrence the minute timer has already chosen is
re-checked against the file before it opens anything, so a schedule removed in that second opens
nothing and is audited as `orchestrator.schedule.skipped` with `why=removed`.

### `POST /v1/orchestrator/schedules/:id/run`

Runs one valid schedule immediately, ignoring `enabled` and the wall clock. This needs
`X-Clawdline-Orchestrator`; a successful response is the ordinary dispatch response. It returns
`404 not_found` for an unknown or invalid schedule, `403 orchestrator_disabled` when dispatch is
off, and `409 schedule_active` while any task from the schedule is non-terminal or its dispatch is
already queued. A successful manual run records the current occurrence as handled when it is at or
after that occurrence; running before the next scheduled time does not consume that future fire.

### `POST`, `GET`, `DELETE /v1/orchestrator/maintenance/restart`

This is the durable drain receipt a replacement command must obtain before swapping the app. POST
accepts the closed body `{"request_id":"<lowercase UUID>"}` and immediately closes terminal
mutation admission. Operations already admitted keep running; the receipt counts them globally and
by terminal. Poll GET until `phase:"ready"` and `safe_to_replace:true`:

```jsonc
{"restart":{"request_id":"b2f073d6-…","phase":"ready",
  "requested_instance_id":"9af84fc1-…","requested_at":1787100200,
  "outstanding":0,"channels":{},"safe_to_replace":true,
  "admission_closed":true,"replacement_before_safe":false,"drained_at":1787100202}}
```

`queued` work with a recoverable sealed secret and every `briefed` task are durable and do not
block. A `spawning` task, or queued work whose sealed secret is unavailable, returns
`409 restart_blocked_by_task_secret` with typed `blockers`; replacing then would lose the only
plaintext briefing secret. Repeating the same request id is idempotent, including after completion.
A different live request returns `409 restart_in_progress` without stealing its admission gate.

DELETE accepts the same closed body and explicitly aborts the matching maintenance window. The
aborted fact is persisted as `phase:"aborted"` with `aborted_at`, then admission reopens. A different
id is refused with `409 restart_in_progress`; a missing receipt is `404 restart_not_found`. This is
the exit when an operator changes course or a drain cannot converge—restarting is never the only
way to reopen the broker.

After replacement, the persisted receipt moves to `reconciling`. Terminal mutations continue to
return `503 restart_maintenance` with `retryable:true`, `request_id`, and `retry_after` inside the
standard `error` object. Exact matches may settle on the first fresh complete inventory. Absence or
identity mismatch requires two different complete generations in one process epoch spanning at
least 60 seconds. Reconciliation is bounded at 120 seconds: admission then reopens with
`reconciliation_timed_out:true` and the exact `unresolved_task_ids`, while ordinary task watching
continues to seek typed evidence. GET reports `phase:"complete"`, the current replacement's
`resumed_instance_id`, and `reconciled_at`. A second replacement updates that instance and time
again. If replacement happened before the drain receipt was safe,
`replacement_before_safe:true` remains durable rather than being hidden.

The closed route has these maintenance-specific codes: `400 bad_restart_request` for malformed or
extra fields and for a non-lowercase UUID; `404 restart_not_found`; `409 restart_in_progress`;
`409 restart_blocked_by_task_secret`; `503 restart_store_failed`; and retryable
`503 restart_maintenance` on terminal mutations while admission is closed. A persisted restart
object that cannot be parsed is audited and loaded as fail-closed `phase:"invalid"`; it stays closed
until the matching DELETE records an explicit abort.

### `GET /v1/orchestrator/tasks`, `GET /v1/orchestrator/tasks/:id`

Every task this Mac knows about, newest first, capped at the most recent 200 records — or one of
them alone under `task`. `404 not_found` for an id that was never registered or has been cleaned up.

```console
$ curl -s http://127.0.0.1:7717/v1/orchestrator/tasks \
    -H "X-Clawdline-Orchestrator: $ORCH" | jq '[.tasks[] | {id, state, title, assistant}]'
[
  {"id":"3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234","state":"briefed","title":"Project portrait","assistant":"codex"},
  {"id":"a70c5e11-3b28-4d6f-8e10-2c94b7f0d3aa","state":"success","title":"Run the suite","assistant":"claude"}
]
```

**Reading is the one thing a paired device may do here.** Send the orchestrator header and it is
used; leave it off and the request falls through to the ordinary token check, so a phone with `read`
sees the same list. That is deliberate — the list is what a session is doing and where, which is the
same class of thing `/v1/sessions` already discloses to a paired device, and a dashboard that could
not show the child rows would be showing a lie about what this Mac is busy with.

The record:

```jsonc
{
  "id": "3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234",
  "state": "briefed",           // queued | spawning | briefed | success | failure
                                // | timeout | cancelled | spawn_failed
  "kind": "image",              // image | code-review | test | custom
  "title": "Project portrait",
  "assistant": "codex",
  "model": "gpt-5.1-codex",     // absent when the task did not name one
  "reasoning_effort": "high",   // Codex only; absent means no CLI override
  "permission": "full",         // ask | edits | full — what was used, after this Mac's ceiling
  "projectDir": "/Users/you/code/clawdline",
  "isolation": "worktree",     // absent for the shared-tree default
  "created": 1787100000,        // integer unix seconds, like every time in this API
  "depth": 1,                   // always 1 in a live tree; 2 only in a record an older build stored
  "spawnedAt": 1787100002,      // absent until a tab exists
  "briefedAt": 1787100014,      // absent until the first message landed
  "finishedAt": null,
  "dir": "/tmp/.clawdline/3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234",
  "worktree": {
    "path": "/Users/you/Library/Application Support/Clawdline/worktrees/clawdline-a1b2c3d4/3f9a21bc-…",
    "branch": "clawdline/task/3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234",
    "base": "b7363e94f9d899d3f3903db7dbad075ce270494f",
    "head": "2655757a…",       // best-effort at finalize; null if neither ref nor tree answers
    "commits": 3,               // git rev-list --count base..branch; null when unreadable
    "dirty": false              // worktree porcelain; null after the checkout disappeared
  },
  "root":  {"sessionId": "841cbb8d-…", "assistant": "claude", "label": "clawdline main", "terminalId": "27439AEE-…",
            "taskId": "a70c5e11-…"},   // the parent task — depth 2 only, and only when it said so
  "child": {"terminalId": "9A1F…", "backend": "iterm", "sessionId": "0f2b91ac-…"},
  "executor": {                    // durable reconciliation evidence; no raw pid/tty is public
    "status": "observed",          // observed | pending | executor_missing | identity_changed
    "provenance": "session_watch_exact_executor",
    "inventory_complete": true, "inventory_generation": 42,
    "inventory_epoch": "65d8fbbc-…", "observed_at": 1787100210,
    "mismatch_observations": 0, "mover": "broker"
  },
  "terminal_intervention": {       // absent unless automatic cleanup is deliberately pending
    "code": "iterm_attention_required", "app": "iTerm2",
    "action": "answer_dialog", "message": "iTerm2 needs attention…"
  },
  "attached": true,                 // present only for an attached follow-up task
  "attachSession": "9A1F…",       // terminal-neutral standing Session id
  "summary": "…",               // finished tasks; the child's own sentence
  "artifacts": ["artifacts/project-portrait.svg"],
  "verification": {"runs": 2, "seconds": 940, "last": "pass",
                   "scope": "swift suite + web-schedules"},
  "graph": {
    "id": "7f7b3c1a-8e1b-4f31-9b75-61f6ef881234",
    "destination": "The reviewed portrait is landed on main.",
    "current_node": "portrait",
    "frontier": [],
    "nodes": [
      {"id":"portrait","kind":"delivery","title":"Draw","depends_on":[],"acceptance":["Matches the brief"],"state":"active","task_id":"3f9a21bc-…"},
      {"id":"review","kind":"review","title":"Review","depends_on":["portrait"],"acceptance":["Three-axis verdict"],"state":"blocked"}
    ],
    "unknowns": [], "out_of_scope": []
  },
  "review": {                       // review nodes only; absent until a valid receipt is collected
    "verdict": "safe_to_land",
    "axes": [
      {"axis":"specification","status":"pass","findings":[]},
      {"axis":"repository_invariants","status":"pass","findings":[]},
      {"axis":"runtime_failure_behavior","status":"pass","findings":[]}
    ]
  },
  "claims": ["Sources/Orchestrator.swift"],   // present (maybe []) only when task.json declared it
  "released_claims": [                        // absent unless something was given back early
    {"path": "/Users/you/code/clawdline/Sources/Orchestrator.swift", "released_at": 1787100090}
  ],
  "untouched_claims": ["Sources/Orchestrator.swift"],  // absent unless the terminal audit found any
  "landing": {                         // absent until root records the post-delivery obligation
    "state": "pending",               // pending | landed | abandoned
    "target": "main",                 // optional branch or HEAD description
    "delivery": "clawdline/task/3f9a21bc-…", // optional branch or review conclusion
    "owner_root_key": "9f1c2e7a",     // rootKeyDigest, never a free-text label
    "since": 1787100110,              // when the obligation was first recorded
    "note": "waiting for the shared tree" // optional
  },
  "usage": {"input": 48210, "output": 9330, "cacheRead": 412880, "cacheWrite": 31200,
            "total": 501620, "model": "claude-sonnet-4-5", "costUsd": 0.4243}
}
```

`executor` compares that one task's persisted terminal, assistant, tty, pid/start and conversation
tuple with one fresh inventory; it never searches by label or cwd and never borrows another task's
row. Incomplete or stale scans preserve the last proved receipt. One complete absence is `pending`;
only two complete observations in the same process epoch spanning at least 60 seconds may become
`executor_missing` or `identity_changed`, both with `mover:"person"`. The exact observed process
tuple remains private in the durable store for diagnosis.

**The secret is not in here and never will be.** The durable identity is its SHA-256. While a
serialized task is still queued, the app also keeps a temporary encrypted copy in its private
registry so startup can resume the queue; it is removed before spawning and never enters an API
record. The at-rest key is a dedicated random 32-byte value in the app's private config directory,
not a value derived from any request credential.

`child.terminalId` is in the same space as every `id` in `/v1/sessions`, which is what makes the
child row in a session list joinable to the task that opened it. `root.assistant` preserves the
validated `task.json` field. New ordinary dispatch requires it whenever `root.session_id` is
non-null. Persisted legacy rows remain readable, but absence is unknown in ownership decisions;
the old Claude default remains only in compatibility readers that do not assert ownership.
Empty strings and values other than `claude` or `codex` are refused.
`root.terminalId` is resolved live — directly from `root.taskId` when there is one; otherwise the
declared assistant must own the current process-bound identity. Claude's exact transcript must
belong to its current process and must have been named by the process registry or hook; title/time
ranking alone is display discovery, not identity. Codex's id must come from the user rollout held
open by its current pid. Null or stale ids and reused terminals stay unresolved instead of mounting
under a different row. `depth` and `root.taskId` are what a client nests a list by: a `depth` of 2
means the row belongs under another *child* row, not under a root, and `taskId` says which. Two is
the floor, so a client never has to draw a third level. `usage` appears at finalize and
is best-effort: `costUsd` is `null` for any model without a published per-token price, which is
every OpenAI one, since Codex bills against a plan. Tokens are still counted. Null fields are
omitted the way they are everywhere else on this API — read by name, and treat absent as unknown.
`waiting_on` follows the same rule: it is present only on a queued serialized task with blockers,
and it may name a current holder or an older FIFO waiter.

`terminal_intervention` means the task's terminal cleanup is still pending, not that its tab was
closed. Clawdline retains the close deadline and will not pile up another close while the inventory
is incomplete or iTerm's circuit is open. A modal case uses `iterm_attention_required` and
`answer_dialog`; a process still present or a failed exact-tty scan uses
`terminal_intervention_required` and `inspect_terminal` without inventing an iTerm dialog. After a
well-formed iTerm list response, a modal intervention gets one next safe cleanup attempt and
success removes this field. A non-modal intervention has no timer-driven retry: another five-second
beat never sends `/exit`, TERM or KILL again; a new explicit close action or a person inspecting the
terminal is required.

`projectDir` never changes meaning: it is the repository/subdirectory the task concerns.
`worktree.path` is the isolated checkout root, while `dir` is still the unrelated protocol and
artifact directory. The broker, not the child, reads `head`, `commits`, and `dirty` from git. A
serialized isolated task names `isolation: "worktree"` while queued but omits the `worktree`
object until its tab exists: its base is resolved only when it acquires its mutex, so no preliminary
SHA is presented as the receipt for what the child actually started from.

`verification` is absent unless the child supplied a well-formed optional object in `result.json`.
Its `runs` and `seconds` are non-negative integers, `last` is `pass`, `fail`, or `skipped`, and
`scope` is a short free-text description. Missing or malformed verification metadata never changes
the authenticated task outcome.

`graph` is absent for legacy/free-form tasks. Its node `state` and top-level `frontier` are derived
at read time from the newest task attempt for each node: `ready`, `blocked`, `active`, `done`,
`failed`, or `awaiting_landing`. They are never persisted readiness claims. A review node reaches
`done` only with a valid three-axis `review.verdict == safe_to_land`; verification nodes additionally
require `verification.last == pass`. A successful review or verification task without its
kind-specific receipt is `failed` for dependency purposes; other node kinds use ordinary task
`success`. While a graph remains in the bounded task registry, its definition is immutable per
graph id; after retention expires, reuse the old id only if the caller independently retained that
definition. `current_node` and the projected task ids vary by task attempt.

For Codex dispatches, `reasoning_effort` accepts only `high` and `xhigh`; use `high` for coding and
`xhigh` for planning. Empty, non-string, unknown values (including `max` and `ultra`), and the field
on a Claude task all return `422 bad_task` with `reasoning_effort` in the message. Omission adds no
`--config` argument. The optional value survives registry reloads, appears in this record, and is
included in `orchestrator.dispatch` audit metadata.

The same payload goes out on [the event stream](#the-event-stream) as an `orchestrator` frame
whenever any record changes, and once when a stream opens, right after `hello` and `sessions`.

`released_claims` and `untouched_claims` are two independent, purely observational trails —
neither ever blocks anything. `released_claims` is written by
[`POST .../claims/release`](#post-v1orchestratortasksidclaimsrelease) below: each entry names an
absolute path this task gave back early and the Unix second it happened, and the field is absent
until the first release. `untouched_claims` is written once, at the task's terminal transition: it
lists the *relative* declared `claims` whose path was never modified after `spawnedAt` — including
one that no longer exists at all — so a root can see it over-declared and claim narrower next
time. Both survive a restart like every other task field.

`landing` is the root's durable post-delivery receipt. Its `owner_root_key` is derived from the
same canonical root identity and `rootKeyDigest` used by claims conflicts; clients do not supply a
label in its place. A newly written `landed` receipt also carries
`verification_origin: "local_target_branch"`, canonical `verified_commit`, and the local target
head observed as `verified_target_commit`; legacy landed rows omit them and cannot produce
`work_complete`.
Optional values are omitted when unknown. The field is informational only: it does not retain
claims, refuse a dispatch, or otherwise turn the task record into a lock.

### `GET /v1/orchestrator/graphs`

Returns one control-sheet row per typed graph under `graphs`, newest graph first, plus the integer
Unix second `at`. The shape is the same `graph` projection embedded in a task record, but nodes are
joined across every retained attempt, so `task_id`, node state, and `frontier` describe the newest
durable evidence. Graph definitions are stored with their tasks rather than in a second registry;
admission prevents two definitions while any task for that graph remains retained. Reading has the
same paired device or orchestrator-token policy as `GET /v1/orchestrator/tasks`.

```console
$ curl -s http://127.0.0.1:7717/v1/orchestrator/graphs \
    -H "X-Clawdline-Orchestrator: $ORCH" \
    | jq '.graphs[] | {id, destination, frontier}'
```

### `POST /v1/orchestrator/tasks/:id/landing`

Updates the root-owned obligation attached to one task. `pending` (and the terminal `abandoned`
declaration) accepts either that task's durable secret in `X-Clawdline-Task-Secret` or the
machine-level `X-Clawdline-Orchestrator` token. `landed` accepts **only** the machine token: the
child necessarily knows its own secret, so letting that credential assert target landing would
make the second check self-reported. The machine credential is the same family used by dispatch,
cancel, and claims release, and lets the dispatching root—or a named root that accepted a
handoff—close the obligation.

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/tasks/$TASK/landing \
    -H "X-Clawdline-Task-Secret: $TASK_SECRET" -H 'Content-Type: application/json' \
    -d '{"state":"pending","target":"main","delivery":"clawdline/task/3f9a21bc","note":"waiting for the shared tree"}'
{"ok":true,"task":{"id":"3f9a21bc-…","state":"success",
 "landing":{"state":"pending","target":"main","delivery":"clawdline/task/3f9a21bc",
            "owner_root_key":"9f1c2e7a","since":1787100110,
            "note":"waiting for the shared tree"}}}
```

The body accepts only `state`, `target`, `delivery`, `commit`, and `note`. `state` is required and
is `pending`, `landed`, or `abandoned`. `target` and `commit` are non-empty strings up to 200
characters and are both required for `landed` (a target already present on `pending` may be
reused); `delivery` and `note` are optional non-empty strings up to 500 characters. A
repeated request for `pending` or `abandoned` preserves the original `since` while
updating any supplied `target`, `delivery`, or `note`. A repeated `landed` request is an immutable
no-op. Moving to either `landed` or `abandoned` requires a terminal task; `landed` also requires a
machine credential. Before writing it, the broker resolves `commit` inside the task's durable Git
repository identity, resolves `refs/heads/<target>` as a local branch, and proves the former is an
ancestor of or equal to the latter. Every new task in a Git project persists the canonical common
Git directory regardless of isolation, because a non-isolated legacy task may still have been
dispatched from a disposable worktree. A readable `project_dir` or retained worktree repository is
independent evidence and must agree. A stale stored absolute path may fall back only to such
independently derived evidence.

The bounded legacy migration accepts one additional shape: a missing `project_dir` must be under
Clawdline's own worktree root with a valid broker task-id component, and its repository slug must
match exactly one still-readable retained worktree receipt in the private 0600 registry. Conflicting
candidates, arbitrary missing paths, basename searches and caller-supplied repositories all fail
closed. This is why the two historical `.none` rows whose deleted path was the broker worktree
`69b79f6d-…` can close without weakening named-local-branch ancestry verification. Caller revision
expressions are replaced by canonical object ids in the receipt. Both terminal states are final and cannot move to another state; reopening
work requires a new task. `since` remains when the obligation began, while `landed_at` records
when the target proof was captured. Verification runs outside the registry lock and a CAS refuses
the write if its pending record changed meanwhile.

| `code` | status | |
|---|---|---|
| `bad_request` | 400 | missing/unknown state, unknown field, invalid optional text, a commit on a non-landed state, or no target/commit for landed |
| `forbidden` | 403 | no accepted credential was supplied; in particular a task secret is never accepted for `landed` |
| `not_found` | 404 | no retained task with that id |
| `not_terminal` | 409 | `landed` or `abandoned` was requested while the child task is still live |
| `invalid_transition` | 409 | a `landed` or `abandoned` receipt was asked to move to another state |
| `unverified_landing` | 409 | commit or local target did not resolve in the task repository, or commit was not contained by target |
| `stale_write` | 409 | the landing changed while git verification ran; retry against the new receipt |

### `GET /v1/orchestrator/landings`

Lists only current `pending` obligations, oldest first. Authentication is identical to
`GET /v1/orchestrator/waits`: the orchestrator token or a paired device with `read` may query it.

```json
{"landings":[{"id":"3f9a21bc-…","title":"Edit the orchestrator",
 "root_key":"9f1c2e7a","root_label":"clawdline main",
 "paths":["Sources/Orchestrator.swift"],"since":1787100110,"age_seconds":42,
 "target":"main","note":"waiting for the shared tree",
 "ownership":{"version":1,"status":"observed_working","subject":"root",
   "reason":"exact_root_observation","task_id":"3f9a21bc-…","task_state":"success",
   "root_key":"9f1c2e7a","root_assistant":"codex",
   "observed_work_state":"working"}}],
 "sources":{"sessions":{"observed_at":1787100151,"generation":42,
   "provenance":"session_watch","freshness":"current"},
   "tasks":{"observed_at":1787100152,"provenance":"orchestrator_task_registry","freshness":"current"},
   "landings":{"observed_at":1787100152,"provenance":"orchestrator_landing_registry","freshness":"current"}},
 "at":1787100152}
```

`paths` is the task's original relative `claims`. `age_seconds` uses the same
`max(0, now - since)` integer-seconds formula as `workspace_busy`, so a clock rollback reports zero.
Nullable `target`, `note`, and `root_label` remain present in this list so a dashboard can render a
stable row shape. `ownership.status` is closed: `observed_working`,
`observed_ready_or_holding`, `observed_other`, `task_still_live`, `not_observed`, or `unknown`.
Only a complete timestamped SessionWatch inventory may produce `not_observed`; missing, stale,
ambiguous or assistant-less legacy root evidence is `unknown`, never dead/offline. The bounded
observation path preserves all durable rows even if the main queue or live watcher is unavailable.
Stable hashed root/task ids and closed evidence fields make the same row comparable with Bearings
without exposing a conversation id, transcript, token, tty, pid, process start or repository path.
A row is a signpost, not a gate: callers decide whether to wait or continue.
Pending landing obligations are exempt from the registry's ordinary newest-200 cleanup cap; they
remain queryable until a root explicitly marks them `landed` or `abandoned`.

### `GET /v1/orchestrator/storage`

Lists only storage with an ownership receipt in
`~/.config/clawdline/owned-storage.jsonl`. Authentication is identical to
`GET /v1/orchestrator/landings`: the orchestrator token or a paired device with `read` may query
it. This GET is the dry run; it evaluates the same held/releasable policy the collector will use,
but has no write or deletion mode.

```json
{"at":1787100152,"source_state":"known",
 "totals":{"owned_items":1,"owned_bytes":3232481792,"held_items":0,"held_bytes":0,
   "releasable_items":1,"releasable_bytes":3232481792,"unknown_items":0,
   "unknown_bytes":0,"unknown_size_items":0,"malformed_ledger_lines":0},
 "owned":[{"task":"3f9a21bc-…","assistant":"claude","session":"8d91a14b-…",
   "kind":"scratchpad","path":"/private/tmp/claude-501/-Users-me-code-repo/8d91a14b-…",
   "proof":"briefing_marker","registered_at":1787000000,"bytes":3232481792,
   "state":"releasable","why":"eligible","age_seconds":46800,
   "eligible_at":1787090000}],
 "warnings":[],
 "config":{"enabled":false,"floor_hours":12,"untracked_process_floor_hours":24}}
```

`state` is `held`, `releasable`, or `unknown`. `unknown` is a fail-closed answer and is treated as
held by every mutating boundary. It appears when a required source (the ownership ledger,
orchestrator registry, Claude live-session registry, process identity, canonical path metadata, or
size reading) is unavailable or malformed. A missing path is `held` with `why: path_missing`; it
is not silently rediscovered elsewhere. `warnings` identifies malformed ledger line numbers while
valid independent rows remain visible.

The endpoint does **not** scan `/tmp/claude-*`, interactive assistant sessions, or unowned
directories. An entry can appear only after a Claude child transcript proves the exact task marker
and Clawdline successfully appends its task, assistant, session, canonical path, proof method,
project, and timestamp to the independent ledger.

### `GET /v1/orchestrator/usage`, `.csv` (legacy forensic contracts)

These two URLs retain their pre-analytics contracts: `/usage` is the original aggregate payload
and `/usage.csv` is the original forensic CSV, including session/path and durable lineage columns.
Existing accounting and incident tools therefore do not receive a replacement contract on
an old URL. They remain authenticated reads; the CSV is forensic rather than privacy-safe and must
not be exposed as a public download.

The legacy payload and analytics payload use the same unavailable-column answer:
`graph_id` and `disposition`. Feature is no longer called an unavailable column; it is an
append-only accepted-attribution projection available in analytics Portfolio responses, not a new
grouping mode retrofitted onto the legacy aggregate URL.

### `GET /v1/orchestrator/usage/analytics`, `/analytics.csv`, `/analytics.json`

**What every assistant session on this machine has spent, out of a store nothing sweeps.**

The task registry keeps 200 rows and a task directory is deleted 24 hours after the task
finishes, so `GET /v1/orchestrator/tasks` cannot answer a question about last month and never
will. These three read it out of `~/Library/Application Support/Clawdline/Observability/usage.sqlite3`
instead — see [`docs/orchestrator.md`](orchestrator.md#the-usage-ledger) for what is in there and
how it gets there. The web app's **Usage** button is the human-facing Overview and Agent Work
reader of the first route; it does not maintain a second store or a second arithmetic path.

All three take the same closed query and none starts anything. Unknown keys and repeated keys are
`400 bad_request`; a misspelled filter may not silently broaden an accounting query.

| Query | Meaning |
|---|---|
| `from`, `to` | inclusive local days, `YYYY-MM-DD`, interpreted in `timezone`. Omitted means every row the ledger holds |
| `timezone` | IANA timezone identifier; the Mac's current timezone by default. Range boundaries and day/week/month buckets are made by `Calendar` in this zone, so a DST day is one bucket even when it is 23 or 25 hours |
| `group` | `model` (the default), `assistant`, `origin`, `project`, `day`, `coverage`, `task`. An unknown value is `400 bad_request` |
| `bucket` | `day` (the default), `week`, `month` |
| `assistant`, `model`, `origin`, `project` | exact filters over the public analytics values. `project` is the final project name, never its filesystem path |
| `view` | `overview` (the default) or `agent_work`; identifies the consumer while preserving one response contract |
| `limit` | drill-down page size, `1...200`, default `50` |
| `cursor` | opaque continuation returned by `pagination.nextCursor`. Rows are ordered by `startedAt`, then interval id, newest first; a newer insertion cannot duplicate or skip the continuation |

```json
{"usage":{
  "range":{"from":"2026-08-01","to":"2026-08-28","timezone":"Asia/Taipei"},
  "freshness":{"generatedAt":"2026-08-29T02:00:00Z","latestObservedAt":"2026-08-29T01:59:30Z",
               "ageSeconds":30,"status":"current","scanTruncated":false},
  "rangeFreshness":{"dataThrough":"2026-08-28T23:59:30Z","ageSeconds":7230,
                    "status":"historical"},
  "capabilities":{"views":["overview","agent_work"],"groupBy":["model","assistant","…"],
                  "buckets":["day","week","month"],"exports":["csv","json"],
                  "maxPageSize":200,"maxScannedRows":100000,
                  "attribution":{"dimensions":["project","feature"],
                    "sources":["explicit","inherited","manual","llm","policy"],
                    "decisions":["proposed","accepted","rejected"],
                    "featureAggregation":"one_unambiguous_accepted_head"}},
  "priceSnapshot":{"activeId":"clawdline-prices-2026-08-28",
                   "observedIds":["clawdline-prices-2026-08-14"],
                   "meaning":"Observed ids price rows; activeId is current, not an actual bill."},
  "groupBy":"model",
  "totals":{"rows":99,
    "tokens":{"inputNew":21587330,"output":1580530,"cacheRead":1548751080,"cacheWrite":2210400},
    "tokenPartsUnknown":{"inputNew":3,"output":3,"cacheRead":3,"cacheWrite":3},
    "tokenRowsUnknown":3,"measuredFloor":1574139340,"strictTotal":null,
    "origins":{"manual":40,"dispatch":49,"schedule":10},"scheduledRuns":10,
    "costs":[
      {"unit":"USD","basis":"list_price_estimate","value":128.41,"rows":30,
       "priceSnapshotIds":["clawdline-prices-2026-08-28"]},
      {"unit":"USD","basis":"provider_actual","value":17.32,"rows":1,
       "priceSnapshotIds":[]}],
    "unavailableCost":{"rows":68,"reasons":{"plan_billed":57,"no_cost_recorded":11}},
    "coverage":{"states":{"complete":92,"partial":4,"source_missing":3},
                "reasons":{"source_unreadable_at_close":3},"tokenRowsUnknown":3,
                "tokenPartsUnknown":{"inputNew":3,"output":3,"cacheRead":3,"cacheWrite":3}}},
  "breakdown":[{"key":"claude-opus-5","…":"the totals shape for this group"}],
  "trend":[{"bucket":"2026-08-28",
             "tokens":{"inputNew":20,"output":30,"cacheRead":400,"cacheWrite":0},
             "measuredFloor":450,"strictTotal":450,"coverage":{"…":"…"}}],
  "rows":[{"id":"opaque interval id","taskId":"…","scheduleId":"…","startedAt":"…","endedAt":"…","assistant":"codex",
           "model":"gpt-5.6-sol","project":"clawdline",
           "tokens":{"inputNew":20,"output":30,"cacheRead":400,"cacheWrite":0},
           "strictTotal":450,"measuredFloor":450,"unknownTokenParts":[],"sourceTotal":450,
           "cost":null,"missingCostReason":"plan_billed","coverage":"complete",
           "coverageReasons":[],"reconciliation":null,"inputBasis":"includes_cache",
           "lineage":{"graphId":null,"parentTaskId":"…","retryOf":null,"attempt":0,
                      "landingState":"landed","disposition":null}}],
  "pagination":{"limit":50,"nextCursor":"eyJhdCI6…","hasMore":true},
  "rowCount":99,
  "corrections":0,
  "schemaVersion":1,
  "unavailableDimensions":{"dimensions":["graph_id","disposition"],
                           "reason":"A whole graph, accepted outcome, or Feature requires explicit lineage or accepted attribution. …",
                           "graphView":false,"retryView":false,"landingView":false,
                           "featureView":true,
                           "featureAvailability":"one_unambiguous_accepted_head_or_unknown"},
  "portfolio":{"schemaVersion":1,"primarySignal":"generated_output","runs":72,
    "scoreWarning":"Generated output is an operational signal, not a productivity score.",
    "comparison":{"status":"comparable","current":1580530,"previous":1400000,
                  "absolute":180530,"percent":12.895},
    "projects":[{"id":"project-opaque-digest","label":"clawdline","rank":1,
      "identity":{"status":"available","reasons":[]},"output":820000,
      "unknownOutputRuns":0,"runs":31,"scheduledRuns":4,"scheduledOutput":12000,
      "lineage":{"status":"partial","rootRuns":5,"childRuns":21,
                 "scheduledRuns":4,"unknownRuns":1,"reason":"lineage_evidence_missing"},
      "cost":{"status":"unavailable","reason":"partial_cost_coverage"},
      "coverage":{"status":"partial","unknownOutputRuns":1},
      "comparison":{"status":"comparable","absolute":70000,"percent":9.33},
      "trend":[],"assistantMix":[],"workMix":[],"recentWork":[]}],
    "scheduledWork":{"status":"available","runs":10,"output":12000,
      "unknownOutputRuns":2,"schedules":[],
      "unknownSchedule":{"runs":1,"reason":"schedule_identity_missing"}},
    "features":{"status":"available","policy":"one_unambiguous_accepted_head",
      "groups":[],"unknown":{"label":"Unknown Feature","runs":12,
      "reason":"no_unambiguous_accepted_head"}},"insights":[]}}}
```

Every response carries range/timezone, schema, capabilities, observed and active price snapshots,
ledger freshness, range data-through/freshness, coverage, corrections and unavailable dimensions.
Top-level `freshness` is always the newest ledger observation independent of the selected range;
`rangeFreshness` describes only that range, so a historical month is not presented as a stale
store. Parent/retry/attempt/landing lineage is best-effort from durable task records. Graph,
accepted disposition remain unavailable until an explicit producer exists. Feature is available
only through one active accepted attribution head; proposal-only, rejected, conflicting and absent
evidence stays `Unknown Feature`. None is inferred from root Session or task success.

`portfolio` is a versioned projection over the exact bounded row subject behind `totals`. `runs`
deduplicates task rows by task id and session segments by stored boundary/session identity.
`projects` ranks generated output with a deterministic id tie-break and keeps measured values beside
unknown-run counts. Lineage classifies stored session boundaries as root/main work, broker-producible
depth-1 task boundaries as child work, and scheduled origin as scheduled; missing evidence stays
Unknown. `scheduledWork.output` is the measured contribution and
`scheduledWork.unknownOutputRuns` is displayed beside it rather than silently omitted. Cost is
comparable only for one fully covered unit+basis series. Cross-range comparison requires both dates,
equal local-calendar-day ranges, non-truncated reads and complete output.

Canonical Project identity comes from the stored canonical key or one accepted append-only Project
attribution event. A legacy key under Clawdline's managed-worktree root is never displayed as its
task UUID and is never resolved from that UUID or basename. Without an evidence-backed migration
event it remains `Unknown Project` with `legacy_managed_worktree_project_key`; migration and restore
requirements are specified in [`docs/usage-attribution.md`](usage-attribution.md).

**Value, unit, basis, availability and reason remain separate.** `null` is unknown and never zero.
`measuredFloor` sums known token parts; `strictTotal` is `null` when any part is unknown. The four
trend token fields are mutually exclusive normalized parts. The human dashboard leads with
generated `output`, keeps new input and both cache parts separate, and shows unique scheduled runs
for the selected range. This is an operational work signal rather than a productivity score. Cost is an array of exact
`unit`+`basis` series: two USD rows are not added when one is `provider_actual` and the other is
`list_price_estimate`. `plan_billed` is unavailable, not free; an estimate is not an actual bill.

`coverageReasons` carries the store's own words for why rows are marked: `session_unresolved` (a
task whose session neither collector ever knew, filed under an invented identity that gets a
cursor of its own — so that group's total may count one session's counters twice),
`source_regressed` (a cumulative counter that went backwards, so the number spans a replaced
transcript), `source_unreadable_at_close`, `no_usage_recorded`. `coverage` says only how much of a
source was read and says nothing about any of these.

`.csv` and `.json` export every matching public row, not merely the page. Both omit raw prompts,
session ids, working directories, source bytes, raw usage objects and the stored machine-local day
(the requested timezone is authoritative instead). Safe CSV deliberately keeps `ended_at`, all
four token parts, `unknown_token_parts`, `source_total`, `reconciliation`, `input_basis`, and the
complete cost value/unit/basis/snapshot identity. CSV leaves unknown fields
empty and prefixes text beginning (after spaces) with `=`, `+`, `-`, `@`, tab or carriage return
with an apostrophe before RFC 4180 quoting, preventing spreadsheet formula execution. JSON is the
lossless public form: null and zero, strict total and measured floor, cost unit/basis/snapshot and
all availability reasons remain distinguishable. A query over more than 100,000 rows is marked
partial on the reading route and refused as `413 export_too_large` on either export; narrow the
range rather than accepting a silently incomplete file. The range and every exact assistant,
model, origin and public-project filter are applied in SQLite before `maxScannedRows + 1` matching
rows are admitted; the service never reads or sorts the whole ledger. A narrow old month can
therefore clear the refusal even when the store contains millions of newer rows.

Analytics rows and their opaque cursor are explicitly ordered newest first by `startedAt`, then
interval id. This is a presentation contract, not the forensic reader's incomplete-first order.
Coverage gaps remain prominent through `availability`, `coverage`, unknown-part counts and the web
warning banner rather than through a hidden ordering bias.

Analytics owns a serial worker and an admission budget of two. Saturation is returned as typed
`429 usage_analytics_busy`; it does not consume the separate eight-place `/info`/`/v1/places`
reading budget. JSON serialization failure is typed `500 json_serialization_failed`, never a 200
empty attachment.

Authentication is identical on all three routes: an orchestrator token or a paired device with
`read`. Anonymous requests are `401`. The projection contains stable accounting ids, task id,
assistant/model/origin, final project name and usage metadata; it contains no prompt text or raw
filesystem path.

The future Project/Feature recording contract and the append-only small-LLM merge boundary are in
[`docs/usage-attribution.md`](usage-attribution.md). An LLM may propose a label; only one
unambiguous accepted head enters Feature totals, and no attribution event can rewrite tokens.

### `GET /v1/orchestrator/inflight?project=<dir>`, `GET /v1/orchestrator/tasks/:id/inflight`

**Every line of work outstanding in a repository, including work a worktree hides.**

A delivery sitting finished on `clawdline/task/…` appears in no `git status`, no `git diff` and no
file listing of the shared checkout, and its claims were released the moment the task ended. A
session asking "has anyone done this?" looked at the tree, saw nothing, and did it again. This is
the answer to that question.

Two doors onto the same list. The repository-wide form takes any absolute `project` (or
`project_dir`) directory and resolves the repository containing it on this side; authentication is
identical to `GET /v1/orchestrator/landings`. The per-task form takes no directory at all — it
reads the repository off the task and authenticates with that task's own secret, because **a child
has no orchestrator token and should not be taught to read one**, and because a task that cannot
write a path cannot ask about a tree it is not working in. The asking task is left out of its own
answer.

```json
{"repository":"/Users/me/code/clawdline",
 "inflight":[{"id":"3f9a21bc-…","title":"Add the schedules page","state":"briefed",
   "visibility":"live","assistant":"claude","project_dir":"/Users/me/code/clawdline",
   "created":1787100110,"age_seconds":2640,"claims":["Sources/RemoteServer.swift"],
   "root_label":"clawdline schedules","root_key":"9f1c2e7a",
   "progress":[{"note":"the real problem is in Settings, not the route","at":1787101400}],
   "worktree":{"branch":"clawdline/task/3f9a21bc","base":"d6781a8","head":"c059bd6",
     "dirty":false,"branch_exists":true,"merged":false}}],
 "at":1787102750}
```

`visibility` is why the row is here, and it is the whole of the staleness policy:

| value | means |
| --- | --- |
| `live` | a session is on it now — any non-terminal state |
| `unmerged` | finished, and its delivery is still on a branch nobody has merged |

There is a third value, `settled`, and it never appears in a list: it is how a row leaves. **An
entry stops being live because git says so, not because somebody remembered to clear a flag.** A
delivery disappears when its branch is merged into the repository's `HEAD` or deleted — the same
moment the work stops being invisible. Two declared answers are honoured first, because a root
saying so is better evidence than a branch this side has to guess about: `landing: landed` and
`landing: abandoned` settle a row, and `landing: pending` keeps one visible even with no worktree.

**Unknown git facts keep the entry visible.** If the repository cannot be read, every delivery in
it stays listed. The costs are not symmetric: showing a delivery that turns out to be merged costs
a glance, and hiding one that is not costs somebody a day of rebuilding it.

A task whose session died has already been finalized as `failure` by the sixty-second rule in
`Orchestrator.watch`, and its branch is kept by `worktreeDisposal` whenever it carries commits — so
it appears here as `state: failure`, `visibility: unmerged`, which is exactly right: the work is
there, on a branch, and nobody is doing it.

A finished task that was **not** isolated is `settled` here, and that is a boundary rather than an
oversight — its edits are in the shared tree where `git status` already shows them. The way such a
delivery stays visible is its root declaring `landing: pending`, which is the declared half of this
and is not duplicated by the derived half.

Branch facts cost two `git for-each-ref` calls for the whole repository rather than two per task.
`head` is what the ref points at now, falling back to what the app last recorded.

### `POST /v1/orchestrator/tasks/:id/progress`

**One line about what this task is actually doing**, authenticated by that task's secret like
`complete` and `notify`.

```bash
curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/tasks/$TASK/progress \
  -H "X-Clawdline-Task-Secret: $TASK_SECRET" -H 'Content-Type: application/json' \
  -d '{"note":"the real problem is in Settings, not the route"}'
```

The title is fixed at dispatch, and by the time `result.json` exists somebody else may have spent
an hour on the same thing. This is the seam between those two moments, and it is deliberately one
sentence and one curl: a session that has to stop and compose a status report will not do it.

`note` is required, non-empty after trimming, and at most 300 characters. The newest 5 are kept and
appear as `progress` on the task record and on every `inflight` row. Sending the same sentence as
the newest one is accepted and ignored — a loop is not news, and refusing it would only cost the
caller a retry. A terminal task is refused with `409 not_live`: what it did belongs in its summary.

**The file half of the same channel.** Most children cannot make this call at all: a Codex child's
sandbox sets `CODEX_SANDBOX_NETWORK_DISABLED=1`, `curl` to loopback exits 7 after 0 ms, DNS itself
is off, and no approval prompt ever appears — measured on the machine this came from (task
be9a54c0), where 133 codex children were briefed to send this curl and 0 notes ever arrived.
`result.json` never had the problem, because it is a file the broker picks up. So progress has a
file twin with the same authentication: the child writes `progress.json` in its own task directory,

```json
{"task_secret": "<TASK_SECRET>", "note": "<one sentence, at most 300 characters>"}
```

replacing the whole file each time, and the watch beat collects it within seconds. Only the latest
sentence is collected — the file is current status, not a log; history is the transcript's job — and
the collected note joins the same `progress` list with the collection time as its `at`. The same
sentence through both channels is recorded once, by the same newest-note rule, and a sentence the
file already delivered is never replayed, even across a restart: `progress_file_note` on the stored
record is the collected-marker. A file with the wrong secret is ignored and audited once
(`orchestrator.progress` with `via=file`), and a half-written file simply fails to parse and is read
again on the next beat. The HTTP route above stays the fast path for a child that can reach it — a
curl lands immediately, the file on the next beat.

### `POST /v1/orchestrator/tasks/:id/respawn`

**Retry a dispatch whose tab never opened**, without writing the task out again. Orchestrator-token
only, like dispatch itself: this opens a session.

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/tasks/$TASK/respawn \
    -H "X-Clawdline-Orchestrator: $ORCH"
{"ok":true,"secret":"9f2c…","respawn_of":"3f9a21bc-…","original_task":"3f9a21bc-…",
 "task":{"id":"7c41e0aa-…","state":"spawning","title":"Project portrait",
         "respawn_of":"3f9a21bc-…","respawn_generation":1,…}}
```

`spawn_failed` was 34 of 206 dispatches on the machine this was measured on — 33 of them Codex —
and until this route existed the answer was that the root writes the whole `task.json` out again
under a fresh id, which is thirty-four rewrites by the most context-loaded session in the tree.
The broker already holds everything the original said, so it does the copying.

The new task is an **ordinary dispatch**: a fresh id, a fresh directory, and every capacity, depth,
claims, quota and serialization gate applied again, with the same refusals. What it inherits is the
original `task.json` with `task_id` swapped — `instructions`, `plan`, `claims`, `serialize`,
`assistant`, `model`, `reasoning_effort`, `permission_mode`, `project_dir`, `isolation` and the
`root` binding. `instructions` is the field that makes this a file copy rather than a record copy:
the registry never held it.

The secret is fresh too, because the old id is finished. Send `{"secret":"<64 hex>"}` to choose it
exactly as an ordinary dispatch does; omit the body and the broker mints one and returns it as
`secret` at the top level of the reply. The reply's `respawn_of` and `original_task` are also on the
task record itself (`respawn_of`, `respawn_generation`), so the chain is visible in
`GET /v1/orchestrator/tasks` instead of looking like three unrelated tasks with the same title.

| `code` | status | |
|---|---|---|
| `forbidden` | 403 | the orchestrator token is missing or wrong |
| `orchestrator_disabled` | 403 | `orchestrator_enabled` is off |
| `not_found` | 404 | no task with that id |
| `not_respawnable` | 409 | the task is not `spawn_failed`. Only the one terminal state meaning *nothing ran* may be retried: a `failure` is an answer, a `timeout` had a session that read the briefing, a `cancelled` was somebody's decision, and a live task has a tab. The error object carries `state` |
| `respawn_exhausted` | 409 | **at most two respawns descend from one original.** What is counted is the whole family the registry still holds below that original, whatever shape it took: a retry of a retry cannot launder the cap by being "the first from *its* parent", and asking the original again cannot launder it either — that one matters, because the id a root has in hand is the one that failed. The error object carries `original_task`, `respawns` and `limit` |
| `bad_task` | 422 | the id is not a lowercase UUID, a supplied `secret` is not 64 hex characters, or the original `task.json` is gone and there is nothing to copy |

Everything `POST /v1/orchestrator/tasks` can refuse, this can refuse too, at the moment it
dispatches the copy — `over_capacity`, `workspace_busy`, `assistant_exhausted`, `rate_limited`.
A refusal there leaves the new directory behind for the ordinary sweep, exactly as a root's own
abandoned attempt does.

### `POST /v1/orchestrator/tasks/:id/claims/release`

Gives back some or all of a task's declared write paths while it is `briefed` or `spawning`, so a
`409 workspace_busy` blocked on them can retry immediately instead of waiting for the whole task to
end — the only way to break a circular wait where two roots each hold what the other needs. A
`queued` task cannot release: it has not started writing anything yet, so its `instructions` will
still write those paths once it is promoted, with no reservation left to stop a conflicting
dispatch from landing on top of it — see `not_started` below; cancel a queued task instead of
releasing its claims. Orchestrator-token only, like dispatch itself; a paired device's own token
answers `403`.

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/tasks/$TASK/claims/release \
    -H "X-Clawdline-Orchestrator: $ORCH" -d '{"paths":["Sources/Orchestrator.swift"]}'
{"ok":true,"task":{"id":"3f9a21bc-…","state":"briefed",
                    "claims":["Sources/Orchestrator.swift","Sources/RemoteServer.swift"],
                    "released_claims":[
                      {"path":"/Users/you/code/clawdline/Sources/Orchestrator.swift",
                       "released_at":1787100090}
                    ]}}
```

`paths` names the original *relative* declarations to give back, in the same spelling `claims` in
`task.json` used; an omitted or empty `paths` releases everything this task still holds. A path
that was never declared, or was already released, is silently a no-op rather than an error, so a
retried release can never fail on its own earlier success. Freed paths are compared the same way
arbitration compares claims — ancestor and descendant, not exact string equality: `Sources`
released frees every declared claim key under it, `sources` (different case) frees nothing, and
naming a path *inside* a directory-shaped claim (say the task declared `Sources` and the request
names `Sources/Orchestrator.swift`) frees that whole claim key, because a directory claim is one
atomic reservation rather than a set of the files under it. `paths` entries may not contain a `..`
component, same as `claims` in `task.json`.

| `code` | status | |
|---|---|---|
| `not_found` | 404 | no task with that id |
| `already_done` | 409 | the task already reached a terminal state — its claims are already released, and there is nothing left to give back early |
| `not_started` | 409 | the task is still `queued` — it has not started writing anything yet, so releasing its lease early would leave `instructions` still able to write those paths with no reservation behind them; cancel it instead |
| `bad_request` | 400 | a `paths` entry contains a `..` component |

Release only ever narrows what dispatch-time arbitration compares against: `claims` and the
GET record's frozen historical reservation are unchanged, but a released path stops counting
toward `409 workspace_busy` and toward the same-root `claims_overlap` warning against every other
task, immediately, under the same lock dispatch itself uses. `orchestrator.claims.released` is
written to the audit log with the task id and the paths actually freed — but not who called it.
Release is a machine-level permission exactly like cancel: the request carries the orchestrator
token, a Mac-wide credential, and nothing that identifies which root made the call, so any root
holding that token may release any task's claims, including one blocking it. There is no narrower
guarantee to record; the audit line names the task and paths, not a caller.

### `POST /v1/orchestrator/tasks/:id/notify`, `POST /v1/orchestrator/notify`

These routes let an agent send **content** the user is waiting for, through the same per-subscription
RFC 8291 WebPush path as Clawdline's state notifications. The task form is for a child: its secret
is accepted in `X-Clawdline-Task-Secret` or the JSON body, exactly like `/complete`, and is compared
in constant time with the task's stored hash. The header is the canonical form shown to children.
It is valid while the task is live and for 60 seconds after `finished_at`. If the user has turned
agent notifications off in Settings → Remote, it returns `409 agent_notify_disabled`:

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/tasks/$TASK/notify \
    -H "X-Clawdline-Task-Secret: $SECRET" -H 'Content-Type: application/json' \
    -d "{\"title\":\"Today's forecast\",\"body\":\"Sunny, high 27°C.\"}"
{"failed":0,"ok":true,"sent":1}
```

The root form is for a local root session or script and takes the orchestrator token. It observes
the same Settings → Remote preference and returns `409 agent_notify_disabled` while it is off:

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/notify \
    -H "X-Clawdline-Orchestrator: $ORCH" -H 'Content-Type: application/json' \
    -d '{"title":"Deploy","body":"Production is healthy."}'
{"failed":0,"ok":true,"sent":1}
```

`title` and `body` are required, non-blank strings of at most 80 and 500 Swift characters.
On the phone the title is `<task title>: <title>` for a task and `Clawdline: <title>` for a root.
The payload keeps its beginning if the WebPush `maxPayload` ceiling requires shortening: the icon
is dropped first and then only the tail of `body` is discarded. Task notifications use the stable
WebPush topic `agent-task-<task-id>` and root notifications use `agent-root`, so a push service can
replace an older undelivered content notification from the same source.

One task may have five accepted notifications over its lifetime; that count survives an app
restart. Separately, the Mac accepts 30 agent notifications in a sliding hour. That hourly window is
process memory and starts empty after an app restart; it is deliberately separate from the dispatch
limiter. A request with no subscriptions returns `409 not_subscribed` and consumes neither
allowance. Once at least one subscription is attempted, the allowance is consumed even if a push
service refuses it; the response and audit say how many subscriptions accepted and failed.

Authenticated attempts append an `orchestrator.notify` audit row with `task_id` or `root`, `title`,
`result`, and delivery counts when delivery was attempted. Before a task secret is verified,
caller-supplied `title` and `body` are never logged: the row has only `source=task_secret`, a
12-character attempt hash, and `result`. Three failed task-secret attempts are allowed in ten
minutes; further attempts return 429 until the sliding window clears.

Agent content has its own `orchestrator_agent_notify` preference, on by default so an older config
with no such key keeps its existing behavior. It remains separate from the automatic finish and
deploy switches: turning agent notifications off does not turn those other notifications off.

| `code` | status | |
|---|---|---|
| `unauthorized` | 401 | the root request has no recognized credential and stops at the device-auth door |
| `forbidden` | 403 | the task secret is absent or wrong, or a paired device tries to replace the root orchestrator token |
| `agent_notify_disabled` | 409 | the user turned agent notifications off in Settings → Remote; no delivery or allowance is attempted |
| `not_found` | 404 | no task with that id |
| `not_subscribed` | 409 | no device has asked for push notifications; no allowance is consumed |
| `notify_expired` | 409 | the task finished more than 60 seconds ago |
| `bad_request` | 400 | `title` or `body` is missing, blank, or beyond its character limit |
| `notify_limit` | 429 | this task already sent five notifications |
| `rate_limited` | 429 | the Mac accepted 30 agent notifications in the hour, or task-secret failures exceeded three in ten minutes |
| `push_failed` | 502 | one or more push services refused or failed; the error includes `sent` and `failed` counts |

As with `/complete`, an unknown task is 404 while a known task with the wrong secret is 403. That
reveals the existing route-shape distinction; consistency with `/complete` is intentionally kept,
and task ids are unguessable UUIDs.

### `POST /v1/orchestrator/tasks/:id/complete`

How a child says it is done, if it would rather not write a file. Auth is that task's secret, in a
header or in the body, compared in constant time against the stored hash:

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/tasks/$TASK/complete \
    -H "X-Clawdline-Task-Secret: $SECRET" -H 'Content-Type: application/json' \
    -d '{"status":"success","summary":"Wrote a 1024×1024 SVG portrait; no raster."}'
{"ok":true}
```

**This route is optional and the file is not.** Writing `/tmp/.clawdline/<id>/result.json` — with
the same secret inside it — is the completion signal, and the app finds it whether or not anything
was posted. A child in a sandbox with no outbound network finishes correctly; this exists for one
that would rather not wonder how often the directory is being read. When a task is finalized this
way the app still opens `result.json` if it is there, for the artifact list.

| `code` | status | |
|---|---|---|
| `forbidden` | 403 | wrong secret. The same answer for an id that exists with a different secret and for one nobody could guess |
| `not_found` | 404 | no task with that id |
| `already_done` | 409 | that task already reached a terminal state. **A retry is not idempotent here** — the first report wins, and the second is told so rather than quietly overwriting a summary somebody has already read |
| `bad_request` | 400 | `status` is not `success` or `failure` |

No `Idempotency-Key`: `already_done` is the honest answer to a repeat, and a stored-reply header
would turn "you are too late" into a silent success.

### Completion delivery ledger, reconciliation and ACK

`GET /v1/orchestrator/completions` is machine-token-only and has a closed query schema: no query,
or exactly one `pending` whose value is `true`, `1`, `false`, or `0`. `1` means exactly `true` and
`0` exactly `false`. Any other key or value, or a repeated `pending`, is `400 bad_request` rather
than a silently broadened ledger read. `pending=true` and `pending=1` limit the answer to unacknowledged,
non-dead-letter envelopes; a transport-delivered notice remains pending until ACK:

```jsonc
{"completions":[{
  "task_id":"3f9a21bc-…", "task_state":"success",
  "accepted_at":1787100000, "executed_at":1787100300,
  "result_verified_at":1787100300,
  "delivery":{
    "notice_id":"6b1d46cb-1111-4222-8333-444444444444",
    "state":"delivered", "attempts":2,
    "created_at":1787100300, "last_attempt_at":1787100310,
    "next_retry_at":1787100330,
    "transport_delivered_at":1787100301,
    "observed_at":null, "acknowledged_at":null,
    "last_error":null
  }
}],"pending_only":true,"at":1787100312}
```

The six lifecycle facts are not aliases. `accepted_at` is dispatch registration;
`executed_at` is terminal task state; `result_verified_at` exists only after the file secret is
checked; `transport_delivered_at` means the terminal bridge returned success. None of those, and
no GET, SSE frame or Apple Event, proves observation. Only an explicit matching ACK records
`observed_at` and `acknowledged_at` and stops retry:

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/tasks/$TASK/completion/ack \
    -H "X-Clawdline-Orchestrator: $ORCH" -H 'Content-Type: application/json' \
    -d '{"notice_id":"6b1d46cb-1111-4222-8333-444444444444"}'
{"ok":true,"acknowledged":true,"changed":true,"notice_id":"6b1d46cb-…"}
```

The ACK request schema is closed and the operation is idempotent: the second matching request is
`200` with `changed:false`. A different UUID is `409 completion_notice_mismatch`; a legacy task
without an outbox is `409 completion_not_reconciled`; the brief in-process interval before the
outcome-plus-outbox snapshot reaches disk is `409 completion_not_persisted`; a store failure is
`500 completion_store_failed`, and the caller retries. That failure compare-and-swaps only this
notice's ACK transition back to its previous `completion_delivery`; it never restores an older
whole Task over concurrent worktree, landing, close or other receipts.

`POST /v1/orchestrator/completions/reconcile` requires one JSON object and accepts only optional
string `task_id` and JSON-boolean `include_dead_letter`. Empty or malformed JSON, arrays/scalars,
extra keys, and wrong types (including `0`/`1` in place of a boolean) are `400 bad_request`. It
creates durable envelopes for at most 25 identifiable terminal tasks per call from the last seven
days, or rearms matching dead letters when explicitly requested:

```json
{"task_id":"3f9a21bc-…","include_dead_letter":true}
```

The response reports `created`, `rearmed`, `limited`, `batch_limit`, and `lookback_seconds`.
Null-root tasks, older history and unknown identities remain available through ordinary task and
`result.json` polling. Reconciliation never rewrites `root.session_id`; a Coordinator conversation
can follow a rebind only through its persisted process-bound binding history. The historical task
assistant validates the old alias interval, while terminal delivery uses the canonical current
binding's conversation id and assistant; cross-assistant reconnect never targets the stale process.

### `POST /v1/orchestrator/tasks/:id/cancel`

Stop it. The task goes to `cancelled` and the child's terminal is ended the polite way
[`/v1/sessions/:id/end`](#post-v1sessionsidend) does it — the assistant is asked to leave through its
own word, then the tab closes. A task that is already finished answers `200` with its record
unchanged; there is nothing to cancel and nothing went wrong.

**Anything found below it goes with it**, deepest first and for the same reason a closing root
sweeps that way: work whose asker has just been stopped is work nobody is waiting for. Unreachable
in a live tree, where a child dispatches nothing; kept for a stored record an older build left
behind. Those cancellations carry `why=parent_cancelled` in the audit log. The reply names only the task that
was asked for — read the list if a client needs to know what else moved.

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/tasks/$TASK/cancel \
    -H "X-Clawdline-Orchestrator: $ORCH"
{"ok":true,"task":{"id":"3f9a21bc-…","state":"cancelled","finishedAt":1787101880, …}}
```

**Two doors, and they are gated differently.** With the orchestrator token this is the same local
credential that opened the task and it needs nothing else. From a paired device it is a write like
any other and goes through [all three gates](#writing-three-gates-in-this-order) — the write switch,
the `send` capability, and an `Idempotency-Key`:

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/tasks/$TASK/cancel \
    -H "Authorization: Bearer $TOKEN" -H 'Idempotency-Key: e41b0a77-92cd' -d '{}'
{"error":{"code":"write_disabled","message":"Sending is switched off. Settings → Remote turns it on, and it is off by default because typing into a session runs code on this Mac.","request_id":"1a9f7b3c-0d52-4e88-b6a1-3f7c25d09e14"}}
```

A device may stop work; it may not start any. That asymmetry is the whole shape of this feature's
authorization, and cancel is where it is easiest to see.

### `GET /`, and the things a browser asks for on its own

The web interface, which ships inside the app rather than being fetched from anywhere.
`/?t=<token>` signs a browser in and answers `303` to `/`, so that a QR code can carry a credential
and the address bar does not keep it:

```console
$ curl -s -i "http://127.0.0.1:7717/?t=$TOKEN" | head -3
HTTP/1.1 303 See Other
Connection: close
Content-Length: 0
```

`/manifest.webmanifest`, `/favicon.ico` and `/icon-32|64|180|192|512.png` need no token, and they
have to not need one: a browser asks for the favicon by itself, before and independently of the
page, and an install prompt fetches the manifest's icons the same way. Any other size is
`404 not_found` rather than being rendered on demand — each one is a bitmap held for the life of the
app, and an open-ended size is an open-ended cache.

---

## The Session object

```jsonc
{
  "id": "27439AEE-3736-4AC3-BF80-CE63280B5CCD",  // iTerm2's session UUID, or a tmux pane id
  "backend": "iterm",                            // "iterm" | "tmux"
  "tty": "ttys006",                              // no /dev/ prefix
  "label": "IG 設定指引改進",                      // what the conversation calls itself — see below
  "isClaude": true,                              // is this a Claude Code session or just a shell
  "assistant": "claude",                         // "claude" or "codex"; absent for a plain shell
  "state": "working",                            // "working" | "waiting" | "idle" | "unknown"
  "work_state": "working",                       // exactly one closed user-facing projection
  "line": "Crafting… (2m 45s · ↓ 6.0k tokens)",  // only when state is "working"
  "menu": { "selected": 2, "options": [ … ] },   // only when state is "waiting", and readable
  "coordination": {                                // absent without active peer waits
    "state": "waiting_on_session",                 // or "has_waiters"
    "waitingOn": [ … ], "waitedOnBy": [ … ]
  },
  "coordinator": {                                 // absent except on exact registered row
    "label": "Clawdfather", "status": "online", "commands": [ … ]
  },
  "disposition": {                                 // only with either completion work_state
    "scope": "task", "taskId": "…",             // root reports use scope:"session"
    "evidence": "authenticated_task_delivery",   // or authenticated_session_delivery,
                                                    // or broker_verified_target_landing
    "receiptAt": 1787049596, "title": "review the delivery"
  },
  "agents": [ … ],                               // only when this session has agents out
  "shells": [ … ],                               // only when it left a command running
  "cwd": "/Users/you/code/atrium",          // absent if the terminal would not say
  "sessionId": "841cbb8d-58b1-…",                // assistant id — only with process-bound proof
  "icon": { "accent": "#5CBBA1", "cells": [ … ] } // absent when the project has no icon
}
```

`state` is decided by looking at the screen, and `waiting` is the one worth acting on: it means a
question is on screen. `unknown` means the terminal did not answer, which is not the same as idle
and is deliberately not flattened into it.

`work_state` is present exactly once and is one of `ready`, `working`, `holding`,
`waiting_you`, `waiting_session`, `unknown`, `milestone_complete`, or `work_complete`. (The
retired spellings `waiting_human` and `needs_triage` are these first two renamed; the per-state
reading contract is [`docs/session-states.md`](session-states.md).) It is a broker
projection over separate terminal, task, landing, handoff, coordination, and self-declaration
axes; clients must
not infer it from `idle`. Precedence is: a question stopped on you, a durable peer/owed wait,
unreadable or
missing evidence, current activity, an idle root's live child, the finished task receipt,
authenticated delivery, the session's own `ready`/`holding` claim, then an assistant-free
prompt's `ready`. Current
activity outranks an older receipt. `ready` requires positive evidence — an assistant-free
prompt, or the session's own authenticated declaration (provenance `self`); an idle assistant
without either is `unknown`, which asks nothing of the reader. Only `waiting_you` asks the
person to act.

Beside it ride `work_provenance` ("broker" or "self"), and — when a declaration supplied them —
`work_note` (one line, the session's own words), `work_since`, `work_moved_by`,
`work_person_needed`, plus the independent second axis `owed`
(`{note, since, person_needed, moved_by?, provenance}`), a debt that survives turns until the
session clears it. `holding` appears only with `self` provenance — it has no broker entrance and
is never a fallback — and a self-declaration can never produce either check state.

Beside all of that rides the independent fourth projection, `closeability` — `ready` says this
session can take work, `closeability` says whether it can end, and neither may be read off the
other. The block carries `state` (`blocked`, `needs_attestation`, `safe`, `unknown`), a typed
`reasons` list, the single `mover` when every outstanding reason points at one, `observed_at`,
`session_generation`, `activity_generation`, `obligation_generation`, `provenance`,
`attestation_id`, an opaque `version` for the close route's compare-and-swap, and a `source`
naming its own `observed_at`, the 45-second maximum age and resulting freshness. Its contract, the closed reason vocabulary and the
precedence that makes doubtful evidence outrank a short obligation list are in
[`session-closeability.md`](session-closeability.md). Clients compare `version`; they never
construct or parse it.

An idle root with a live task resolved to its exact current process is `waiting_session`: the
broker already knows it has an outstanding ChildSession. If the root works beside that child it is
`working`; if the child finishes, the wait evidence ends and the root needs its own delivery
receipt after integration. The web client checks the same live `root.terminalId` relationship and
draws `⏳` with the child task title rather than falling back to triage.

`milestone_complete` is one check: the task bound to this exact current assistant process has an
authenticated success result and finish receipt. The binding requires the exact assistant,
terminal and tty, pid plus process start, process-bound rollout/conversation id, and the child's
task-marker proof. A terminal id reused by a later process, or a legacy row missing any proof,
therefore gets no check. An unresolved handoff may name its source in either of two strict
namespaces—exact terminal id or exact process-bound conversation id—and each is compared only in
its own namespace; no prefix/title/tty guessing occurs.

An ordinary root may produce the same one-check milestone for its current turn through
[`POST /v1/orchestrator/sessions/:id/complete`](#post-v1orchestratorsessionsidcomplete). That
receipt is also bound to the exact current process, carries `scope:"session"` and
`evidence:"authenticated_session_delivery"`, and is consumed when the terminal begins its next
observed turn. It is the root's authenticated delivery claim, not independent review or closure of
every obligation in its graph.

`work_complete` is two checks and means only **broker-verified target landing for that task**. The
same task must carry a new-format `landed` receipt whose machine-authenticated git verification
proved its canonical commit is contained by the named local target branch. Legacy landed data,
arbitrary commit text, the task secret alone, or missing verification fields stay at one check.
This does not claim that a whole multi-task graph or its tests are complete. `disposition` names
the receipt scope and typed evidence; for the double check it also carries canonical `commit`,
`target`, `targetCommit`, and `landedAt`. It is output only: agent prose and coordinator advice
cannot write either check.

`coordination` is an independent broker overlay, not another terminal state. `waitingOn` lists
durable file-wait groups that block this session; `waitedOnBy` lists each active waiter for groups
this session owns. A blocked session carries `state: "waiting_on_session"` inside this object while
its top-level `state` remains whatever the terminal is doing — normally `idle`. Therefore it does
not count as waiting for a person and does not trigger the waiting push. Each wait names `id`,
`repository`, canonical relative `paths`, `ownerSessionId`, `releaseCondition`, `createdAt`, and its
current waiter's `reason`; `waitedOnBy` also names `waiterSessionId`. Live session labels are added
as `ownerLabel`/`waiterLabel` when available; ids stay authoritative and unresolved relationships
remain visible when a session disappears. The full waiter arrays live on the wait-registry route.

`coordinator` is a different optional overlay and is never inferred from the row's label, cwd,
title, task ancestry or age. It appears on exactly the current process-bound Session whose
terminal-neutral id, assistant, tty, pid/start and assistant conversation identity match the
durable machine coordinator record. The closed renderer boundary contains only `label`,
`status: "online"` and `commands`; an offline or unknown-liveness durable identity does not decorate a similar or
reused terminal row. Its registration and read-only Bearings contract are documented under
[Machine coordinator identity and Bearings](#machine-coordinator-identity-and-bearings).

`label` is what Clawdline calls the session, and **it is never the tab's title**. On 2026-08-28 an
iTerm2 restart left eleven of fifteen rows reading `Default` — the profile name iTerm2 reports for a
tab nobody has titled — while every one of those sessions had a name sitting in the assistant's own
files the whole time. A tab title is a place a name is *displayed*, not a place a name is *kept*:
anything in the terminal may overwrite it, the assistant clears it, and nothing announces either. So
the sources are records, ranked: a name a person typed for this conversation; the Clawdline task
title, when this app opened the tab; what the conversation calls itself, out of the transcript
Claude Code writes or the thread metadata Codex keeps; and the short handle Claude Code's session
registry derives, `clawdline-cb` and the like. When none of them answer it is `⌘<window>-<tab>`,
which is at least a true statement about this tab and no other.

**And a name is never guessed at.** When nothing can say *which* conversation a tab holds — no
session id from the registry or a hook, and no process start time this Mac could measure — the field
falls back rather than naming the project's most recently written transcript. A plausible name
belonging to somebody else is worse than a coordinate, because nobody checks a name that reads
right. A client should treat `label` as a display string and never as identity; `sessionId` below is
the identity, and it is absent exactly when it could not be proved.

`id` is the terminal's own id and is opaque:
it comes back from iTerm2 or tmux, and a client should carry it around rather than take it apart.

`icon` is the project's mark as colours rather than as a picture — `cells` is rows of `#RRGGBB` or
`null` for transparent, rows need not be the same length, and `accent` is the one colour to tint
text with. A PNG would have been fewer bytes and a worse answer: you are drawing this at a size and
a pixel ratio this end does not know, and a pixel mark that has been resampled is not a pixel mark
any more.

`sessionId` appears only when Clawdline can bind the assistant's durable conversation id to the
process currently occupying this terminal. For Claude, a process registry entry or hook must name
an exact transcript which belongs to that process; for Codex, the current pid must hold the user
rollout whose `session_meta.session_id` is returned. A stale hook, stale rollout or reused tty
therefore omits the field. `id` is still the terminal's id for the tab, and clients should use it
everywhere in this API. In particular, child grouping uses the broker-resolved
`tasks[].root.terminalId`; clients must not compare `root.sessionId` directly with an unverified
session field.

`menu` is the question a `waiting` session is showing, read off the same screen capture the state
came from. Each option is `{"n":1,"label":"Yes","selected":false,"can":true}` — and **`n` is a
keystroke, not a position**. It is what `POST /key` takes, so a client must draw the number it was
given rather than renumbering the rows to run 1…n: renumbering produces a button whose label and
effect disagree. `selected` marks the row the caret is parked on over on the Mac, which is what a
bare Return there would confirm. `can` is false for a row no keystroke reaches — draw it, do not
offer it.

Claude Code draws some dialogs with the numbers hidden — a held cross-session message, a plan
waiting for approval, a permission prompt that defaults to No. Those rows carry an `n` too, counted
here rather than printed there, and `POST /key` still takes it: what changes is only on the Mac,
where the app walks the highlight onto that row instead of typing the digit, because the same flag
that hides the numbers also stops the dialog accepting them. Nothing about the request differs, so
a client needs no branch for it.

`submit` is present only on a **multi-select** — `{"label":"Submit","selected":false}` — and it
changes what pressing a row means. A multi-select's digits *toggle* their rows; nothing is sent
until the button below them is pressed. It has no `n`, because it has no number on screen: press it
with `POST /key` and the body `{"key":"submit"}`, and the Mac walks the highlight onto it and
confirms there. `selected` says the caret is already on it, which is the one case where a bare
Return on the Mac would send. Draw it apart from the rows — it is not another answer.

The whole field is **absent when the menu could not be read**, which is a real state and
not an error: the dialog is undocumented terminal drawing, and a shape this end does not recognise
has to be admitted to rather than guessed at. A client that sees `waiting` with no `menu` should
say so and offer nothing to press.

`agents` is what that session sent off to work in the background, newest first, at most six.

```jsonc
{
  "id": "a42cc4cf998a3ae33",              // Claude Code's id for the agent
  "what": "Search the delivery logs",     // the description whoever spawned it wrote
  "type": "general-purpose",              // the agent type asked for
  "state": "running",                     // "running" | "done" | "failed"
  "depth": 1,                             // 1 for one the session spawned, 2 for one an agent did
  "at": 1787049596,                       // when its transcript was last written to
  "doing": "Bash: swift build",           // the tool it last reached for — running ones only
  "result": "Depth is flat at 0.",        // what it handed back — finished ones only
  "tokens": 18420, "tools": 5, "seconds": 44.2,
  "model": "…"                            // when the sidecar named one
}
```

**This is the one thing in this API that is not read off a screen.** A subagent leaves no mark on
the terminal — a session with three of them out draws exactly the spinner of one thinking hard — so
it comes from the transcripts Claude Code keeps beside the session's own. There is no record that
says an agent *started* and none that says it is still going, so `running` means no ending has been
written yet. A finished one stays in the list for about three minutes with what it returned, then
goes; the record is the transcript, this is the notice. `doing` and `result` are the same slot asked
at two different times, and both can be absent.

`shells` is what that session left running in the background — `Bash` with `run_in_background`,
which is a command that outlives the turn that started it. Newest first, at most six, running ones
only: a command that has finished is already in the transcript as a tool result.

```jsonc
{
  "id": "bvlp3xmku",                              // Claude Code's id for it, the one /bashes shows
  "at": 1787049596,                               // when it last printed something
  "command": "cargo build --release",             // the command line it was started with
  "what": "Build the importer",                   // the description written beside it, if any
  "doing": "[214/318] Compiling importer/rows.rs" // its last line of output, when it has printed one
}
```

`command` is the only field here somebody can match against what they remember asking for, and it
is joined from two transcript records — the assistant's call, marked `run_in_background`, and the
reply that carries the id Claude Code minted for it. Newlines become spaces and both it and `what`
are cut at 160 characters: this goes in a row one line tall, and a heredoc is not a label. Both are
**absent** when the two records straddled a read of the transcript, which loses the command and
never the id — the id is what decides whether anything is running at all.

**This is the field that stops an idle session reading as a finished one.** The terminal says a
command is still going exactly once, on the line where the turn ended — "Cooked for 1h 25m 13s · 1
shell still running" — and then draws an ordinary prompt for as long as the command takes. Every
reading after that says what a session with nothing left to do says.

It is worked out from two things Claude Code already writes. Every `Bash` call gets an output file
under `/tmp/claude-<uid>/…/tasks`, and a background one has `[exited with code 0]` written under its
last line when it ends — so a file with no marker under it belongs to something that has not
finished. That is not enough on its own: a *foreground* command normally has its file deleted when
it returns, but one that was interrupted leaves it behind looking exactly like a build still going.
So the transcript settles it. Claude Code answers a backgrounded `Bash` with *"Command running in
background with ID: …"*, which is what says an id was ever a background command at all, and a file
counts only when it was announced **and** has no ending under it.

## The transcript Entry

```jsonc
{
  "role": "user",                  // "user" | "assistant" | "peer" | "message" | "notice" | "tool"
  "text": "請幫我在網頁加入 favicon",
  "tool": "Bash",                  // present only on a tool call, absent on its result
  "at": 1787049580,                // absent if the record carried no timestamp
  "source": "release-room",        // human-readable session name; peer only
  "sourceMode": "prompting",       // peer sender mode, or "clawdline" for message
  "sourceAssistant": "claude",     // "claude" | "codex"; message only
  "artifacts": [{                   // version-2 message only; 1…6 closed references
    "id":"46cb6d40-c13f-4fea-9cf0-936f86b78da4","media_type":"image/png",
    "byte_count":18422,"width":1280,"height":720,"expires_at":1787983200
  }]
}
```

Oldest first, `limit` counting back from the newest. **A tool call and the result it returned are
both `role: "tool"`**, and the way to tell them apart is `tool`: the call names the tool, the result
does not.

For a `peer` entry, `source` is the other session's human-readable name, never its socket or
transport path. `sourceMode` is the mode that session used to send the message. Either field is
absent when the transcript did not carry a non-empty value.

For a `message` entry, `source` is the live source session's resolved display label,
`sourceMode` is `clawdline`, and `sourceAssistant` is `claude` or `codex`. The envelope's terminal
id does not become a link or action. The body is session-authored Markdown, unlike an inert
`notice` card.

`peer`, `message` and `notice` never stand in for one another: `peer` is Claude Code's native
session-to-session transport, `message` is a session-to-session relay through Clawdline, and
`notice` is Clawdline reporting a broker fact. `source` metadata never appears beside `notice`.

`role: "notice"` is a versioned, single-line Clawdline envelope that either transcript reader
recognized as a whole message. The wire wrapper contains no LF or CR. Its `text` is the
human/model-readable fallback and it also carries a `notice` object. Clients must switch on its
closed `kind` and `state` fields instead of parsing `text`:

```jsonc
{
  "role": "notice",
  "text": "[clawdline] task 3f9a21bc (…) finished: timeout — read …",
  "notice": {
    "kind": "task_finished",       // see the five-value version-2 set below
    "audience": "root",            // or "parent"
    "task": {"id": "3f9a21bc-…", "title": "Project portrait"},
    "state": "timeout",            // success | failure | timeout | cancelled | spawn_failed
    "result_path": "/tmp/.clawdline/3f9a21bc-…/result.json",
    "notice_id": "6b1d46cb-1111-4222-8333-444444444444",
    "ack_path": "/v1/orchestrator/tasks/3f9a21bc-…/completion/ack",
    "outstanding": 0,
    "claims_released": true,
    "child_may_still_write": true
  }
}
```

Version 1 is a closed legacy schema containing only `task_finished` and `workspace_overlap`, and
literal version-1 transcript rows continue to decode. Version 2 is the current writer schema and
has exactly five kinds: those two plus `file_wait_request`, `file_wait_release`, and
`handoff_receipt`. Current `task_finished` writers include `notice_id` and `ack_path`; readers also
accept the original version-2 completion shape without them for transcript compatibility, but that
legacy shape has no delivery identity and cannot be ACKed. For `workspace_overlap`, `notice` has
`kind`, `audience`, `task`, and a non-empty
`overlaps` array of `{"task":{"id":…,"title":…},"path":…}`. The file-wait shapes are:

```jsonc
{"kind":"file_wait_request","audience":"owner","wait_id":"0d9579fb-…",
 "repository":"/Users/you/code/clawdline","paths":["Sources/Foo.swift"],
 "waiter_session_id":"WAITER-TERMINAL-ID","reason":"unfinished diff",
 "release_condition":"commit or explicit release"}
{"kind":"file_wait_release","audience":"waiter","wait_id":"0d9579fb-…",
 "repository":"/Users/you/code/clawdline","paths":["Sources/Foo.swift"],
 "commit":"abc123","note":"tree rechecked"} // commit and note are independently optional
{"kind":"handoff_receipt","audience":"source","handoff_id":"7c1e9b02-…",
 "title":"Cloud planning line","assistant":"codex","project_dir":"/tmp/repo",
 "state":"picked_up"} // or first_line_failed; title is optional
```

No notice has payload-defined HTML, CSS, actions, or executable links. The bundled Web client
escapes every displayed payload field, chooses the card wording and state styling itself, and keeps
both versions non-clickable. Malformed, partial, unknown-version, extra-field, and quoted lookalikes
are never dropped and never partly
interpreted: they keep their full visible text and stay whatever the row they arrived in already
was. In an ordinary turn that is `role: "user"`; quoted inside a cross-session envelope it is
`role: "peer"`, and quoted inside a Clawdline session-message body it is `role: "message"`, because
who sent a message is a stronger fact than what its text looks like.

`tool` is whatever the assistant calls it, so the vocabularies differ: Claude Code's are `Bash`,
`Edit`, `Read` and the rest; Codex's are `shell` for a command, `edit` for a file change,
`server.tool` for an MCP call and the extension's own kind — `web.search` — for a plugin. Treat it
as a label to show, not as a set to switch on.

```console
$ curl -s "http://127.0.0.1:7717/v1/sessions/$ID/transcript?limit=2" -H "Authorization: Bearer $TOKEN"
{"signature":"38391392-1787049299","entries":[{"role":"tool","at":1787049285,"text":"ok"},{"tool":"Bash","text":"sed -i '' 's/# 993 checks, a couple of seconds/# 1007 checks, a couple of seconds/' README.md CONTRIBUTING.md","at":1787049294,"role":"tool"}]}
```

`text` is plain text and may be long, may be empty, and for an assistant turn is Markdown. Nothing
here is folded or summarised — the app's own transcript pane collapses long runs of tool calls when
it draws them, and that is a drawing decision which does not travel over the wire.

## The error envelope

```json
{"error":{"code":"unauthorized","request_id":"920e5e58-67c0-4367-bec3-6ac4af3389ec","message":"This needs a paired device."}}
```

| `code` | status | when |
|---|---|---|
| `bad_request` | 400 | a missing or unusable field, or a mutating call with no `Idempotency-Key` |
| `unauthorized` | 401 | no token, or one that is not ours |
| `forbidden` | 403 | wrong `Host`, a cross-site request, a foreign `Origin` on a `POST`, or a device that may read and not send |
| `wrong_code` | 403 | that pairing code is not the one on the Mac. `tries_left` says how many are left |
| `expired` | 403 | the pairing lapsed, or five wrong codes used it up. Start a new one |
| `write_disabled` | 403 | the second switch is off. Its own code because it is not the client's fault and no retry fixes it |
| `not_found` | 404 | no such session, no such place, or no such route |
| `terminal_closed` | 409 | the terminal a session would start in is not running |
| `terminal_unsupported` | 409 | the terminal in Settings is not one a session can be started in |
| `depth_exceeded` | 409 | a session already at the bottom of the tree tried to dispatch a task of its own |
| `already_done` | 409 | that task has already reported; the first report wins |
| `bad_task` | 422 | a `task.json` that is missing, unparseable, or out of range. `message` names the field |
| `rate_limited` | 429 | a sliding window of counted attempts is full — pairing attempts, dispatches per ten minutes, schedules written per ten minutes, agent notifications per hour. What was counted ages out of the window on its own; nothing is draining, which is what separates this from `busy` |
| `busy` | 429 | a queue on this Mac is full — something is already in hand and will drain in seconds. On `/v1/voice`, one recording is being read and one is waiting. On `/v1/sessions/:id/info` and `/v1/places`, eight slow reads are already in hand — `/v1/sessions/:id/transcript` stands in that same queue but is never refused by this number. The terminal broker admits eight operations globally and two per terminal session, shared by remote terminal mutations, terminal-bearing orchestrator writes and automatic child close; a same-key in-flight retry joins without consuming another place. These refusals are not filed under an `Idempotency-Key` |
| `over_capacity` | 429 | the root's child slots are full — `orchestrator_max_children` — or the whole Mac's are. `retry_after` is seconds. (`rate_limited` covers the other orchestrator limit: dispatches per ten minutes) |
| `terminal_io_failed` | 502 | a terminal mutation reached its isolated command queue but the selected backend did not complete the handoff; this includes a bounded tmux subprocess timeout after cleanup |
| `iterm_attention_required` | 502 | an iTerm Apple Event timed out or returned a malformed list; `app` is `iTerm2`, `action` is `answer_dialog`, and a well-formed later list response re-enables automation. The timed-out event may still execute later |
| `internal` | 500, 502 | another internal operation failed, including a tab that would not open |
| `no_whisper` | 503 | `/v1/voice` only: this Mac has nothing to transcribe with. `reason` is `no_binary` or `no_model` |

A client that has handled one of these has handled all of them. Branch on `code` — the status is
there for the layers between you and this, and `message` is a sentence for a person that may be
reworded or translated at any time.

Two replies carry no envelope: `431` when the request headers exceed 64 KiB, which is `text/plain`
and empty, and the `303` above, which has no body at all.

---

## The event stream

```console
$ curl -sN http://127.0.0.1:7717/v1/events -H "Authorization: Bearer $TOKEN"
event: hello
id: 6
data: {"protocol":1,"write":false,"version":"0.5.0"}

event: sessions
id: 7
data: {"sessions":[…],"at":1787049596}

event: transcript
id: 8
data: {"id":"B3ACDE0D-DE72-4E58-A99A-AB845A539C90","signature":"38603412-1787049597","at":1787049597}

: ping

event: sessions
id: 9
data: {"sessions":[…],"at":1787049611}

: ping
```

`hello` first, then the current state immediately. A `sessions` frame follows when terminal/session
state changes; its `data` is byte-for-byte the payload of `GET /v1/sessions`. A `transcript` frame
contains only a session id and file signature when that assistant's transcript file changes. The
client fetches the authenticated transcript route for content, so conversation text never rides
the event stream. Transcript events are not replayed; after reconnect, quietly refetch the
currently open transcript once to cover bytes written while offline.

**It sends the whole list on every change rather than a diff, and that is the design.** A client
that has just reconnected — a phone coming out of a tunnel, a laptop waking up — is level with the
server as soon as the first frame lands, without asking for anything and without replaying anything
it missed. There is no resume protocol because there is nothing to resume; `Last-Event-ID` is not
read and does not need to be. The list is a handful of sessions on the biggest desk anybody has, so
the bytes saved by a diff would buy one thing: a category of bug where the client's copy and the
server's copy disagree and nothing on either side can tell.

Two details a client author will notice:

- **`id:` is monotonic but not contiguous for you.** The counter is the server's and is shared by
  every open stream, so a client watching alone still sees gaps whenever another one is connected —
  `6, 7, 9` above is a second client, not a dropped frame. Do not treat a gap as loss.
- **`: ping` every fifteen seconds.** Nothing reads it. Its whole job is to be bytes, so that a
  proxy or a phone radio that drops idle connections finds that this one is not idle.

`EventSource` cannot set an `Authorization` header, which is why the cookie exists; from a script,
send the header and read the stream yourself.

---

## Writing: three gates, in this order

A mutating call passes through all three, and they are separate because they are three different
people's decisions.

1. **The switch.** `remote_write` — the owner of the Mac. Off, and the reply is `write_disabled`
   before anything else is looked at, which is why you cannot observe the other two while it is off.
2. **The capability.** Does *this* device have `send`. `403 forbidden` — *This device may read, and
   not send.*
3. **The key.** `Idempotency-Key`, non-empty, or `400 bad_request`.

A key is remembered for **ten minutes** with the response it produced, and a repeat within that
window gets the stored reply without the work being done again. Two things follow. **Use a fresh
UUID per request** — the key is looked up on its own, not against the route or the body, so reusing
one for a different call inside ten minutes returns the first call's answer. And **a retry after a
timeout is safe**: that is what the header is for, because the failure being defended against is a
phone that changed networks halfway through a `send` and a user who has no way to know whether their
prompt arrived once or twice.

Every write is recorded in `~/.config/clawdline/remote-audit.jsonl` — the ones that failed as well,
and the line is appended *before* the answer goes back, because the interesting case for a log is
the one where something went wrong afterwards. That file, and what to do with it, is
[in the other page](remote.md#when-something-looks-wrong).
