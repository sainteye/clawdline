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
| `GET` | `/v1/sessions/:id` | token | `read` |
| `GET` | `/v1/sessions/:id/transcript` | token | `read` |
| `GET` | `/v1/sessions/:id/agents/:agentId` | token | `read` |
| `GET` | `/v1/sessions/:id/shells/:shellId` | token | `read` |
| `GET` | `/v1/sessions/:id/links` | token | `read` |
| `GET` | `/v1/sessions/:id/info` | token | `read` |
| `GET` | `/v1/sessions/:id/skills` | token | `read` |
| `GET` | `/v1/sessions/:id/git` | token | `read` |
| `GET` | `/v1/projects` | token | `read` |
| `GET` | `/v1/places` | token | `read` |
| `GET` | `/v1/places/:id/sessions` | token | `read` |
| `GET` | `/v1/events` | token | `read` |
| `POST` | `/v1/places/:id/start` | token + key | `send` **and** the write switch |
| `POST` | `/v1/places/:id/start/:assistant` | token + key | `send` **and** the write switch |
| `POST` | `/v1/places/:id/resume/:session` | token + key | `send` **and** the write switch |
| `POST` | `/v1/sessions/:id/send` | token + key | `send` **and** the write switch |
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
| `POST` | `/v1/orchestrator/notify` | orchestrator token | — |
| `GET` | `/v1/orchestrator/tasks` | orchestrator token, **or** token | `read` |
| `GET` | `/v1/orchestrator/tasks/:id` | orchestrator token, **or** token | `read` |
| `POST` | `/v1/orchestrator/tasks/:id/notify` | that task's secret | — |
| `POST` | `/v1/orchestrator/tasks/:id/complete` | that task's secret | — |
| `POST` | `/v1/orchestrator/tasks/:id/cancel` | orchestrator token, **or** token + key | `send` **and** the write switch |
| `GET` | `/v1/orchestrator/schedules` | orchestrator token, **or** token | `read` |
| `GET` | `/v1/orchestrator/schedules/:id` | orchestrator token, **or** token | `read` |
| `POST` | `/v1/orchestrator/schedules` | token + key | `send` **and** the write switch |
| `POST` | `/v1/orchestrator/schedules/:id/run` | orchestrator token | — |
| `GET` | `/`, `/index.html`, `/manifest.webmanifest` | — | — |
| `GET` | `/favicon.ico`, `/icon-<size>.png` | — | — |

