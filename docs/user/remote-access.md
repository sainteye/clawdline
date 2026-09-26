# Remote access: another browser, a phone, Clawdline Cloud

After this page you can open the console somewhere other than the machine's own browser — another
computer, or your phone — decide whether that device may only read or may also act, and take that
permission away again. Your agents and code stay on your machine whichever way you choose.

## Which way to choose

| Way | Account | Reaches | What it can do | State |
| --- | --- | --- | --- | --- |
| A. SSH port forward | None | Another computer you can SSH from | Read, or read and send | Works |
| B. Your own cloudflared tunnel | None | Any browser, including a phone | Read only | Built; the real Cloudflare leg has not been tested end to end |
| C. Clawdline Cloud | Clawdline account (GitHub sign-in) | Any browser, including a phone; several machines | Read, and act once you allow it | Preview |

The daemon itself listens on `127.0.0.1` only. None of these opens a port on your network.

The console's interface is in Traditional Chinese, the only language it ships so far. Labels below
are given as they appear, with their meaning in parentheses.

## A. Another computer, over SSH

On the computer you are sitting at, forward the port and ask the machine for a sign-in address:

```sh
ssh -L 7727:127.0.0.1:7727 you@your-machine
./bin/clawdline open --print          # in that SSH session; add --send to allow typing
```

Open the printed address in your local browser. It carries a key in its fragment; treat it like a
password until it has been used.

**Check:** the console loads with the machine's sessions. **Undo:** stop the SSH forward. Each
`clawdline open` creates a device of its own; there is no command yet to list or remove those
devices, so run it only for browsers you mean to keep.

## B. A phone, through your own cloudflared tunnel

You need [cloudflared](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/)
installed on the machine. The daemon starts and stops it itself, and only when all three hold:

1. **At least one device is already paired.** A browser signed in with `clawdline open` counts.
   Otherwise the tunnel would lead only to a locked door.
2. **Remote access is on**: **讓瀏覽器或你的手機看得到你的 session** (let a browser or your phone see
   your sessions).
3. **The tunnel is on**: **從任何地方連到這台機器** (connect to this machine from anywhere), set to
   **自動產生的網址** (an automatically generated `trycloudflare.com` address) or **我自己的網域**
   (a named tunnel on your own domain, with its **主機名稱**, hostname).

On a Mac, both switches are on the **遠端** (Remote) tab of the macOS app's settings window. Without
the app, set them in `config.json` in the state directory and restart `serve`:

```json
{ "remote": true, "remote_tunnel": "quick" }
```

Then:

```sh
./bin/clawdline tunnel                # state, mode and the public address
./bin/clawdline pair --watch          # leave running: shows a code when a device asks
```

1. Open the tunnel's address on your phone. The page says **這個瀏覽器還沒被放進來** (this browser
   has not been let in yet).
2. Name the device and press **要求配對** (ask to pair).
3. A six-digit code appears on the machine — in `pair --watch`, or as an alert in the macOS app —
   and never on the phone. Type it in and press **配對這台裝置** (pair this device) within two
   minutes.

**Check:** the phone shows **配對好了——這個瀏覽器進來了** (paired) and then the session list.

**Limits.** A device paired this way can **read only**; there is not yet a switch in the interface
that lets it send. Asking to pair is limited to three requests every ten minutes, and five wrong
codes end the attempt. **Undo:** set the tunnel to **關閉** (off), or `"remote_tunnel": "off"`.

## C. Clawdline Cloud

Clawdline Cloud is off by default. When on, the machine keeps an outbound connection to
app.clawdline.com that carries only end-to-end encrypted envelopes: the service passes them along
and cannot read your sessions. Cloud is also how you use **several machines** from one console.

Cloud is a preview. The machine side has been tested against a local copy of the service.

### Connect the machine

```sh
./bin/clawdline cloud login           # prints a code and an address; approve this machine there
./bin/clawdline cloud on              # turn the line on
# restart `serve`: the daemon reads the Cloud switch when it starts
./bin/clawdline cloud status          # the switch, the identity, the connection
```

Sign in at app.clawdline.com with **用 GitHub 繼續** (continue with GitHub). `cloud login` waits up
to ten minutes for you to approve the machine.

### Pair a browser or a phone

Any one of these:

- **From the machine:** `./bin/clawdline cloud pair` prints a one-time link. Open it in a browser
  signed in to the same account.
- **From the macOS app:** its settings window has **配對瀏覽器** (pair a browser) → **產生配對 QR**
  (generate a pairing QR). **On an iPhone or iPad, first add app.clawdline.com to the Home Screen
  (Safari → Share → Add to Home Screen), open it from there, sign in, and scan from inside it.** A
  pairing made in Safari does not carry over to the Home Screen app.
- **From the browser, for a machine you reach only over SSH:** in the hosted console, press
  **配對** (pair) on the machine's row. It shows one line to run on the machine,
  `clawdline cloud pair --offer <code>`, with the browser's fingerprint beside it.

Both screens show the same fingerprint when pairing finishes; compare them.

**Check:** app.clawdline.com lists the machine with its sessions.

### Let paired devices act

A paired device can read. To let it send text, answer, and start or close sessions on this
machine:

```sh
./bin/clawdline cloud commands on     # takes effect on the next request; `off` stops it at once
```

### See and remove devices

```sh
./bin/clawdline cloud devices
./bin/clawdline cloud revoke <device-id>
./bin/clawdline cloud off             # and restart `serve`: the line goes down
```

### Plans

Clawdline Cloud has a Free plan and a Pro plan ($10 a month); the **方案** (Plan) page of the
hosted console shows your plan and its limits. Everything on this machine without Cloud stays free
and needs no account.

## Troubleshooting

- **`clawdline tunnel` shows a reason instead of an address**: it names which of the three
  conditions is missing, or says cloudflared is not installed.
- **The Cloud line stays off**: `./bin/clawdline cloud status` says why. A broken Cloud setting
  never stops the daemon.
- **A phone can read but not act**: over Cloud, run `cloud commands on`. Over the tunnel, sending
  is not available yet.
- **Notifications on the phone**: [notifications.md](notifications.md).

## Deeper

- [getting-started.md §6](../getting-started.md#6-reach-it-from-a-phone) — the same Cloud steps in
  the longer guide.
- [remote.md](../remote.md) — the line between the free product and Cloud (in Chinese).
- [cloud-wire.md](../cloud-wire.md) — envelopes, keys, pairing and signed commands (in Chinese).
- [projects.md](projects.md) — bringing project settings to a second machine.
