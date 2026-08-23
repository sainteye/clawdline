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
| `GET` | `/v1/sessions/:id/links` | token | `read` |
| `GET` | `/v1/sessions/:id/skills` | token | `read` |
| `GET` | `/v1/projects` | token | `read` |
| `GET` | `/v1/places` | token | `read` |
| `GET` | `/v1/events` | token | `read` |
| `POST` | `/v1/places/:id/start` | token + key | `send` **and** the write switch |
| `POST` | `/v1/places/:id/start/:assistant` | token + key | `send` **and** the write switch |
| `POST` | `/v1/sessions/:id/send` | token + key | `send` **and** the write switch |
| `POST` | `/v1/sessions/:id/key` | token + key | `send` **and** the write switch |
| `POST` | `/v1/sessions/:id/focus` | token + key | `send` **and** the write switch |
| `POST` | `/v1/sessions/:id/end` | token + key | `send` **and** the write switch |
| `POST` | `/v1/auth/pair` | — | — |
| `POST` | `/v1/auth/pair/confirm` | — | — |
| `POST` | `/v1/auth/password` | — | — |
| `POST` | `/v1/auth/adopt` | a token in the body | — |
| `POST` | `/v1/auth/logout` | — | — |
| `GET` | `/`, `/index.html`, `/manifest.webmanifest` | — | — |
| `GET` | `/favicon.ico`, `/icon-<size>.png` | — | — |

Every approved device has `read`. `send` is granted to all of them together by
Settings → Remote → **Let paired devices type**, and taken back from all of them together — there is
no per-device grant. `admin` is defined, and the local `This Mac` device holds it, and **no route
requires it today**; it is there so that adding one later does not mean handing out a capability
nobody asked for.

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

`signature` is the transcript file's size and modification time joined by a dash. It is a cheap way
to ask *would fetching this again tell me anything new*, and nothing else — do not try to read
meaning into either half.

**A session with no transcript is not an error.** An empty `entries` and an empty `signature` come
back with `200`, because a session that has not spoken yet and a session that could not be found are
different things and only the second is a `404`. A shell that is not running an assistant answers
the same way, and so does a session whose record could not be matched to it.

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
  {"label":"ci","url":"https://github.com/you/repo/actions/runs/123","kind":"deploy","state":"ok","local":false},
  {"label":"web","url":"http://127.0.0.1:3000","kind":"server","state":"ok","local":true},
  {"label":"backlog","url":"file:///Users/you/code/repo/artifacts/backlog.html","kind":"artifact","state":"","local":true}
]}
```

| field | |
|---|---|
| `kind` | `site` · `deploy` · `server` · `artifact` — for choosing an icon |
| `state` | that thing's own health where it has one, else empty. **Worth drawing**: a server that is down is worth knowing before it is tapped |
| `local` | the address only resolves on the Mac's own network. A phone on mobile data cannot open it, and saying so beats a link that times out |

**A route rather than a field on the session.** The session list goes out on the event stream
whenever anything moves, and gathering these costs a `git` invocation plus a handful of file
reads per project — free when a menu is opened, a subprocess per session per second on the stream.

Nothing here is invented: the health endpoint comes from the icon registry, the run from the
deploy status, the servers from the project's own `status` command, the backlog page from
whatever produced it. An untrusted dev stack stays silent rather than being probed, and a
`file://` entry is handed over as a path so a client can decline it honestly.

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

### `GET /v1/places`

**Where a new session may be started**, which is a different list from `/v1/projects` and exists
for a different reason. `/v1/projects` is "directories somebody drew an icon for"; this is
"directories an assistant has actually been run in, and that are still there".

Both assistants' records go in and the list says nothing about which of them has been run where.
A directory is a directory: a folder you have only ever opened Claude Code in is a perfectly good
place to open Codex.

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
no arguments. If `claude --resume` is wanted one day it will be a second named action with its own
literal, not a field on this one.

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

Answers a menu with a single keystroke. `{"key":"1"}`…`{"key":"9"}`, or `{"key":"tab"}`. Anything
else is `400 bad_request`, and the allowlist is checked before anything goes looking for a terminal.

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

`502 internal` carries what actually failed; the session is left as it was found.

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
offer it. The whole field is **absent when the menu could not be read**, which is a real state and
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
| `rate_limited` | 429 | too many pairing attempts |
| `internal` | 500, 502 | a tab that would not open; a terminal that would not take the text |

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