Every approved device has `read`. `send` is granted to all of them together by
Settings → Remote → **Let paired devices type**, and taken back from all of them together — there is
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
{"ok":true,"version":"0.5.0","build":1787096354,"protocol":1,"write":false,"auth":false,"password":false,"authed":false}
```

| field | |
|---|---|
| `version` | the app's, for a person |
| `build` | which build, as opposed to which release — the executable's modification time. `version` is the same string for every build of a release, so a long-lived page watching only that could never tell it had fallen behind. Compare it to what you saw first; if it moved, the app was rebuilt under you |
| `protocol` | this document's; bumped when a client would have to change |
| `write` | is the second switch on — **draw the UI from this**, because saying "you may not" once is kinder than a button that fails when pressed |
| `auth` | has anybody paired a device or set a password. The local token does not count |
| `password` | is there a password to offer at all — separate from `auth`, so a page can decide whether to draw that door rather than offering it blind and letting somebody learn from a 401 that it was never set |
| `authed` | did *this* request carry a credential that works |

### `GET /v1/sessions`

Everything the bar knows, as of one reading.

```console
$ curl -s http://127.0.0.1:7717/v1/sessions -H "Authorization: Bearer $TOKEN" \
    | jq '{at, sessions: [.sessions[] | {id, label, state, cwd}]}'
{
  "at": 1787049596,
  "sessions": [
    {
      "id": "35D87610-E7F4-4A9A-95A0-11947CF5115C",
      "label": "設計基本問題和股票相關聊天內容",
      "state": "idle",
      "cwd": "/Users/you/code/cairn"
    },
    {
      "id": "B3ACDE0D-DE72-4E58-A99A-AB845A539C90",
      "label": "評估動態島實現機制",
      "state": "working",
      "cwd": "/Users/you/code/clawdline"
    },
    {
      "id": "27439AEE-3736-4AC3-BF80-CE63280B5CCD",
      "label": "IG 設定指引改進",
      "state": "idle",
      "cwd": "/Users/you/code/atrium"
    }
  ]
}
```

Four fields per session are picked out there so the reply fits on this page; the whole object is
[below](#the-session-object). `at` is when the reply was built, not when the reading was taken.

### `GET /v1/sessions/:id`

The same object, alone, under `session`. `404 not_found` if that id is not currently on screen —
which includes a session that has since been closed, since ids come from the terminal and are not
kept after the tab is gone.

```console
$ curl -s http://127.0.0.1:7717/v1/sessions/27439AEE-3736-4AC3-BF80-CE63280B5CCD \
    -H "Authorization: Bearer $TOKEN"
{"session":{"id":"27439AEE-3736-4AC3-BF80-CE63280B5CCD","isClaude":true,"state":"idle","icon":{"accent":"#5CBBA1","cells":[["#2F6B5E","#EEF6F4","#EEF6F4","#EEF6F4","#EEF6F4","#EEF6F4","#2F6B5E"],["#2F6B5E","#EEF6F4","#2F6B5E","#2F6B5E","#2F6B5E","#EEF6F4","#2F6B5E"],["#2F6B5E","#EEF6F4","#2F6B5E","#EEF6F4","#2F6B5E","#EEF6F4","#2F6B5E"],["#2F6B5E","#EEF6F4","#2F6B5E","#2F6B5E","#2F6B5E","#EEF6F4","#2F6B5E"]]},"tty":"ttys006","backend":"iterm","label":"IG 設定指引改進","sessionId":"841cbb8d-58b1-4765-9a71-bcdba19bcfef","cwd":"/Users/you/code/atrium"}}
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
  {"label":"backlog","url":"file:///Users/you/code/repo/artifacts/backlog.html","kind":"artifact","state":"","local":true}
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
deploy status, the servers from the project's own `status` command, the backlog page from
whatever produced it. An untrusted dev stack stays silent rather than being probed, and a
`file://` entry is handed over as a path so a client can decline it honestly.

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
| `session` | `id` and `assistant` always; `sessionId` with hooks installed; `model` when a transcript has named one — the **last** model the transcript names, so a session that switched mid-way shows what it is on now; `cwd`, `startedAt` and `seconds` (its age, as of this answer) when the process could be found |
| `permission` | Claude Code's current permission mode and the Shift-Tab cycle order. `current` is `auto`, `manual`, `acceptEdits`, `plan`, or `unknown`; `manual` specifically means the screen was readable and showed no mode line, while `unknown` means the screen capture was absent or empty. **Absent for Codex sessions**, which do not have this mode cycle |
| `usage` | the transcript's token totals — `input`, `output`, `cacheRead`, `cacheWrite`, `total` — with `model` and, for Claude, `costUsd` at list price. **Absent** when no transcript has been found, which is not the same as zero |
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

**A route rather than a field on the session**, for the reason `/links` gives and one more: on
top of that route's `git`, this one reads the transcript, which can be fifty megabytes. Free when
a card is opened; not something to do on every beat of the stream. Everything here is read and
nothing is written — the `git` runs with `GIT_OPTIONAL_LOCKS=0` and a deadline, and nothing is
asked of GitHub that `/links` did not already ask.

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

### `GET /v1/places/:id/sessions`

**What Claude Code has already recorded in one place**, so a client can offer to carry one of them
on instead of starting something new. Reading, not starting — it discloses the titles of
conversations held in a directory whose name this token could already see in `/v1/places`.

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

**`title` is read, never invented.** It is the name somebody renamed the conversation to, or the
one Claude Code gave it, or — for the few transcripts that carry neither — the opening of the first
thing a person typed into it. A transcript with none of the three is a tab that opened and closed,
and it is left out rather than listed as an untitled row somebody has to guess at.

**`live` means something is writing to that transcript right now.** Resuming one of those would put
a second process on the same file, so a client is told which they are rather than left to find out.
It is a fact about the instant it was read.

