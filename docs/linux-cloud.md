# Putting a Linux machine on a Clawdline Cloud account

Measured on 2026-09-20 on a headless Ubuntu 24.04 server (amd64, 2 vCPU, 4 GiB, tmux 3.4, no
desktop) in `ap-east-2`, reached only over SSM, with no inbound port opened, no security group
touched and no new AWS resource created; §3.3 continues it on 2026-09-21, after the account
approval that §2 was still waiting for. `docs/linux.md` is the same class of machine and says what
a *local* Linux daemon does; this file is the next question — **what it takes for a phone to reach
that machine through `app.clawdline.com`, and what is still missing.**

**In one sentence, as of 2026-09-21: the machine is joined, connected and publishing, and a phone
still cannot open a session on it — because no browser has paired with *this machine*, and the
console's chooser draws an unpaired machine as a row that cannot be pressed, so nothing is ever
sent (§3.3).** Past that, one square on the phone's path is missing by name, `dispatch`, and a
one-shot schedule is the door that is open in its place.

**What §2 says about the join is kept as it was measured on 2026-09-20**, when the one-time code
had expired unapproved; it has been approved since.

## 1. Getting this build onto the machine

Cross-compile and build the console on a workstation. **Do not set `VITE_HOSTED_CONSOLE`**: that
variable selects the hosted build, which loads `CloudGate`, refuses every origin but
`app.clawdline.com`, and is useless to a daemon serving its own console (`docs/hosted-console.md`;
`tools/package-macos.sh` refuses a bundle that contains `CloudGate` for this reason).

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o clawdline ./cmd/clawdline   # 26.9 MB, 1.6 s
( cd web && npm install && npm run build )                                     # web/console/dist
grep -rql CloudGate web/console/dist/assets && echo "hosted build — wrong one"  # must print nothing
tar -czf payload.tgz clawdline -C web/console dist                              # 15.1 MB
```

### Shipping it over SSM without opening anything

SSM Session Manager forwards a port *into* the instance, and the agent dials the instance's own
loopback, so a listener bound to `127.0.0.1` there is reachable from the workstation and from
nowhere else. That is enough to push 15 MB in 18 seconds with no S3 bucket, no inbound rule and no
SSH key:

```sh
# on the instance, through AWS-RunShellScript: a one-shot receiver on loopback only
python3 - <<'PY' &
import socket
s = socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind(("127.0.0.1", 9911)); s.listen(1); s.settimeout(600)
c, _ = s.accept()
with open("/var/tmp/payload.tgz", "wb") as f:
    while (b := c.recv(1 << 20)): f.write(b)
PY

# on the workstation
aws ssm start-session --target <instance> --document-name AWS-StartPortForwardingSession \
  --parameters '{"portNumber":["9911"],"localPortNumber":["19911"]}' &
cat payload.tgz | nc 127.0.0.1 19911
```

The SHA-256 on both ends matched. Nothing else about the machine changed.

### Running it as a service account

```sh
tar -xzf /var/tmp/payload.tgz -C /opt/clawdline-next
systemd-run --unit=clawdline-next --uid=<service user> --gid=<service user> \
  --setenv=HOME=<service home> \
  --setenv=CLAWDLINE_NEXT_WEB=/opt/clawdline-next/dist \
  --setenv=CLAWDLINE_NEXT_STANDALONE=1 --setenv=CLAWDLINE_NEXT_OWN_SESSIONS=1 \
  /opt/clawdline-next/clawdline serve
