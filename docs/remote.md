# Reaching your sessions from somewhere else

Clawdline already works out what every session is doing — once, for the bar, the strip above the
transcript, the menu bar and the island. This serves that same reading over HTTP, so a browser on
the sofa, a phone in a hotel, or somebody's script can ask what the bar asks.

It costs something, and it does not cost the same thing twice.

**Reading a session hands over a repository name, a branch and a task title.** Through
`/v1/sessions/:id/transcript` it hands over the conversation: what you asked, what Claude
answered, which files it touched, what the tests said. On loopback that is nobody's business but
yours. On an address a stranger can reach it is an index of somebody's private work.

**Writing to a session is remote code execution.** That is not a dramatic way of putting it.
`POST /v1/sessions/:id/send` types a line into Claude Code and presses Return, and Claude Code
runs `bash`. Anything that can send to your sessions can run programs on this Mac, as you.

So these are not two positions on one dial. They are two features at two risk levels, and they are
**two separate switches** — both off in a fresh install, and turning the first on does not turn the
second on. Everything below follows from that one decision.

---

## Turning it on

Four steps, in this order. Each one is inert without the one before it, which is the design rather
than an ordering accident.

### 1. The server — Settings → Remote → **Answer over HTTP**

It binds `127.0.0.1` and nothing else. Not "binds everything and filters": the listener is created
with a required local endpoint of loopback, so there is no interface on your network for it to be
found on. A listener that accepts from the local network is one coffee shop away from being a
listener that accepts from the coffee shop.

`~/Library/Logs/Clawdline.log` says so when it comes up:

```
08-18 18:28:41.855  remote: listening on http://127.0.0.1:7717/
```

and from another terminal:

```console
$ curl -s http://127.0.0.1:7717/v1/health
{"ok":true,"version":"0.5.0","protocol":1,"write":false,"auth":false,"authed":false}
```

`auth: false` is this Mac saying nobody has paired anything yet. `write: false` is the second
switch. Both matter in a moment.

Starting the server also mints a token for **this machine** at
`~/.config/clawdline/remote-token`, mode `0600`, so that a script or a plugin running as you finds
a key already sitting there rather than a 401 it has to go and understand. That is the file
[`docs/api.md`](api.md) is about. It deliberately does **not** count as having set this up — see
step 3.

### 2. Pair a device — Settings → Remote → **Paired devices**

Nothing outside this Mac can read anything until you do. There is no grace period and no exception
for loopback: every route except the page itself, its icons, the manifest, `/v1/health` and the
pairing routes needs a token, wherever the request came from.

Two ways in:

- **Open in a browser** — mints a device of its own called `Browser on this Mac` and opens
  `http://127.0.0.1:7717/?t=<token>`. A query string rather than a fragment, and that is the whole
  point: **a fragment is the one part of a URL a browser never sends**, so on a cold open the
  server would have nothing to authenticate and the page would be refused before it could run any
  script. The server takes the token off the query, sets a cookie and answers `303` back to `/`,
  which takes it out of the address bar and out of history in the same move. The page also accepts
  `#t=` for the case where something already loaded hands it one.
- **From the other device** — the six-digit flow below.

### 3. A tunnel, if the device is not on this Mac — Settings → Remote → **Reachable from outside**