**It is not instant on a large project.** Naming a conversation means reading its transcript, and
these run to tens of megabytes; the first call for a project takes on the order of a second and
every one after it is served from memory until the files change. Show a waiting line.

**Claude Code only**, which is what `assistant` in the reply says out loud. Codex records the same
conversations somewhere else and keeps their names in a process this list will not start, so there
is nothing to show rather than nothing to resume. A place with no Claude Code transcripts answers
`{"sessions": []}`, which is ordinary — it is what a directory only Codex has ever been opened in
looks like. An id that is not on `/v1/places` is `404 not_found`.

### `POST /v1/places/:id/resume/:session`

Opens a terminal tab in that place and picks that conversation back up in it — `claude --resume
<id>`. Everything about *where* and *whether* is
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
the listing the server builds for that directory at that moment. An id nobody was handed is
`404 not_found`, never a string on a command line.

The shape check is exact rather than merely shell-safe, and that is worth knowing if you are
building on this: `--resume` takes an **optional** value, so a value the CLI cannot read as an id
it treats as a search term for, and opens its own picker in a tab nobody is sitting at. The failure
being refused is a session that never starts, not only an injection.

The refusals are `…/start`'s, plus `not_found` for a conversation that is not on the listing — a
transcript deleted since you last looked, an id from another project, or one that was never real.
**Resuming a `live` conversation is not refused**, because the Mac cannot always tell which tab has
a transcript open; a client that has been told `live` should go to that session instead of asking
for this.

### `POST /v1/places/:id/start`, `POST /v1/places/:id/start/:assistant`

Opens a terminal tab in that place and runs an assistant in it. Without the last segment that is
`claude`, which is what this route did before there was anything else to run — an existing client
keeps working and does not have to know Codex exists.

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/places/3b9e26c1587facfd/start \
    -H "Authorization: Bearer $TOKEN" -H 'Idempotency-Key: 6f1c9d3a-41b2' -d '{}'
{"error":{"code":"write_disabled","message":"Sending is switched off. Settings → Remote turns it on, and it is off by default because typing into a session runs code on this Mac.","request_id":"2fd356e8-bef8-4f54-a312-851c0cfa8045"}}
```

With the switch on: `{"ok":true,"id":"…","backend":"iterm","assistant":"claude","place":"…","cwd":"…","at":…}`.

To open Codex instead, name it:

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/places/3b9e26c1587facfd/start/codex \
    -H "Authorization: Bearer $TOKEN" -H 'Idempotency-Key: 6f1c9d3a-41b3' -d '{}'
{"ok":true,"id":"…","backend":"iterm","assistant":"codex","place":"…","cwd":"…","at":…}
```

**There is no request body.** Not "the body is optional" — it is not read, and there is no field
anywhere on this route that a directory or a command could be written into. The `id` in the path is
resolved against a list the server builds at that moment and the server's own copy of the path is
what gets used, so an id nobody was handed is `404 not_found` and never a directory.