```

`doctor` then prints `port 7727`, `upstream none`, the state directory and an empty store, and
`GET /v1/health` answers `{"authed":false,"ok":true,"password":false,"served_by":"clawdline-go"}`.
The two `CLAWDLINE_NEXT_*` switches are still read and still change nothing, exactly as
`docs/linux.md` §1 says.

**One trap, and it is not in `docs/linux.md`: a service account's shell is usually
`/usr/sbin/nologin`, and tmux runs the shell from `/etc/passwd`.** `tmux new-session -d` under such
an account starts a server, the shell exits immediately, the server exits with it, and every later
command answers `no server running on /tmp/tmux-<uid>/default` — which reads like "tmux is broken"
and is not. `SHELL=/bin/bash` in the environment, or an explicit command on the `new-session` line,
is the whole fix. The daemon itself is unaffected; only a session started by hand is.

## 2. What joining an account actually is

Two separate handshakes, and they are separate on purpose. Neither can be done by this repository's
code alone, and that is the design (`docs/remote.md` principle 3: this machine never reaches a
person's account without the person saying so).

| | What it is | Who does it | Where the code is |
|---|---|---|---|
| **A. Enrolment** | RFC 8628 device authorization: the machine declares a name, a platform and a fresh Ed25519 public key, gets back a short user code, and a person approves that code in a browser signed in to the account | the person, in a browser | `internal/adapters/cloud/account.go` (`StartLogin`, `WaitForApproval`), `cmd/clawdline/cloud.go` (`cloudLoginCommand`) |
| **B. Pairing** | the E2EE handover: the machine draws a 32-byte secret, tells the control plane only its SHA-256, shows a link whose fragment carries the secret, and seals this account's content key for the browser that answers | the person, on the phone | `internal/transport/cloud/pairing.go`, `internal/adapters/cloud/pairing.go` |

A headless machine has no native shell to show either of them, so both come out of the CLI, which
is what `docs/remote.md` promised for platforms without one.

### Measured, in order

```
$ clawdline cloud preflight
ok    settings   readable
ok    endpoints  production: https://api.clawdline.com, wss://relay.clawdline.com/v1/connect, https://app.clawdline.com
ok    switch     cloud_enabled is off; it is turned on after sign-in (step 3)
ok    commands   cloud_commands is off: a paired browser may read and may not act
ok    keys       this app's own directory, <state dir>/cloud (the Swift app's ~/.config/clawdline is refused by construction)
ok    identity   none yet; `clawdline cloud login` creates it
ok    device key none yet; sign-in creates it under this app's directory
next       the person's step: `clawdline cloud login`, then approve the code in the browser
network    nothing in this check was sent anywhere

$ clawdline cloud login --wait 55m --name <machine name>
api        https://api.clawdline.com
code       XXXX-XXXX
approve at https://clawdline.com/connect?user_code=XXXX-XXXX
waiting    up to 55m0s
```

That is as far as a machine may go by itself, and it is where this work stops.

**The key is this machine's own.** `LoadOrCreateDeviceKey` mints a fresh Ed25519 pair under this
app's directory; `cloudkeys.Open` refuses `~/.config/clawdline` by construction; nothing was copied
from any other machine. Two producers under one sender id would share one sequence space and each
would make the other's envelopes look like replays.

### The code's life is about ten minutes, and nothing prints that

Measured: a code issued at 12:45:20 UTC was refused at 12:54:58 UTC with

```
clawdline: device_not_approved (login_expired): the one-time code expired before it was approved
what to do Run `clawdline cloud login` again and finish the approval page.
```

`--wait 55m` does not extend it — the control plane's TTL is authoritative and the flag is only the
ceiling on a machine whose clock disagrees. `LoginStart` carries `expires_in` and
`cloudLoginCommand` prints `code`, `approve at` and `waiting <the flag>` and **not** that field, so
the one number that decides whether a person has time to walk to another device is the one number
missing from the screen. On a desktop that gap is invisible; on a machine whose operator is
somewhere else entirely, every relay of the code has to beat a clock nobody can see.

## 3. The whole path from a phone, square by square

Stated as the path is walked: account, line, pairing, permission, then the act.

### 3.1 Built and measured working

| Square | Evidence |
|---|---|
| The daemon runs headless and serves its own console | `serve` under a transient unit; `/v1/health` `200` |
| The account's endpoints are production and unmixed | `cloud preflight`, all `ok` |
| Enrolment reaches the code | `cloud login` printed a code and waited |
| The line opens by itself once enrolled | `startCloudLine` in `cmd/clawdline/main.go` runs at every `serve`, off unless `cloud_enabled` |
| The machine publishes sessions, an inventory and its dispatched work | `internal/transport/cloud/publish.go` sends `s/<machine>/<id>`, `s/<machine>/<inventory>` and `orch/<machine>`; `tasklist.go` is the `tasks` array the hosted console groups children by |
| The hosted console has a machine chooser | `web/console/src/cloud/CloudGate.tsx` — a `machines` screen, `choose()`, `machine.selectable`, `machine_pairing_required` |
| A session exists on the machine to receive work | a Codex session listed as `%0`, `backend: tmux`, right `cwd` |
| A viewer can start a session | `POST /v1/places/<place>/start/codex` → `{"ok":true,"id":"%1","backend":"tmux"}`, a second pane appeared — **from this machine's own local device credential, on the machine**; see §3.3 for what that does not prove |
| A viewer can type into one | `POST /v1/sessions/%1/send` → `{"action":"typed"}`; Codex answered; **the Enter landed** — `docs/linux.md` §4.2's second defect did not reproduce on this build |
| A viewer can press a waiting card | `POST /v1/sessions/%0/key {"key":"1"}` answered Codex's trust dialog |
| The broker dispatches and collects a Codex child on this machine | task `state: success`, the file it was asked to write contained what it was asked to write, `result.json` collected |

### 3.2 Missing, each with where it stops

**1. `dispatch` is refused by name, and that is the headline.**
`internal/app/cloudops/ops.go` registers `op{name: "dispatch", refusal: &cloudDispatchUnpinned}` —
`409 cloud_dispatch_unpinned`, *"Cloud dispatch has no pinned wire shape on this Mac."* The console
does not even send it: `dispatch` is in `NO_MAC_ROUTE` in `web/console/src/cloud/carry.ts`, whose
sentence is *"Dispatching a task over Clawdline Cloud has no pinned wire shape on this Mac: dispatch
it on the Mac."* It is a **refusal, not silence**, at both ends.

Why it is refused is one fact, and it is worth stating precisely rather than as "not implemented".
`POST /v1/orchestrator/tasks` is not a create: it takes `task_id`, `secret` and
`inventory_generation`, and reads the brief **from a file the caller already wrote** at
`<state dir>/tasks/<id>/task.json` (`internal/app/orchestrator/draft.go`: *"the brief arrives as a
file, not as a request body"*). Measured here, in this order:

```
POST /v1/orchestrator/tasks  {"kind":…,"instructions":…}   → 400 "task_id and secret are required."
POST /v1/orchestrator/tasks  {"task_id":…,"secret":…}      → 422 bad_task "No readable task.json under <state dir>/tasks/<id>/."
```

A phone has no filesystem on the machine, so there is no body it can send that satisfies that
route. What is missing is not code but a decision: **who writes that file, and where**.

**2. The door that is open instead: a schedule.** `DispatchScheduled`
(`internal/app/orchestrator/scheduled.go`) writes `task.json` *itself* from a stored template,
mints the secret and admits the run through the same `Dispatch` — the same arbitration, `CHILD.md`,
timeout and collection. `schedule-create` and `schedule-run` are already in the Cloud vocabulary
and already carried by the console (`CARRIED` in `carry.ts`). Measured end to end on this machine:

```
POST /v1/orchestrator/schedules  {title, on, at, place_id, assistant, instructions, timeout_minutes}
  → {"ok":true,"schedule":{"id":…},"dispatch_enabled":true}
