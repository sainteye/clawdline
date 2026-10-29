# Remote access: another browser, a phone, Clawdline Cloud

After this page you can open the console somewhere other than the machine's own browser — another
computer, or your phone — pair that device to read only or to read and operate, and revoke it when needed.
Your agents and code stay on your machine whichever way you choose.

## Which way to choose

| Way | Account | Reaches | What it can do | State |
| --- | --- | --- | --- | --- |
| A. SSH port forward | None | Another computer you can SSH from | Read and operate unless set to read only | Works |
| B. Tailscale Serve | A Tailscale account, signed in on both ends | Devices on your own tailnet, including a phone | Read only or read and operate, chosen per device after pairing | Built; verified over a real tailnet |
| C. Clawdline Cloud | Clawdline account (GitHub sign-in) | Any browser, including a phone; several machines | Read and act on paired machines while their Cloud commands are on | Preview |

The daemon itself listens on `127.0.0.1` only. None of these opens a port on your network.
A device paired directly with this machine is either **read only** or **read and operate**, chosen
per device on this machine (see [Read only or read and operate](#read-only-or-read-and-operate)).
Clawdline Cloud has a separate switch for the whole machine and a local grant for each pinned
browser (C below). Both must allow operation before that browser can act.

Clawdline no longer runs a Cloudflare tunnel of its own (removed on 2026-10-20; the automatically
generated `trycloudflare.com` address went the day before). A settings file that still holds the
old tunnel settings (`remote_tunnel`, `remote_tunnel_name`, `remote_hostname`, `cloudflared_path`)
starts nothing, and that tunnel's hostname is no longer an address the daemon answers. Use B or C instead.

Labels below follow the English interface; the console also supports other languages.

## A. Another computer, over SSH

On the computer you are sitting at, forward the port and ask the machine for a sign-in address:

```sh
ssh -L 7727:127.0.0.1:7727 you@your-machine
./bin/clawdline open --print          # in that SSH session; the browser can read and type
```

Open the printed address in your local browser. It carries a key in its fragment; treat it like a
password until it has been used.

**Check:** the console loads with the machine's sessions. **Undo:** stop the SSH forward, and
revoke the browser (see [Signed-in devices](#see-and-revoke-devices-signed-in-directly)): each
`clawdline open` creates a device of its own, and it stays signed in until it is revoked.

## B. A phone on your tailnet, through Tailscale Serve

Verified on 2026-10-20 over a real tailnet, with Serve's own certificate: the address opens the
pairing page, an unpaired client is refused Sessions, a paired client reads and sends, and revoking
it refuses its next request. The client was a Tailscale node, not yet a phone app.

[Tailscale Serve](https://tailscale.com/kb/1312/serve) gives this machine an HTTPS address that only
devices on your own tailnet can open, and passes each request to the daemon on `127.0.0.1:7727`.
Clawdline does not sign anybody in with their Tailscale identity and does not manage Tailscale
accounts or keys: a phone that reaches the address still pairs with a six-digit code.

1. **Install Tailscale and sign in on both ends**, this machine and the phone, with the same
   tailnet.
2. **In the Tailscale admin console, turn on MagicDNS and HTTPS certificates** (DNS page). Serve
   needs both to answer on `https://<machine>.<tailnet>.ts.net`.
3. **On this machine, start Serve in the background:**

   ```sh
   tailscale serve --bg 7727
   tailscale serve status        # prints the https://<machine>.<tailnet>.ts.net address
   ```

4. **Tell Clawdline that address.** Turn on **Let a browser or your phone view your sessions**, and
   enter the full name, without `https://`, in **Tailscale address** on the same tab. Without the
   app, set both in `config.json` in the state directory and restart `serve`:

   ```json
   { "remote": true, "tailscale_hostname": "my-machine.your-tailnet.ts.net" }
   ```

   The daemon answers that name only while both are set. Turning the switch off, or clearing the
   name, refuses the address on its next request even while Serve still runs.
5. **Pair the phone**: keep `./bin/clawdline pair --watch` running on this machine (or keep the
   macOS app open), open the address on the phone, name the device, press **Ask to pair**, and type
   the six-digit code that appears on this machine — in `pair --watch`, or as an alert in the macOS
   app, and never on the phone — within two minutes.

**Serve, not Funnel.** Serve is private to your tailnet. `tailscale funnel` publishes the same
address to the whole internet; it is not recommended, and nothing in Clawdline needs it.

**Check:** the phone shows **Paired — this browser is in** and then the session list.

**Limits.** Asking to pair is limited to three requests every ten minutes, and five wrong codes end
the attempt.

**Undo:** `tailscale serve --https=443 off` (or `tailscale serve reset`) stops Serve; clear
**Tailscale address**; revoke the phone (see
[Signed-in devices](#see-and-revoke-devices-signed-in-directly)).

### Read only or read and operate

Each device paired with a code, signed in with the password, or opened with `clawdline open` has
one of two grants:

- **Read only** — it sees Sessions, terminal screens, and the documents and images Clawdline shows.
  It cannot send, type in a terminal, or start or close anything. Notifications still reach it.
- **Read and operate** — it can also send messages, type in terminals, and start or close Sessions.
  An assistant can run programs on this machine because of what it sends.

Only this machine chooses. On the **Devices** page, under **Signed in to this machine**, each
device shows its grant and offers the other one; the page reads the list again before it shows
the change. From a terminal on the machine:

```sh
./bin/clawdline devices permission <device-id> read-only           # or read-and-operate
./bin/clawdline pair --permission read-only                        # choose before showing the code
./bin/clawdline devices pairing-permission <pairing-id> read-only  # a pairing still waiting for its code
```

A device paired before this choice existed, and a new pairing the machine did not set, stays
read and operate. A device cannot raise its own grant: the pairing request and its confirmation
ignore any permission they carry. Clawdline Cloud has its own machine-wide command switch (below),
which does not change these grants.

### See and revoke devices signed in directly

Browsers signed in with `clawdline open`, devices paired with a code, and devices signed in with the
password are listed in two places. Only this machine's own key can see the list or revoke from it:

- **The macOS app's window**, on the **Devices** page, under **Signed in to this machine**. Each device shows what it may do, when it signed in and when it was last used.
  **Revoke** asks first, naming the device, and only **Revoke it** acts. The line
  **This window uses the machine's own key** says the window itself uses the machine's own key: that key is
  never in the list and cannot be revoked there.
- **A terminal on the machine:**

  ```sh
  ./bin/clawdline devices                    # one line per device: id, capabilities, signed in, last used, name
  ./bin/clawdline devices revoke <device-id> # names the device and asks [y/N]; --yes skips the question
  ```

A browser signed in as a device of its own is not shown the list. Its Devices page says **This browser is signed in as its own device** and offers **Sign this browser out**, which asks first and then revokes that browser's own key.

**Check:** a revoked device's next request is refused and it is back at the pairing page.
Revoking does not take back what the device has already seen or downloaded.

These are not the browsers Clawdline Cloud relays for; those are `clawdline cloud devices` (C below).

## C. Clawdline Cloud

Clawdline Cloud is off by default. When on, the machine keeps an outbound connection to
app.clawdline.com that carries only end-to-end encrypted envelopes: the service passes them along
and cannot read your sessions. Cloud is also how you use **several machines** from one console.

Cloud is a preview. The app steps below need an app version that includes them; updating the website does not update the app.

### Connect the machine

On macOS, open Clawdline's **Cloud** tab and do the steps it still shows:

1. Choose **Continue with GitHub**, sign in in your default browser, and allow it to open Clawdline.
2. Choose **Connect this machine**, approve the native confirmation, approve the machine in your default browser with the same GitHub account, and return to Clawdline.
3. Choose **Pair this app**, then **Pair directly with this app's machine**. Approve, compare the fingerprints, and choose **They match — done**.

**Check:** this machine's Sessions appear in the Cloud tab. Signing in identifies you, connecting registers this machine, and pairing gives one app or browser its keys; every other browser pairs separately.

Without the app, run these on the target machine, in a terminal or over SSH:

```sh
clawdline cloud login     # open the printed address, sign in with the same account, approve within ten minutes
clawdline cloud on        # the running daemon connects; no restart
clawdline cloud status    # identity, switch and connection
```

If the shell says `command not found: clawdline`, open a new terminal, or run `~/.local/bin/clawdline` (standard installation).

### Pair a browser on the same computer

1. In the browser profile you will keep using, open [app.clawdline.com](https://app.clawdline.com/) and sign in to the same account.
2. Open **Pair**, choose **Show the pairing code** (no machine needs to be selected), then **Open Clawdline to pair**.
3. In the app's confirmation, check the machine and choose **Pair with this machine**.
4. Return to the same browser page, compare the fingerprints, and choose **They match — done**.

**Check:** the app says **Browser paired** and the browser shows the machine's Sessions. This shortcut only pairs the computer the browser runs on; otherwise use [a code or command](#pair-with-a-code-or-command).

### Pair an iPhone, iPad, or Android phone

1. **Prepare the phone first.** Keys stay where you pair, so pair where you will keep using Clawdline.
   - **iPhone or iPad:** in Safari, open app.clawdline.com, choose **Share → Add to Home Screen** (turn on **Open as Web App** if shown), open Clawdline from the Home Screen, and sign in there.
   - **Android:** choose a Chrome profile, or install Clawdline from Chrome's menu, and sign in there.
2. **Show the QR on the computer.** In the app's **Cloud** tab, find **Pair a browser or phone** (once Sessions appear: **Settings → Remote → Pair a browser or phone**). Choose **Show a pairing QR code** and approve. Click the QR to enlarge it.
3. **Scan inside the phone app:** **Pair → Scan the machine's QR**, then allow the camera. The system Camera app may open Safari instead.
4. Compare the fingerprints and choose **They match — done**.

**Check:** the phone shows the machine's Sessions and the computer says **Browser paired**.

- **QR expired** (about ten minutes): choose **Get a new QR code** on the computer.
- **Camera denied or black:** allow the camera in the phone's settings, or choose **Black screen? Choose a photo of the QR** or **Choose a QR image** in the same phone app. The image is read on the phone and does not move keys from another browser.
- **Still no scan:** use a code, below.

### Pair with a code or command

Use one of these when the app does not open, scanning fails, or the target machine has no app:

**Where pairing lives:** open **Settings → Remote** in either the local console or Cloud. In Cloud, **Pair this browser** opens the target-machine picker and the existing pairing flow. To pair another browser or phone, use **Open Clawdline setup guide** on the machine you want to connect; the macOS app's Cloud view also offers QR and pasted-code pairing here. An ordinary browser cannot approve a machine's pairing itself. Without the app, **Copy pairing command** copies `clawdline cloud pair`: run it on the target machine and open its one-time link on the device to pair. The **Sessions and access** tab contains workflow defaults and iTerm2 consent.

- **Paste a code:** in the browser or phone to pair, choose **Pair → Show the pairing code** (on a phone, **Can't scan? Use a pairing code instead**, or **Use a pairing code** after a camera error) and copy the whole code. On the computer, open the app's **Cloud** tab → **Settings → Remote → Pair a browser or phone**, paste it into **Paste the pairing code from the browser you want to connect**, choose **Pair this browser**, and approve.
- **Terminal:** on the browser's pairing card, choose **Copy pairing command** and run the whole command once on the target machine. It finds an installed Clawdline even when `clawdline` is not on PATH. If it says **Clawdline was not found**, install and start Clawdline there, then generate a new code.
- **QR without the app:** run `clawdline cloud pair` on the target machine. It prints a QR, a one-time link, the fingerprints and the expiry.

Keep the original browser page open, compare its fingerprints with the machine's, and choose **They match — done**; if they differ, reject the pairing. **New code** replaces an expired browser code. **wrong_account** means the two ends are signed in to different accounts. Each browser profile, private window and installed app pairs separately, and clearing its site data removes its keys.

### Let paired devices act

A paired browser can send text, answer, and start or close Sessions only when both its local
`operate` grant and this machine's Cloud command switch allow it:

```sh
./bin/clawdline cloud commands on     # takes effect on the next request; `off` stops it at once
```

Choose a browser's grant on the machine with `clawdline cloud grant <device-id> readonly` or
`operate`. Use `clawdline cloud pair --scope readonly` to pair a read-only browser. Omitting scope
when pairing again preserves its existing grant; a never-pinned browser keeps the legacy
`operate` default. A remote browser cannot promote itself. See [per-browser machine grants](../cloud.md#per-browser-machine-grants).

### See and remove devices

```sh
./bin/clawdline cloud devices
./bin/clawdline cloud revoke <device-id>
./bin/clawdline cloud off             # the running daemon disconnects; no restart
```

### Plans

Clawdline Cloud has a Free plan and a Pro plan ($10 a month); the **Plan** page of the
hosted console shows your plan and its limits. Everything on this machine without Cloud stays free
and needs no account.

## Troubleshooting

- **The Tailscale address answers "Wrong host."**: the name in **Tailscale address** does not
  match the one `tailscale serve status` prints, or remote access is off.
- **The old Cloudflare address no longer opens**: Clawdline stopped running that tunnel; use
  Tailscale Serve (B) or Clawdline Cloud (C).
- **The Cloud line stays off**: `./bin/clawdline cloud status` says why. A broken Cloud setting
  never stops the daemon.
- **A phone can read but not act**: check that it is paired with the selected machine and that
  `clawdline cloud commands` is on there and its local grant is `operate`. Over Tailscale Serve, check that its sign-in is still valid.
- **Notifications on the phone**: [notifications.md](notifications.md).

## Deeper

- [getting-started.md §6](../getting-started.md#6-reach-it-from-a-phone) — the same Cloud steps in
  the longer guide.
- [remote.md](../remote.md) — the line between the free product and Cloud (in Chinese).
- [cloud-wire.md](../cloud-wire.md) — envelopes, keys, pairing and signed commands (in Chinese).
- [projects.md](projects.md) — bringing project settings to a second machine.