**The assistant is a path segment for the same reason.** It is matched against the two names in
`/v1/places`' `assistants` and nothing else — `…/start/emacs` is `404 not_found` with
`"No assistant named that"`, decided before the place is even looked up — and what runs is a
literal picked out of a closed list, never a string that reaches a shell. Each of them runs with
no arguments. Picking a recorded conversation back up is the second named action this said it
would be if it were ever wanted — [`POST /v1/places/:id/resume/:session`](#post-v1placesidresumesession),
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
anything else is `400 bad_request`. Failure to reach the terminal is `502` with code `internal` and
whatever the terminal said as the message.

### `POST /v1/sessions/:id/key`

Answers a menu with a single keystroke. `{"key":"1"}`…`{"key":"9"}`, `{"key":"tab"}`, or
`{"key":"shift+tab"}`. The last sends back-tab (`ESC [ Z`) as one terminal sequence and is used
to cycle Claude Code's permission mode. Anything else is `400 bad_request`, and the allowlist is
checked before anything goes looking for a terminal.

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

Ends the session and closes the terminal tab it occupied. No body.

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/sessions/$ID/end \
    -H "Authorization: Bearer $TOKEN" -H 'Idempotency-Key: 3d9b7c14-55e2' -d '{}'
{"ok":true}
```

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
that happened and the tab was being held for a reader who is leaving. **Both levels go**, deepest
first: what this session's children handed on in turn goes before they do, and it is collected
from the finished children as well as the live ones. Closing a tab by hand cascades to nothing:
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
one best-effort typed delivery receipt. This is intentionally the loose resolver: a conversation
id or the watched terminal's own id may identify the sender. Task `root.session_id` does not have
that terminal-id shortcut.

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

### `POST /v1/orchestrator/tasks`

**Dispatch.** One session asks for a task to be run by another one: Clawdline opens a terminal tab
in the task's directory, starts the assistant the task named, types a first message into it, and
watches for the answer. The concept, the trust model and the file formats are
[`docs/orchestrator.md`](orchestrator.md); what follows is the request.

The body is two fields and neither of them is the work:

```console
$ TASK=$(uuidgen | tr 'A-Z' 'a-z'); SECRET=$(openssl rand -hex 32)
$ umask 077; mkdir -p /tmp/.clawdline/$TASK/artifacts   # …and write task.json into it
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/tasks \
    -H "X-Clawdline-Orchestrator: $(cat ~/.config/clawdline/orchestrator-token)" \
    -H 'Content-Type: application/json' \
    -d "{\"task_id\":\"$TASK\",\"secret\":\"$SECRET\"}"
{"ok":true,"task":{"id":"3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234","state":"spawning","kind":"image","title":"Project portrait","assistant":"codex","projectDir":"/Users/you/code/clawdline","created":1787100000,"spawnedAt":1787100002,"dir":"/tmp/.clawdline/3f9a21bc-8d4e-4c1a-9f2b-6a7e5d0c1234","root":{"sessionId":"841cbb8d-58b1-4765-9a71-bcdba19bcfef","assistant":"claude","label":"clawdline main"},"child":{"terminalId":"9A1F…","backend":"iterm"}}}
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

Eight refusals, and a client should branch on all of them:

| `code` | status | |
|---|---|---|
| `forbidden` | 403 | the header is missing or wrong — or `orchestrator_enabled` is off |
| `bad_task` | 422 | `task.json` is missing, unparseable, or a field is out of range — including an `isolation` other than `none` or `worktree`, an invalid `isolation_base`, `model`, `permission_mode`, `plan`, `claims`, or `serialize`. `claims` is 0…32 unique relative POSIX paths of 1…1024 characters with no `/` prefix or `..` component; `message` names every invalid item |
| `worktree_unavailable` | 409 | worktree isolation was requested but the repository has no commit to use as a base or the destination volume has less than 2 GB available. This is an environment refusal rather than malformed JSON |
| `workspace_busy` | 409 | a live task from another definitely identified root reserved an equal, ancestor, or descendant claim. The error object carries `blocking_task`, `title`, nullable `root_label`, Unix-second `created`, absolute `conflict_paths`, and advisory `retry_after`. The rejected task is not registered and does not spend dispatch rate-limit budget |
| `depth_exceeded` | 409 | **the caller is already as deep as this Mac goes.** A root's child may dispatch; that child's may not. `orchestrator_max_grandchildren` of `0` puts the floor back at one level. Not a retry — stop |
| `over_capacity` | 429 | this dispatcher's slots are full (`orchestrator_max_children` from a root, `orchestrator_max_grandchildren` from a child), or the whole Mac's are. Registered `queued` tasks count toward these limits even before a tab opens, preventing an unbounded queue. The error object carries `retry_after` in seconds, and `message` says which |
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
          "created":1787696800,
          "conflict_paths":["/Users/you/code/clawdline/Sources/Orchestrator.swift"],
          "retry_after":60,"request_id":"c1e0b7a4-2f5d-4a19-8b0e-71c93d5ea882"}}