POST /v1/orchestrator/schedules/<id>/run
  → {"ok":true,"task_id":…,"task_dir":…}
  → state success; the child wrote what it was told to write
```

So the capability exists one door over. Three differences, and they are why this is a finding and
not a fix: a scheduled run has **no root** (`root.session_id: nil`), so there is no completion
notice into a root session, no grouping and no close cascade; the schedule is a stored object that
outlives the run unless something removes it; and the console's schedule form is a schedule form,
not a dispatch form — nothing labels this path as "send work to that machine".

**3. A paired viewer may read and may not act, until somebody says otherwise.**
`cloud_commands` is off by default and is read per request. Without
`clawdline cloud commands on` on the machine, `send`, `start`, `key` and the schedule writes are
refused at the gate (`writePolicy`: *"This device may read, and not send."*). This is one more
thing the person has to do on the machine, and it is deliberate.

**4. The free plan registers one machine.** `docs/cloud-cutover.md` §2 says the approval page fails
with `free allows 1 machine(s)` / `machine_limit_reached` when the plan's slot is taken, and that
this is the plan and not a fault. **Untested here** — the code in §2 expired before it was
approved, so this machine has never been presented to the entitlement check. If the slot is taken,
the approval fails and nothing on the machine can tell: the terminal only ever sees
`login_expired`, because the machine cannot read the approval page's reason. Both screens have to
be watched at once, which is hard when they are in two places.

**5. There is no way to remove a machine from the account afterwards.**
`docs/cloud-cutover.md` step 2's rollback: *"帳號那一列目前沒有按鈕可刪（hosted console 沒有 machine
撤銷介面）"*. The daemon has `RotateMachineKey` and `Machines`, and no revoke. On a free plan with
one slot, an enrolment that goes to the wrong machine is not reversible from the console.

**6. Pairing is a second thing to carry, and its link is not a code.** After enrolment the phone
still holds no content key. `clawdline cloud pair` prints a URL whose **fragment carries a 32-byte
secret**; that URL has to reach the phone's browser intact. Refused correctly before enrolment:
`409 cloud_not_signed_in`, *"This Mac is not connected to a Clawdline Cloud account."* A code a
person can read aloud is a different object from a URL with a secret in it, and a headless machine
produces the second.

**7. `GET /v1/cloud/devices` answers `501`** — "this daemon does not own that route yet" — although
`clawdline cloud devices` works, so the CLI and the HTTP surface disagree about a route that a
settings page would need.

**8. Three routes a Cloud read maps to answer `501` on a stock Linux build**: `/v1/schedules`,
`/v1/past-sessions` and `/v1/skills`. Two of those are harmless — nothing calls them: the console
and the Cloud bridge both use `/v1/orchestrator/schedules` and `/v1/places/<id>/sessions`, which
answer `200`. `docs/linux.md` §4.3 reads as though the schedules feature were unreachable on Linux;
measured here, the route the console actually calls works, creates and runs. `skills` is genuinely
absent, and is already declared absent over Cloud (`NO_MAC_ROUTE`).

**9. A Codex child still cannot reach `127.0.0.1` from inside its own sandbox.** Both children
dispatched here reported `accepted` and `inflight` failing to connect and fell back to
`accepted.json`, which the broker collected — the design working as written (`docs/linux.md` §4.4),
and still a round trip that costs a child two failures before it finds the path that works.

**10. Nothing on Linux installs a login item.** The daemon here runs under a **transient**
`systemd-run` unit: it does not survive a reboot. `clawdline autostart` does not exist; a
`systemd --user` unit plus `loginctl enable-linger`, or an ordinary system unit, is still the manual
answer.

### 3.3 Where the path actually stops, measured 2026-09-21

`docs/linux-cloud.md` was written the day before this machine was approved. It has been approved
since: the line is up, the switch and the command switch are both on, and the machine publishes.
The question this section answers is the next one — **a person on a phone, at the hosted console,
tries to open a session on this machine and nothing arrives here.** Every fact below was read on
the machine, read-only, over SSM.

**The machine is ready, and by its own count no viewer has ever asked it for anything.**

| Read | Answer |
|---|---|
| `GET /v1/cloud/status` | `enabled: true`, `commands: true`, `state: "connected"`, `roster_readable: true`, `pinned_readable: true` |
| its `commandset` | 27 words, `start` among them |
| `GET /v1/places` | three places, both assistants installed |
| `GET /v1/sessions` | three live `tmux` sessions |
| `inbound_total` / `answered` / `refused` | `0` / `0` / `0` |
| `place.start` in the whole daemon log | one, and its caller is this machine's own local device |
| `cloud ack … fanout=` | `0` 519 times, `1` 92 times, `2` 142 times |

So a viewer **is** connected and **is** receiving this machine's envelopes — `fanout` is not always
zero — and that viewer has never sent this machine a command. It is not a dropped request and not a
silent refusal: a bridge refusal would have moved `refused`, and a queue refusal logs its own line.
Nothing was ever addressed to this machine.

**Why, in one sentence: the browser holds no pairing for this machine, so it cannot open what this
machine publishes, so the console's machine chooser draws it as a row that cannot be pressed.**

The evidence that no browser is paired *with this machine* is three-fold and agrees with itself:
there is no `paired-devices-v1.json` in the key directory; every device the account roster lists
answers `pinned: false`; and the daemon log holds no `cloud: pairing invitation` and no
`cloud: paired viewer` line at all. The account roster is a different thing and it is healthy —
the devices on it carry `send_prompt`, which is what `link.go`'s `WriteGateAllows` asks for.

The browser half is decided in the copied client, which is the code app.clawdline.com runs:

- an envelope from a machine this browser holds no pairing for, from a sender it holds no key for,
  is recorded by `_noteUnpairedMachine` rather than treated as a decrypt failure
  (`cloud-client.js`, `_openEnvelopeFrame`);
- `_machineRows()` then reports that machine `pairing: "not_paired"`, and
  `selectable: pairing !== "not_paired"` — so `selectable: false`;
- `CloudGate.choose()` opens `if (!current || reader.current || !machine.selectable) return`, and
  the row is drawn `disabled`, with `webDeviceNotPaired` under its name;
- and a command addressed to such a machine never leaves: `_outboundMachinePairing` throws
  `machine_pairing_required` before anything is sealed.

Driven against the copied client itself, with a machine put in each state:

```
{"id":"machine-paired",  "pairing":"paired",     "selectable":true}
{"id":"machine-unpaired","pairing":"not_paired", "selectable":false}
CloudGate would open machine-paired : true
CloudGate would open machine-unpaired : false
outbound refused: machine_pairing_required | This browser is not paired with the selected machine.…
```

A machine whose `orch/` snapshot cannot be opened also has no descriptor to be named by, so the row
carries the generic `webStartMachine · <short id>` label rather than the name the machine publishes.
On a phone that is a row with an unfamiliar name, greyed out, that says it is not paired.

**What is missing is not a route and not a screen — it is step 3 of §4, which nobody has done for
this machine.** `clawdline cloud pair` exists on this build (`clawdline cloud` lists it) and prints
a one-time link whose fragment carries the secret; that link has to reach the browser. Until it
does, the hosted console cannot be pointed at this machine, and so
`POST /v1/places/<place>/start/<assistant>` is never sent.

**Two things the console could say better, and neither is the cause.**

1. The gate's machine list names the state and offers no next step: a disabled row and
   `webDeviceNotPaired`. The Devices page does carry the sentence — `webDevicePairHelp`, *"Start
   Pair a Browser on this machine to let this browser read its Sessions."* — but that names a
   control this platform does not have; here the pairing link comes out of the CLI.
2. `deviceViewModel` puts a **New session** button on a Devices card when `canStart`, and
   `devices-bridge.ts` hands the press to a callback that ignores the machine id and returns to the
   session list of whichever machine the gate already chose. It is not drawn for an unpaired
   machine, so it is not on this path — but on a paired second machine it would be an affordance
   that does not do what it says.

Neither swallows a refusal. `web/console/src/refusals/scan.ts` reports `0 unanswered` over 119
files and 29 refusal ladders, and it is right to: a row nobody can press produces no refusal for a
guard to find. That is the shape worth remembering — **the silence here is upstream of every
refusal path, which is exactly why every refusal path is clean.**

## 4. What the person has to do

In order, and only the person can do 1 and 3:

1. **Approve the code.** Open the `approve at` URL the machine printed, check the code matches, sign
   in. The code lasts about ten minutes; if it has expired, the machine has to print a new one
   (`clawdline cloud login`) — see §2.
2. On the machine: `clawdline cloud on`, then restart the daemon. The switch is read at start.
3. **Open the pairing link on the phone** (`clawdline cloud pair` prints it) and check the two
   fingerprints match.
4. On the machine, if the phone is to act and not only watch: `clawdline cloud commands on`.

It is not one button. It is two approvals and two switches, and the two switches are the machine's
own — the shape `docs/cloud-cutover.md` already describes, with no desktop to show either half.

## 5. What it costs

Prices read from the AWS Pricing API on 2026-09-20 for `ap-east-2` (Asia Pacific, Taipei), Linux,
shared tenancy:

| Item | Rate | Per day | Per 30 days |
|---|---|---|---|
| `t3.medium`, on demand, running | $0.0490 /hr | $1.176 | $35.28 |
| gp3 storage, 52 GB attached | $0.0864 /GB-month | $0.150 | $4.49 |
| one in-use public IPv4 address | $0.0050 /hr | $0.120 | $3.60 |
| SSM Session Manager, including the port forward that carried 15 MB | no charge | — | — |
| **Total, left running** | **$0.0604 /hr** | **$1.45** | **$43.37** |

Stopping the instance removes the compute and the address and leaves the storage: **$4.49 per 30
days** to keep it stopped. Cost Explorer for the whole region reports `$3.2832` of EC2 compute and
`$0.3803` of EC2-Other per day, unchanged across 2026-09-17, 09-18 and 09-19 — that is the region,
not this machine; resource-level granularity is an opt-in the payer account has not turned on, so
this machine's own line cannot be read back from the bill and the table above is the rate card
times the hours.

## 6. Not run

**On 2026-09-20**, when §1–§5 were measured: the approval itself, and therefore everything behind it —
the device token, the relay handshake, the first publish, the machine appearing in the phone's
chooser, the pairing handover, and whether the plan has a slot. Reboot survival. The hosted console
against this machine. Claude Code — it is installed on this machine and **not signed in**
(`Not logged in · Please run /login`), so Codex was the only assistant available and every dispatch
measurement here is a Codex one.

**Since (§3.3, 2026-09-21):** the approval, the device token, the relay handshake and the publishes
have all happened and are measured. This machine got a slot. Still not run, and each for the same
reason — nobody has opened a pairing link for this machine on a browser: the pairing handover, the
machine becoming selectable in the chooser, and any Cloud command reaching this machine at all,
`start` included. Reboot survival is still not run. Claude Code is now installed *and* offered by
`GET /v1/places`, which reports availability as `unknown` and does not read whether it is signed
in, so that row says nothing about whether a Claude session would open.