Optional, and refused until step 2 is done. See [the tunnel](#the-tunnel).

### 4. Writing, if you want it — Settings → Remote → **Let paired devices type**

Also optional, also refused until step 2 is done, and separate from everything above. Until this is
on, every `POST` answers `write_disabled` and says why:

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/sessions/$ID/send \
    -H "Authorization: Bearer $(cat ~/.config/clawdline/remote-token)" \
    -H 'Idempotency-Key: 8f0c1e2a-3b4d' -d '{"text":"run the tests\n"}'
{"error":{"message":"Sending is switched off. Settings → Remote turns it on, and it is off by default because typing into a session runs code on this Mac.","request_id":"595e3d72-1a89-42bd-ad99-f98a9bdcefbe","code":"write_disabled"}}
```

Turning it on grants `send` to **every** paired device at once, and turning it off takes it back
from all of them at once. Per-device grants would be the finer control and the worse one to have
as the only one: the moment somebody wants sending off they want it off everywhere, and walking a
list is how one gets missed.

The same switch covers **starting** a session, and a device that may start one may start either
assistant: *Start with* on the sheet says which, and it is offered only where this Mac has both.
What travels is a name out of a two-case list — `POST /v1/places/:id/start/codex` — resolved on the
Mac against its own copy of the directory. There is still no field anywhere on that route a
directory or a command could be written into, which is the property that mattered before there was
anything to choose between. [The route in full →](api.md#post-v1placesidstart-post-v1placesidstartassistant)

It also covers **picking a recorded conversation back up**, which is the same switch and one more
step of the same promise. The sheet's tick box turns the project list into the conversations Claude
Code has already written down in that project; the row that is pressed sends
`POST /v1/places/:id/resume/:session`, and the conversation is a path segment checked twice — once
for being a UUID, once for being one this Mac just listed for that directory. The listing itself is
read-level, because the titles it discloses belong to a directory whose name a reading token could
already see. [The routes in full →](api.md#get-v1placesidsessions)

**Dictating from a phone is behind this switch as well**, and not because transcribing writes
anything — it writes nothing at all. Two reasons, and neither is about the audio.

A device that may only read has **nowhere to put a sentence** once it has one. It cannot send, so
the best the feature could do for it is put words in a box that will not open. A microphone offered
on those terms is a microphone that lies about what pressing it achieves.

And transcribing **costs this Mac twelve seconds of every core it has**, on demand, from a device
that is not in the room. Read-level access is meant to be something you can hand out without
thinking hard about it: it copies a reading this app had already done for its own windows. This is
not that. It is the one read-shaped thing here that spends real money, so it rides with the switch
that already means *this device may cost me something*.

#### `POST /v1/voice`

Base64 of little-endian 16-bit mono PCM and the rate it was sampled at, behind the same
`Idempotency-Key` as every other write:

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/voice \
    -H "Authorization: Bearer $(cat ~/.config/clawdline/remote-token)" \
    -H 'Idempotency-Key: 3f9a1c04-77e2' \
    -d '{"audio":"'"$(base64 < clip.raw | tr -d '\n')"'","rate":16000}'
{"text":"change the retry to exponential backoff","ms":1640}
```

**`rate` is required, and the only value it may have is `16000`.** Checked rather than resampled:
16 kHz is what the recorder produces and what whisper wants, so a body naming 48 kHz has not made a
small mistake — it has sent something that would transcribe as a voice three times too fast, and
quietly correcting it would hide that from whoever wrote the client. The browser does the decoding
and the resampling before it uploads, which is also why nothing here needs `ffmpeg` to accept a
recording from a phone.

**An empty `text` with a `200` is an answer, not a failure.** Whisper comes back with nothing for
silence, for a recording of a room, and for a clip it decided was a rhythm rather than a voice. The
question the route was asked is *what was said*, and "nothing" answers it; a `4xx` there would have
a page apologising for a microphone that had worked perfectly.

| | when |
|---|---|
| `400 bad_request` | no `Idempotency-Key`; no `audio`, not base64, `rate` missing or not `16000`, under 0.25 s, over 300 s |
| `401 unauthorized` | no token, or one this Mac does not know |
| `403 write_disabled`, `403 forbidden` | the switch above; or `Host`, `Origin`, `Sec-Fetch-Site` |
| `429 busy` | two recordings are already in the queue, and the third is refused rather than made to wait |
| `503 no_whisper` | nothing here to transcribe with — `error.reason` is `no_binary` or `no_model` |

The 300 seconds is the far end this server accepts rather than the working limit; the page stops a
recording at three minutes on its own and transcribes what it has.

**The transcription runs on a queue of its own, and that is the design rather than an
optimisation.** Everything else here is read, decided and answered on one serial queue, which is
what makes the server's state safe to touch without a lock — and whisper takes 1.6 seconds warm and
about twelve after a reboot, so on that queue one dictation would hold every other request *and*
`/v1/events` for as long as it ran. The queue it goes to instead is serial too: two whispers at once
on one Mac are slower than two in a row, so the queue **is** the concurrency limit, and the only
thing left to choose was how long a line is worth standing in. Two.

The language is not the phone's to name. `voice_language` on this Mac decides, exactly as it does
for the bar, and what comes back is put through `voice_vocabulary` the same way — same binary, same
model, same words this project has taught it. [docs/whisper.md](whisper.md) is where all of that is
set, and the phone inherits it by not being asked.

**A `429` and a `503` are the two answers that are not filed under the idempotency key**, and
everything else is, refusals included. Those two are facts about this machine at this moment — the
queue drains, whisper gets installed — and an answer frozen for ten minutes would mean the retry
that was supposed to work is told *busy* by a cache long after the queue emptied. A repeated key
within those ten minutes otherwise hands back the same transcript rather than reading the same audio
twice.

### The same four things in `~/.config/clawdline/config.json`

The settings window writes this file and hand-editing it takes the same path, so the two cannot
drift apart.

| key | default | what it is |
|---|---|---|
| `remote` | `false` | the server, on `127.0.0.1` |
| `remote_port` | `7717` | |
| `remote_write` | `false` | the second switch — sending and starting sessions |
| `remote_tunnel` | `"off"` | `"off"`, `"quick"` or `"named"` |
| `remote_tunnel_name` | `""` | named tunnels only — the name you gave `cloudflared tunnel create` |
| `remote_hostname` | `""` | named tunnels only — the address you routed to it |
| `cloudflared_path` | `""` | where the binary is, if it is somewhere unusual |

**Anything unrecognised in `remote_tunnel` is `off`.** A typo in a config file must not be a config
file that opens a tunnel; the only way to get one is to have spelled it correctly.

---

## How a device is paired

The device asks. The Mac answers with an id and nothing else:

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/auth/pair \
    -H 'Content-Type: application/json' -d '{"name":"Phone on the sofa"}'
{"expires":1787049347,"pairing_id":"f306e75b-81f9-4376-b9ac-ee4fc7e11f08"}
```

A six-digit code appears **on the Mac's screen**, in an alert whose only button is *Ignore*, and it
is never in that response. That direction is the entire security property: anybody who can reach
the machine can start a pairing, and only somebody who can see its screen can finish one. If you
did not just ask for this, ignore it — whoever asked cannot get anywhere without the code.

The device sends the code back:

```console
$ curl -s -X POST http://127.0.0.1:7717/v1/auth/pair/confirm \
    -H 'Content-Type: application/json' \
    -d '{"pairing_id":"f306e75b-81f9-4376-b9ac-ee4fc7e11f08","code":"000000"}'
{"error":{"message":"That code is not right. 4 tries left.","code":"forbidden","request_id":"46e270b6-b9a7-48cc-a0fd-0ad2170ee51f"}}
```

Right, and it comes back with a 256-bit token and a cookie. Wrong, and there are four more tries
and then the pairing is gone — a million codes and five guesses is not a number anybody grinds
through, and the counter lives on the pending record rather than on a clock, so retrying from a
different connection does not reset it. The code lapses after **two minutes**: long enough to walk
to the Mac, short enough that one left on a screen is not a key somebody finds later.

Three more limits, all of them about the same thing. The pairing route has to be reachable without
a token, and it puts a modal alert on somebody's screen — left alone, that is a way to make a Mac
unusable from a shell script. So: **one pairing open at a time** (a new request replaces the old
rather than joining it, because two live codes is two chances to guess), **one alert on screen at a
time**, and **three requests in ten minutes**, after which the route answers `429 rate_limited`
until the window rolls.

A newly paired device may **read**. Sending is armed separately, by the switch, because it is a
different risk entirely.

---

## The tunnel

Your phone is not on loopback. A tunnel is how that gets crossed without opening a port:
`cloudflared` **dials out** to Cloudflare, and traffic comes back down the connection it made. So
nothing on this Mac is listening for the internet and no router has to be told anything. The
alternative was asking people to forward a port, which is a worse thing to ask for and a far worse
thing to get wrong.

**`cloudflared` is never bundled and never downloaded.** It is your own install, exactly like
`tmux` and `whisper-cli`: looked for at `cloudflared_path`, then in the places package managers put
it (`/opt/homebrew/bin`, `/usr/local/bin`, `/usr/bin`, `/opt/local/bin`), then on `PATH` — which is
last because it is almost never the answer, a GUI app being launched by launchd with the bare
system `PATH` and no Homebrew in it. Missing, and the settings window says so:

> cloudflared is not installed — `brew install cloudflared`, or put its path in
> `"cloudflared_path"`.

### It will not start until a device is paired

This is the interlock, and it is the reason the file exists. What is behind the tunnel is a list of
every repository, branch and task title on this machine. Behind an unauthenticated public hostname
that is a stranger's index of somebody's private work, reachable by anyone who guesses four English
words.

So the check lives in the thing that opens the door, not in the settings window — a settings window
can be walked around by editing `config.json`, and *"I set `remote_tunnel` to `quick` and it went
live"* should not be a sentence anybody is able to say. **One config key must never be the whole
distance between private and public.** Four refusals, in the tunnel's own words:

> Not opening a tunnel: this machine has no paired device yet, so anything that found the address
> could read your sessions. Pair a device first.

> The local server is off, so there is nothing to tunnel to. Turn remote access on first.

> A named tunnel needs `remote_tunnel_name`.

> A named tunnel needs `remote_hostname` — the address you point at it.

The token this Mac made for itself does not satisfy the first one. It is created automatically so
that things running as you keep working; if it counted, then merely switching the server on would
be enough to go public, which is exactly the accident the interlock is for.

### Quick — a generated address, no account

```
cloudflared tunnel --no-autoupdate --metrics 127.0.0.1:0 --grace-period 2s --url http://127.0.0.1:7717
```

That is the whole command, and it is the whole setup. Cloudflare invents a hostname for the run —
four words under `trycloudflare.com` — and Clawdline learns it the only way it can, by reading
cloudflared's own logging on stderr and waiting for a registered connection before publishing it.
It publishes on the registration rather than on the banner because the banner arrives about a
second early and says so itself: *it may take some time to be reachable*.

The address is new on every start and exists nowhere else — not in your config, not on a dashboard,
nowhere on Cloudflare's side you can look it up. So it is written to `~/Library/Logs/Clawdline.log`
**once**, because while the tunnel is up those four words are a password wearing a hostname's
clothes, and that log is the file people attach to bug reports. Reconnections get a line with the
random part taken out.

The three flags are not decoration:

- `--no-autoupdate` — a cloudflared from Cloudflare's `.pkg` replaces its own binary and restarts,
  which from this end is indistinguishable from the tunnel dying. Clawdline would back off and
  retry against something that was already coming back.
- `--metrics 127.0.0.1:0` — left alone, cloudflared takes the first free port from `20241…20245`
  **in order**. The Mac most likely to have cloudflared installed is the Mac already running it for
  something else, and taking 20241 out from under somebody's production tunnel to serve a page
  nothing here reads is not a trade worth making.
- `--grace-period 2s` — the default is **thirty seconds** spent waiting for in-flight requests on
  SIGTERM, and `/v1/events` is a stream that never finishes on purpose. The default turns every
  quit into half a minute of a process refusing to die.

### Named — your own domain

What Clawdline runs is only this:

```
cloudflared tunnel --no-autoupdate --metrics 127.0.0.1:0 --grace-period 2s run <remote_tunnel_name>
```

**Read what is not there.** No `--url`, and no `--config`. Clawdline does not tell cloudflared what
to serve or where its configuration is — it runs your tunnel by name and cloudflared reads its own
default config at `~/.cloudflared/config.yml`. Which means the four things below are yours to have
done first, and Clawdline never does any of them for you:

```console
$ cloudflared tunnel login                                  # once, per Cloudflare account
$ cloudflared tunnel create clawdline                        # writes ~/.cloudflared/<UUID>.json
$ cloudflared tunnel route dns clawdline clawd.example.com   # the DNS record
```

```yaml
# ~/.cloudflared/config.yml
tunnel: clawdline
credentials-file: /Users/you/.cloudflared/<UUID>.json
ingress:
  - hostname: clawd.example.com
    service: http://127.0.0.1:7717      # must match remote_port
  - service: http_status:404
```

Then `remote_tunnel_name: "clawdline"` and `remote_hostname: "clawd.example.com"`.

**`remote_hostname` is not cosmetic and cannot be worked out.** cloudflared never prints the
hostname of a named tunnel — the routing is in *your* config, and this end has no way to read it
back — so without it the tunnel comes up and can never be told to anybody. It is also what the
server checks incoming requests against, which is the next section. Give it the bare host; a
`https://` or `http://` you pasted along with it is taken off.

A tunnel that will not stay up is retried with a doubling backoff — 1, 2, 4 … capped at a minute —
six times, and then it gives up and says why. A child that ran for a minute before dying is a fresh
problem rather than the same one getting worse, so its counter starts over. The commonest failure
is a typo in the tunnel name, and cloudflared reports that one as a bare line on stderr with no log
level at all, which is why anything that is not a log line is passed through as a complaint:

```
error parsing tunnel ID: clawdline-no-such-tunnel is neither the ID nor the name of any of your tunnels
```

---

## Being told, instead of looking

A paired device can subscribe to notifications and then buzz when **a session starts waiting for an
answer** — the one state that costs you something for every second it goes unnoticed. Two more, both
off unless asked for: `push_on_finish` for a turn that ran over two minutes and stopped, and
`push_on_deploy` for a deploy that stopped running, whichever way it went.

<img src="assets/web-push.gif" width="390" alt="A notification arriving on a phone: the banner drops over the home screen carrying the app's own mark, sits long enough to be read, and slides away. This one is the test the page can ask for; the ones that arrive unasked name the session task and project that are waiting.">

The message is sealed to the device, so the push service carries ciphertext and learns only that
something went to a subscription. Encryption settles who may read it in transit and settles nothing
about who reads it off a locked phone lying face-up on a table, so what is inside is the project and
the state together with the session's task title. Prompt and transcript contents stay out of it.

**The Mac has to be running.** This is not a service somewhere; it is your machine, awake, noticing
and posting. Asleep or quit, nothing goes out, and nothing is saved up to go out later. And on iOS
the page has to have been added to the home screen and opened from there — Apple only delivers
notifications to a web app that lives there, which is a rule of theirs and not a setting here.

Revoking a device in Settings → Remote takes its notifications with it.

---

## What this defends against, and what it does not

### "It only listens on loopback" is not a boundary

Two reasons, and the second is the one that bites.

**Once a tunnel is running, every request arrives from `127.0.0.1`.** `cloudflared` connects to the
local port like any other program on the machine, so the socket address of a request from a phone
in another country is identical to the socket address of a `curl` in another terminal. Anything
built on *but it came from this machine* would be wrong in exactly the situation it was meant to
cover.

**A web page you merely visit can reach a local port.** JavaScript on `evil.com` can already
`fetch` `http://127.0.0.1:7717/…`. What normally saves a local server is that the page cannot
*read* the reply, because the origins differ — and **DNS rebinding removes that**: let `evil.com`
resolve to the attacker's address long enough for the page to load, then re-answer with
`127.0.0.1`, and the browser now believes the local server *is* `evil.com`. Same origin, no
protection left. The port number is not a secret either; it is `7717` in a public repository.

### What is actually in the way

- **`Host` validation, before anything else.** The one thing rebinding cannot change is the `Host`
  header, which still says `evil.com`. A request whose `Host` is not a name this server answers to
  is refused before it is looked at, and the whole attack is over:

  ```console
  $ curl -s -H 'Host: evil.com' -H "Authorization: Bearer $TOKEN" http://127.0.0.1:7717/v1/sessions
  {"error":{"message":"Wrong host.","request_id":"d49c4fd4-8e52-4451-89c0-fb06389c6979","code":"forbidden"}}
  ```

  The names it answers to are `127.0.0.1`, `localhost`, `::1`, whatever is in `remote_hostname`, and
  anything under `.trycloudflare.com`. That last one is a whole suffix because a quick tunnel's name
  is generated per run and cannot be in anybody's config — and it is safe for this attack
  specifically, since rebinding needs the attacker to control the DNS answer and `trycloudflare.com`
  answers are Cloudflare's.

- **`Sec-Fetch-Site`.** A modern browser saying, unforgeably, that the page asking is on a different
  site — a page's script reaching for this server behind your back:

  ```console
  $ curl -s -H 'Sec-Fetch-Site: cross-site' -H 'Sec-Fetch-Mode: cors' -H 'Sec-Fetch-Dest: empty' \
      -H "Authorization: Bearer $TOKEN" http://127.0.0.1:7717/v1/sessions
  {"error":{"request_id":"435148a5-7aea-4caa-ba11-9285ed0696a7","code":"forbidden","message":"Cross-site requests are not answered."}}
  ```

  **A cross-site request is not always an attack, and that distinction cost a bug.** Typing the
  address into a bar that happened to be showing another page is cross-site too — Chrome calls a
  navigation out of `chrome://newtab` exactly that — so the first version of this refused to open
  when you typed the URL in. What separates the two is not the site but the *mode*: a top-level
  navigation says `navigate` / `document` and a script's `fetch` cannot claim either, because the
  browser sets both headers. So navigations are let through and everything else cross-site is
  refused. Absent headers mean it is not a browser at all, and a script is left to the token.

- **A token on everything else.** Open without one: `/`, `/index.html`, `/manifest.webmanifest`,
  `/v1/health`, the pairing routes, and the icons. Each is on that list for a reason rather than for
  convenience — you cannot log in through a page you cannot load, you cannot pair with a machine you
  cannot ask, and a browser fetches `/favicon.ico` on its own before it has any idea who you are.
  The icons disclose nothing: it is the same drawing of the same creature for everybody, and it is
  in a public repository. Everything else is `401`. Tokens are 256 random bits, stored as a SHA-256
  and compared in constant time — including the lookup, so a wrong token cannot be used to find out
  which device ids are real.

- **An `Origin` check on top, for anything that mutates.** A cookie is sent by the browser whether
  or not the page asking wanted it to be, so a `POST` from a foreign origin is refused even with a
  valid cookie in it. Reads are exempt; they are already gated by the token.

- **A password, if you set one, goes through PBKDF2-HMAC-SHA256 at 600,000 iterations** — about
  98 ms on an M-series Mac. Device tokens are not stretched and that is not an inconsistency: there
  is nothing to guess in 256 random bits, so there is nothing to slow down. A password is a
  low-entropy secret chosen by a person, and guessing it *is* the attack. A correct password mints a
  device token rather than becoming one, so a password that leaks later cannot be replayed against a
  device already paired, and revoking a device does not mean changing it.

### What is not in the way

**Malware already running as you.** `~/.config/clawdline/remote-token` and
`~/.config/clawdline/remote.json` are mode `0600` — anything running as your user can read them,
exactly as it can read `~/.ssh/id_ed25519`. That is the same trust boundary a Unix socket would
give and no better one exists at this layer. A `0600` file is a real defence against a web page,
because a page cannot read files. It is not a defence against a process, and nothing here is. If
something is already running as you on this Mac, this feature is not your problem.

**Cloudflare sees your traffic in the clear.** A tunnel is HTTPS from the browser to Cloudflare's
edge, where the TLS is terminated, and Cloudflare then carries it to `cloudflared` on this machine.
Your transcripts pass through Cloudflare as plaintext at that point. That is how every Cloudflare
tunnel works and it is a reasonable thing to accept — but it is a choice, and it belongs here rather
than in a footnote. If it is not acceptable for what is on your screen, do not run a tunnel; a
paired browser on the same machine, or your own VPN, gives you the same interface with nobody in the
middle.

**The address of a quick tunnel is a secret.** Four English words and anyone who has them reaches
your login page. They still need a token to get past it — but treat the URL as a credential, because
for the length of that run it is one.

---

## When something looks wrong

Two files, and they answer different questions.

**`~/Library/Logs/Clawdline.log`** is *did it start, and is the tunnel up*:

```
08-18 17:27:40.875  remote: listening on http://127.0.0.1:7717/
08-18 17:35:12.857  remote: listener failed — The operation couldn’t be completed. (Network.NWError error 48 - Address already in use)
08-18 18:07:47.243  remote: POST /v1/places/3b9e26c1587facfd/start by 0f3c1d92-7a44-4c18-9b30-2e6f5a81c407 → 200
```

Error 48 is the common one and it means what it says: something else has `remote_port`. Change the
port and the whole thing comes back. Tunnels write here too — `tunnel: refused — …` for an
interlock, `tunnel: up at <url> — via <edge location>` when it registers, and
`tunnel: cloudflared exited (status 1) after 0s — try 2 of 6 in 2s` while it is failing.

**`~/.config/clawdline/remote-audit.jsonl`** is *what was done, and by whom*. One JSON object per
line, appended and never rewritten, mode `0600`. It is a security control rather than bookkeeping:
if somebody does get in, the question you will have is what they did, and that question has no
answer at all unless it was written down while it was happening.

```jsonl
{"at":1787046782,"device":"scanner","event":"pair.begin"}
{"at":1787047664,"device":"This Mac","event":"device.add","id":"0f3c1d92-7a44-4c18-9b30-2e6f5a81c407"}
{"at":1787047667,"cwd":"/Users/you/code/clawdline","event":"place.start","id":"B71E04A9-3D52-4F6B-A118-7C0946DE2B33","ok":"1","place":"3b9e26c1587facfd"}
```

Every line has `at` (Unix seconds) and `event`. The rest depends on the event:

| `event` | when | the fields that matter |
|---|---|---|
| `pair.begin` | a device asked to pair | `device` — the name it gave itself, and it chose it |
| `pair.done` | somebody typed the code at the Mac | `device`, `id` |
| `pair.locked` | five wrong codes; the pairing is gone | `device` |
| `device.add` | paired, or minted by a button | `device`, `id` |
| `device.revoke` | one device dropped | `device`, `id` |
| `device.revoke_all` | **Disconnect everything** | `count` |
| `device.caps` | the write switch moved | `id`, `caps` — e.g. `read+send` |
| `password.set` / `password.clear` / `password.fail` | | `device` on a failure |
| `session.send` | **text typed into a session** | `id`, `tty`, `chars`, `ok` |
| `place.start` | a session started from outside | `place`, `cwd`, `assistant`, `ok`, and `id` or `why` |
| `session.focus` | a session brought to the front | `id` |
| `voice.transcribe` | **a recording was read on this Mac** | `device`, `seconds`, `ms`, `chars`, `ok` |

`session.send` records the length of what was sent and not the text, and `ok` is `"1"` or `"0"` —
a send that failed is still a send that was attempted, and it is written down before the answer goes
back. The audit log is a record of access, and turning it into a copy of everything anybody ever
typed would make it the most sensitive file on the machine.

`voice.transcribe` keeps the same rule for the same reason: `seconds` is how long the recording was,
`chars` how long the transcript came out, and the words themselves are nowhere in the line. It is
also the one event here that records **what a device made this Mac spend** — `ms` is wall-clock time
in whisper — which is worth having when the question is why the fans came on.

Two things worth knowing before you read it. **`device` on a `pair.begin` is whatever the requester
called itself** — it is a label, not an identity, and only `pair.done` means anything happened.
And **a run of `device.revoke` immediately followed by `device.add` for "This Mac" is not an
intrusion**: it is the local token being re-minted because the file and the stored hash disagreed.

```console
$ jq -c 'select(.event|startswith("session."))' ~/.config/clawdline/remote-audit.jsonl | tail -20
$ jq -r '"\(.at|todate)  \(.event)  \(.device // .id // "")"' ~/.config/clawdline/remote-audit.jsonl
```

Nothing rotates this file and nothing trims it. It grows by a line per event, which for this
feature is a handful a day.

### The plan windows say unknown

The Session info card draws a Claude session's 5h and 7d windows from a file the status line
keeps, because Claude Code hands those two numbers to the status line on stdin and writes them
nowhere else — not the transcript, not a file of its own. The `?` beside *unknown* on the card
points here. It means one of three things, in the order they usually turn out to be:

**1. The status line is not [claude-bestiary](https://github.com/sainteye/claude-bestiary)'s.**
That is the one that writes the file. Install it:

```bash
git clone https://github.com/sainteye/claude-bestiary
cd claude-bestiary
./install.sh          # symlinks into ~/.claude, idempotent
./verify.sh
```

and put it in `~/.claude/settings.json`:

```json
{ "statusLine": { "type": "command",
                  "command": "bash ~/.claude/statusline-command.sh",
                  "refreshInterval": 2 } }
```

`refreshInterval` matters here as well as on the bar: the file is rewritten on that beat, so
without it the numbers only move when something else on the line does.

**2. No Claude Code session is open on the Mac.** The status line runs inside one, so the file
only moves while one is running. A window whose reset has passed is dropped rather than shown
stale, and the card goes back to *unknown* — which is the word for it, since nothing since then
has been seen. Open a session and the numbers are back within a couple of seconds.

**3. The file is somewhere else.** Clawdline reads `rate-limits.json` from the directory the
status line writes its other caches to: `~/.claude/statusline-cache/` unless `status_dir` in
`~/.config/clawdline/config.json` says otherwise. Check that the file is there and current:

```console
$ cat ~/.claude/statusline-cache/rate-limits.json
{
  "at": 1787537762,
  "rate_limits": {
    "five_hour": { "resets_at": 1787547000, "used_percentage": 15 },
    "seven_day": { "resets_at": 1787860800, "used_percentage": 8 }
  },
  "session_id": "beaa6f31-…"
}
```

A status line of your own can write that shape to that path and the card will read it the same
way: `at` in Unix seconds, and under `rate_limits` the `five_hour` and `seven_day` objects exactly
as Claude Code hands them over.

---

## Turning it off

**One control, for the moment you are in a hurry: Settings → Remote → Disconnect everything.** It
drops every paired device and any password, immediately, with no confirmation sheet — nothing about
that moment is improved by one, and re-pairing is cheap. Every token that existed stops working on
the next request.

Two things it does that are worth knowing before you press it.

**A tunnel that is already running stays running.** The address goes on resolving and everything
behind it answers `401`, because there is no longer a device that may ask. To close the tunnel as
well, set **Reachable from outside** back to Off.

**It also revokes the token this Mac made for itself**, so `remote-token` on disk stops working and
local scripts start getting `401`. A fresh one is minted the next time the server starts — switch
**Answer over HTTP** off and on again, and anything that had cached the old string will need to read
the file again.

The rest, from smallest to largest:

- **Let paired devices type** off — reading continues, every `POST` goes back to `write_disabled`,
  and `send` is taken back from every device at once.
- **Reachable from outside** off — `cloudflared` gets a SIGTERM, and a `SIGKILL` three seconds later
  if it ignored that. A tunnel is not a thing to leave running because a process was rude about
  closing. The app also kills it on the way out, so quitting Clawdline closes the tunnel; being
  force-killed with SIGKILL is the one case it cannot cover, because Darwin has no way to ask the
  kernel to do it for us.
- **Answer over HTTP** off — the listener goes away, open event streams are closed, and the tunnel
  comes down with it: with no server there is nothing to tunnel to, and the interlock refuses
  rather than leaving an address that works pointing at a port that does not.

Turning the server off leaves `~/.config/clawdline/remote.json`, `remote-token` and
`remote-audit.jsonl` where they are, so switching it back on finds the same paired devices. Delete
those files if you want none of it remembered.

---

Talking to it — every route, the JSON shapes, and a working `curl` for each — is
[`docs/api.md`](api.md).