```

The failed attempt writes `orchestrator.claims.blocked` to the audit log but does not count toward
the ten-minute dispatch rate limit. The blocking context is enough for the caller to choose
whether to wait, coordinate with that root, or escalate.

A `200` means *registered and being opened*, not *running*. `state` is `queued` or `spawning` when
this answers and the child has typed nothing yet; watch the record, or wait to be told.

`"isolation":"worktree"` asks for a clean private checkout and a delivery branch named
`clawdline/task/<complete-task-id>`. Optional `isolation_base` is resolved to a commit; without it,
the base is `HEAD`. A dirty base succeeds with a warning because its uncommitted files are absent
from the isolated checkout. Unknown isolation values are refused rather than silently sharing the
tree. The checkout lives under `~/Library/Application Support/Clawdline/worktrees/`; `dir` remains
the task protocol directory under `/tmp/.clawdline`.

An optional `serialize` array in `task.json` makes named operations machine-global mutexes. A task
leaves `queued` only when it can acquire every name together; shared names are FIFO across roots,
and every terminal state releases all of them. While blocked, the response's task record carries
`"waiting_on":["<task-id>",…]`; the field is absent when nothing blocks it. Queue waiting does not
consume `timeout_minutes`, whose clock starts at `briefedAt`. Cancelling a queued task is immediate.
Queued tasks do count toward children/grandchildren and machine-wide descendant capacity from the
moment they are registered.
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
              "message":"Task 3f9a21bc-… shares claimed paths with task a70c5e11-…: /Users/you/code/clawdline/Sources/Orchestrator.swift."}]}
```

The wire field has three distinct states, preserved through the registry and all GET responses:

| `claims` value | Meaning | Effect |
|---|---|---|
| one or more paths | declared write scopes | freezes and holds those lease keys |
| `[]` | explicit read-only declaration | holds no lease, never conflicts or receives claims `409`, and participates in L1 silence |
| field absent | unknown write scope | holds no lease and retains L1 directory warnings |

`[]` gives read-only work a proactive, harmless way to say so. Silence now has only one meaning:
both sides supplied declarations whose frozen scopes do not intersect; omission says nothing.

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

[`POST /v1/orchestrator/schedules`](#post-v1orchestratorschedules) below creates one; nothing over
HTTP edits or deletes one. The Settings app can change an existing source file's top-level
`enabled` boolean with one switch; every other field remains user-owned.

### `GET /v1/orchestrator/schedules/:id`

One schedule in full, under `schedule`. Everything the list row carries, plus `file`, `when` in the
file's own spelling, `close_tab`, `catch_up_hours`, `notify_on_failure`, and the whole `task`
template — `project_dir`, `instructions` and all. The list deliberately exposes none of that, which
is the right amount for a row and the wrong amount for the only screen where somebody can check
what a schedule actually does. Same door as the list: `read` is enough, and the orchestrator token
works too. `404 not_found` for an id that is unknown, invalid, or not an id at all.

```json
{"schedule":{"id":"4d2f54ce-…","title":"Publish the next post","enabled":true,
             "file":"4d2f54ce-….json","next_fire":1787880600,
             "when":{"at":"09:30","days":["mon","wed","fri"]},
             "close_tab":"on_success","catch_up_hours":6,"notify_on_failure":true,
             "task":{"assistant":"codex","project_dir":"/Users/me/code/blog",
                     "instructions":"Publish the next ready post."}}}
```

### `POST /v1/orchestrator/schedules`

Makes a schedule file. The only route that writes one, and it never edits or deletes one.

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/schedules \
    -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
    -H 'Idempotency-Key: 3f9a1c04-77e2' \
    -d '{"title":"Publish the next post","at":"09:30","days":["mon","wed","fri"],
         "place_id":"3f2a91c47e0b5d68","assistant":"codex",
         "instructions":"Read the checklist and publish the next ready post."}'
{"ok":true,"schedule":{"id":"4d2f54ce-…","title":"Publish the next post","enabled":true,"next_fire":1787880600}}
```

Required: `title`, `at` as `HH:MM` in the Mac's local time, `days` as `"daily"` or a non-empty
weekday array, `place_id`, `assistant`, `instructions`. Optional: `enabled` (default `true`),
`close_tab`, `catch_up_hours`, `notify_on_failure`, `timeout_minutes`, `model`. An empty `model`
is left out of the file rather than written into it. `days` has no default — picking `daily` for
somebody would be choosing how often their work runs — while `enabled` does, because a schedule
somebody has just asked for is on.

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

### `POST /v1/orchestrator/schedules/:id/run`

Runs one valid schedule immediately, ignoring `enabled` and the wall clock. This needs
`X-Clawdline-Orchestrator`; a successful response is the ordinary dispatch response. It returns
`404 not_found` for an unknown or invalid schedule, `403 orchestrator_disabled` when dispatch is
off, and `409 schedule_active` while any task from the schedule is non-terminal or its dispatch is
already queued. A successful manual run records the current occurrence as handled when it is at or
after that occurrence; running before the next scheduled time does not consume that future fire.

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
  "permission": "full",         // ask | edits | full — what was used, after this Mac's ceiling
  "projectDir": "/Users/you/code/clawdline",
  "isolation": "worktree",     // absent for the shared-tree default
  "created": 1787100000,        // integer unix seconds, like every time in this API
  "depth": 1,                   // 1 for one a person's session dispatched, 2 for one its child did
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
  "summary": "…",               // finished tasks; the child's own sentence
  "artifacts": ["artifacts/project-portrait.svg"],
  "usage": {"input": 48210, "output": 9330, "cacheRead": 412880, "cacheWrite": 31200,
            "total": 501620, "model": "claude-sonnet-4-5", "costUsd": 0.4243}
}
```

**The secret is not in here and never will be.** The durable identity is its SHA-256. While a
serialized task is still queued, the app also keeps a temporary encrypted copy in its private
registry so startup can resume the queue; it is removed before spawning and never enters an API
record. The at-rest key is a dedicated random 32-byte value in the app's private config directory,
not a value derived from any request credential.

`child.terminalId` is in the same space as every `id` in `/v1/sessions`, which is what makes the
child row in a session list joinable to the task that opened it. `root.assistant` preserves the
validated `task.json` field; an absent or explicit-null input is omitted, preserving legacy rows,
which resolve as Claude. Empty strings and values other than `claude` or `codex` are refused.
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

`projectDir` never changes meaning: it is the repository/subdirectory the task concerns.
`worktree.path` is the isolated checkout root, while `dir` is still the unrelated protocol and
artifact directory. The broker, not the child, reads `head`, `commits`, and `dirty` from git. A
serialized isolated task names `isolation: "worktree"` while queued but omits the `worktree`
object until its tab exists: its base is resolved only when it acquires its mutex, so no preliminary
SHA is presented as the receipt for what the child actually started from.

The same payload goes out on [the event stream](#the-event-stream) as an `orchestrator` frame
whenever any record changes, and once when a stream opens, right after `hello` and `sessions`.

### `POST /v1/orchestrator/tasks/:id/notify`, `POST /v1/orchestrator/notify`

These routes let an agent send **content** the user is waiting for, through the same per-subscription
RFC 8291 WebPush path as Clawdline's state notifications. The task form is for a child: its secret
is accepted in `X-Clawdline-Task-Secret` or the JSON body, exactly like `/complete`, and is compared
in constant time with the task's stored hash. The header is the canonical form shown to children.
It is valid while the task is live and for 60 seconds after `finished_at`:

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/orchestrator/tasks/$TASK/notify \
    -H "X-Clawdline-Task-Secret: $SECRET" -H 'Content-Type: application/json' \
    -d "{\"title\":\"Today's forecast\",\"body\":\"Sunny, high 27°C.\"}"
{"failed":0,"ok":true,"sent":1}
```

The root form is for a local root session or script and takes the orchestrator token:

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

Agent content deliberately bypasses the automatic push preference switches: it is an explicit
delivery requested by the user through a root, task, or schedule, not a state/finish notice.
Integrating agent-content delivery into a separate user preference remains backlog work.

| `code` | status | |
|---|---|---|
| `unauthorized` | 401 | the root request has no recognized credential and stops at the device-auth door |
| `forbidden` | 403 | the task secret is absent or wrong, or a paired device tries to replace the root orchestrator token |
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

### `POST /v1/orchestrator/tasks/:id/cancel`

Stop it. The task goes to `cancelled` and the child's terminal is ended the polite way
[`/v1/sessions/:id/end`](#post-v1sessionsidend) does it — the assistant is asked to leave through its
own word, then the tab closes. A task that is already finished answers `200` with its record
unchanged; there is nothing to cancel and nothing went wrong.

**What the task handed on goes with it**, deepest first and for the same reason a closing root
takes both levels: work whose asker has just been stopped is work nobody is waiting for. Those
cancellations carry `why=parent_cancelled` in the audit log. The reply names only the task that
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
  "label": "IG 設定指引改進",                      // the tab title, cleaned up — see below
  "isClaude": true,                              // is this a Claude Code session or just a shell
  "assistant": "claude",                         // "claude" or "codex"; absent for a plain shell
  "state": "working",                            // "working" | "waiting" | "idle" | "unknown"
  "line": "Crafting… (2m 45s · ↓ 6.0k tokens)",  // only when state is "working"
  "menu": { "selected": 2, "options": [ … ] },   // only when state is "waiting", and readable
  "agents": [ … ],                               // only when this session has agents out
  "shells": [ … ],                               // only when it left a command running
  "cwd": "/Users/you/code/atrium",          // absent if the terminal would not say
  "sessionId": "841cbb8d-58b1-…",                // Claude Code's own id — only with hooks installed
  "icon": { "accent": "#5CBBA1", "cells": [ … ] } // absent when the project has no icon
}
```

`state` is decided by looking at the screen, and `waiting` is the one worth acting on: it means a
question is on screen. `unknown` means the terminal did not answer, which is not the same as idle
and is deliberately not flattened into it.

`label` is the tab title with two things taken off: iTerm's ` (job name)` suffix, which helps nobody
pick a tab, and the status glyph Claude Code puts on the front — which is now a frame of an
animation, so a title kept whole would change four times a second and stop being a label. When
nothing is left, it falls back to `⌘<window>-<tab>`. `id` is the terminal's own id and is opaque:
it comes back from iTerm2 or tmux, and a client should carry it around rather than take it apart.

`icon` is the project's mark as colours rather than as a picture — `cells` is rows of `#RRGGBB` or
`null` for transparent, rows need not be the same length, and `accent` is the one colour to tint
text with. A PNG would have been fewer bytes and a worse answer: you are drawing this at a size and
a pixel ratio this end does not know, and a pixel mark that has been resampled is not a pixel mark
any more.

`sessionId` appears only when Claude Code's hooks are installed ([`docs/hooks.md`](hooks.md)). It is
Claude Code's id for the conversation and the name of its transcript file; `id` is the terminal's id
for the tab, and they are different things. Use `id` everywhere in this API.

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
  "role": "user",        // "user" | "assistant" | "tool"
  "text": "請幫我在網頁加入 favicon",
  "tool": "Bash",        // present only on a tool call, absent on its result
  "at": 1787049580       // absent if the record carried no timestamp
}
```

Oldest first, `limit` counting back from the newest. **A tool call and the result it returned are
both `role: "tool"`**, and the way to tell them apart is `tool`: the call names the tool, the result
does not.

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
| `busy` | 429 | a queue on this Mac is full — something is already in hand and will drain in seconds. On `/v1/voice`, one recording is being read and one is waiting. On `/v1/sessions/:id/info` and `/v1/places`, eight slow reads are already in hand — `/v1/sessions/:id/transcript` stands in that same queue but is never refused by this number. All of them drain on their own, and none is filed under an `Idempotency-Key` |
| `over_capacity` | 429 | the dispatcher's child slots are full — `orchestrator_max_children` from a root, `orchestrator_max_grandchildren` from a child — or the whole Mac's are. `retry_after` is seconds. (`rate_limited` covers the other orchestrator limit: dispatches per ten minutes) |
| `internal` | 500, 502 | a tab that would not open; a terminal that would not take the text |
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

: ping

event: sessions
id: 9
data: {"sessions":[…],"at":1787049611}

: ping
```

`hello` first, then the current state immediately, then a `sessions` frame every time anything
changes. The `data` of a `sessions` frame is byte-for-byte the payload of `GET /v1/sessions`.

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
