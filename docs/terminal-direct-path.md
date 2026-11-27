# The terminal's direct path

A terminal opened from app.clawdline.com used to travel browser → Cloud relay → machine for every
keystroke and every screen. This page says why that was slow, what four comparable projects do,
and how the direct path works. The wire contract itself is in `docs/cloud-terminal-wire.md`.

## Why the relay path was slow (measured 2026-10-05)

From a machine in Taiwan:

```sh
curl -s https://relay.clawdline.com/cdn-cgi/trace | grep -E 'colo|loc'
# colo=SJC  loc=TW
for i in 1 2 3 4 5; do curl -s -o /dev/null -w '%{time_connect}\n' https://relay.clawdline.com/v1/health; done
# 0.143893 0.143560 0.141779 0.142696 0.144072
```

Cloudflare served the relay from San Jose, so one round trip to the relay edge was about 143 ms. A
phone and a machine both in Taiwan therefore crossed the Pacific four times per keystroke → echo
(phone → SJC → machine → SJC → phone): about 290 ms of network before tmux, capture or crypto.

The relay also paced continuous output. The machine keeps one frame in flight and sends the next
only after the relay acknowledges the previous one as delivered (`sendTerminalFrame`,
`internal/transport/cloud/terminal.go`; the relay acks after fan-out). With a ~150 ms round trip
that is at most about six frames a second, whatever `CaptureGap` (33 ms) allows.

Neither is a tuning problem: the colo is Cloudflare's routing for this network, and the one-frame
window is what keeps the relay's cache and fan-out bounded.

## What comparable projects do (read in source, 2026-10-05)

None of the four uses WebRTC, iroh, libp2p or tsnet.

| Project | How the remote client reaches the machine | What is streamed |
|---|---|---|
| [Collie](https://github.com/AltanS/collie/blob/main/ARCHITECTURE.md) (phone PWA for herdr) | Directly over a tailnet (`tailscale serve`), or the person's own reverse proxy; no relay of its own | HTTP polling of a `capture-pane` snapshot, 12 s when idle |
| [T3 Code](https://github.com/pingdotgg/t3code/blob/main/docs/internals/remote.md) | An ordered list of routes — LAN, Tailscale, SSH port-forward, and [T3 Connect](https://github.com/pingdotgg/t3code/blob/main/docs/internals/t3-connect.md) (a per-machine Cloudflare tunnel); it connects on the first that works and upgrades to a better one in the background | Raw PTY output plus one history snapshot |
| [herdr](https://github.com/ogulcancelik/herdr/blob/master/docs/next/website/src/content/docs/connecting-machines.mdx) | Plain OpenSSH with ControlMaster; no cloud | A server-side parsed screen, sent as diffed ANSI or revisioned surface patches |
| [Orca](https://github.com/stablyai/orca/blob/main/cloud/README.md) | The phone app tries a direct LAN or Tailscale endpoint first, else its own regional relay (cells chosen by measured latency); end-to-end encrypted either way | Raw PTY bytes in binary frames |

Each of them gets a direct path because its client is a native app, a desktop app, or a page the
machine serves itself. Clawdline's client is the hosted page `https://app.clawdline.com`, and a
browser does not let an HTTPS page open `ws://192.168.x.x` or `ws://100.x.y.z` (mixed content, and
Chrome's local-network restrictions). The browser-native way for that page to reach the machine
without a certificate on the machine is a WebRTC data channel. Its host candidates include the
machine's Tailscale address, so a phone on the same tailnet gets a direct path even behind a
carrier's NAT.

Copied: T3's "connect on what works, upgrade in the background", and Orca's "direct first, relay as
fallback, end-to-end encrypted either way". Not copied, for now: raw-PTY streaming (it replaces the
snapshot and delta model end to end) and regional relay cells (infrastructure that a working direct
path makes unnecessary). No project does mosh-style predictive echo.

## How the direct path works

The terminal still opens on the relay, exactly as before. Once it is showing, the page tries to
upgrade it:

1. **Offer.** The browser makes a WebRTC offer with one ordered data channel,
   `clawdline-terminal-v1`, gathers its candidates (at most 3 s), seals the SDP under the
   connection key and sends it as a `direct_offer` terminal request over the relay. The machine
   applies every terminal rule to it first: a live connection that this viewer owns, the viewer
   allowed terminals (principal, `send_prompt`, pairing pin), the setting on, one peer per viewer,
   at most 8 peers per machine and 6 offers per viewer per minute.
2. **Answer.** The machine (pion, `internal/transport/cloud/direct.go`) answers with its own
   candidates in the keyed receipt. Both SDPs, and so both DTLS fingerprints, travel only inside
   signed, end-to-end encrypted envelopes; the relay sees neither.
3. **Switch.** When the channel opens, the browser rekeys the connection with `carrier:"direct"`.
   From then on `term`/`termd` frames and `termi` requests ride the channel, in the same signed and
   sealed envelopes as before. Notices, probes and every connection registration stay on the
   relay; so do receipts, except those of typing, pasting, resizing and lease checks, which the
   machine sends on the channel when the browser asks (2026-10-06: they had made every keystroke
   spend the relay's per-account budget of 16 messages per 2 s, and a refused one dropped the
   connection).
4. **Flow.** The machine keeps one frame in flight, as before, but the browser acknowledges it on
   the channel after drawing it, so the pace is set by the direct round trip, not by the relay's.

**The relay stays the authority.** Every second the machine sends a small probe for each direct
connection on that connection's relay receipt channel. If the relay refuses to deliver it (the
viewer's terminal authority was withdrawn, or the viewer is gone) or does not settle it within 2 s,
the machine retires the connection. A revoked device is refused frames and input within 4 s and
told within 5 s, the same bound as on the relay.

**It falls back by itself.** If the channel does not open within 5 s, closes, or misses an ack for
3 s, the machine retires the direct connections and the browser rekeys back onto the relay (or
reopens with its lease proof if the old connection is gone). Input sent before the drop is settled
by its relay receipt, so nothing is replayed. The browser waits 30 s before trying again, or 5 s
when the refusal only meant "not now" (`terminal_busy` from the machine, or the relay's
`rate_limited` or `over_capacity`). After two direct paths of one terminal fail in a row, that
terminal stays on the relay for 5 minutes (`DIRECT_PAUSE_AFTER_FAILURES`, `DIRECT_PAUSE_MS` in
`terminal-direct.ts`). While no frame is pending, a path that loses its network is retired on
ICE state instead: the machine gives a disconnected pair 4 s and retires the peer only when ICE
reports failed, up to 25 s later (`CloudTerminalDirectICERecoverySecondsLimit`).

The page shows "direct" next to a terminal on the direct carrier, and the wire contract with every
number is in `docs/cloud-terminal-wire.md`, Direct carrier.

**Where the person sees which road a machine is on.** The carrier is one channel per page and
machine, shared by that machine's terminal and by every Session read of it, so it is a fact about
a machine and never about a Session: two Sessions on one machine cannot take different roads, and
two machines routinely do. It is therefore drawn on the machine's own row — the fleet list's
machine heading and the switcher's machine rows — as one dot and no word: filled when this page's
reads of that machine take the channel, hollow when they take the relay, faint when this browser
has not read that machine's descriptor yet, which is not the same as relay. Before the road, the
dot reads whether the machine still reports in: a machine whose roster entry has gone stale is
drawn offline whichever road it used to take, and one with no report yet is drawn as unknown
(`machineLight`). The tip says which and why (the machine opens no direct path, there is none
right now, or the machine is not reporting), and says that sends and dispatches take the relay
whatever the dot shows (`carrier-state.ts`, `CarrierDot.tsx`). Nothing is opened to draw it: the
dot reads the roster and the carrier this page already holds.

A hollow dot on a machine is usually its build or its switch, not its kind. `session_carrier_v1`
is `cloud_terminal_direct`, which is on unless the settings file says otherwise, and this path has
no build tags and no platform test — pion is pure Go. Measured on 2026-10-11 against one account's
two Linux machines, neither of which carried the key at all: one was three days older than the
carrier and one was built as `devel`, and after both were updated to the same commit as the Mac,
one of them published the key and the other did not, because its own settings file had turned the
switch off. With the switch on it opened a carrier in 927 ms.

The machine's local Settings → Remote page and `clawdline cloud direct on|off` now expose that
switch, and beside it the same-network option (`cloud_direct_local_network`, off by default; see
Decisions). Changing it replaces the active Cloud line, retiring open direct peers; the viewer can
continue over the relay. For an EC2 private NIC with a public IPv4 mapping, setting both
`cloud_direct_udp_port` and `cloud_direct_public_ip` makes Pion share one UDP listener and publish
that external IPv4 at that port as a host candidate. The daemon then omits its STUN servers so it
does not gather an extra server-reflexive candidate on an OS-selected port. See [Linux setup](linux.md#let-a-cloud-browser-connect-directly-to-ec2) for the security-group cutover and verification.

**A machine that cannot send UDP answers the offer and still cannot connect.** The other updated
machine, a cloud host whose network allows outbound TCP and DNS and no other UDP, answered every
`carrier_offer` in about 3.5 s with a sealed answer SDP — authority, signalling and capability all
working — and its answer carried only private host candidates, because the STUN servers it would
have learned its public address from are reached over UDP it may not send. Nothing could be tried,
the data channel never opened, and each attempt cost about 9.3 seconds (3 s gathering plus the
negotiate bound) before the page went back to the relay for the next `DIRECT_RETRY_MS`. There is no
TURN, so that machine's dot stays hollow until its network lets it speak UDP, and the page's own
sentence for it is the right one: there is no direct path to it right now.

## Measured after the change (2026-10-05)

In app.clawdline.com in Chrome, against the fixture terminal, on one page load per carrier. Each
sample types one character into the terminal and times from that keystroke to the first terminal
screen (`term/` or `termd/` envelope) the page receives, on the WebSocket for the relay or the data
channel for direct. The relay run kept the same page on the relay by removing `RTCPeerConnection`
from the page before it opened the terminal; nothing on the machine was changed.

| Carrier | Median | p95 | Range | Samples |
| --- | --- | --- | --- | --- |
| relay | 443 ms | 832 ms | 341–832 ms | 15 |
| direct | 18.5 ms | 26 ms | 12–26 ms | 20 |

One more relay sample, 40 ms, is left out: it was an idle-screen heartbeat that happened to arrive
right after the keystroke, not the echo. A separate run timed to the page's `frame_observed` (the
screen drawn) gave 35 ms and 34 ms on direct.

The browser and the machine were the same Mac, so the direct figure is what remains without a
network: tmux, one `CaptureGap` (33 ms at most), crypto. Between two networks it adds that pair's
own round trip, not the relay's crossings to San Jose. Each run was short because Chrome reported
the tab as hidden, and at the time a hidden page released its Cloud line after a minute. That was
removed on 2026-10-06: a hidden page now keeps its Cloud connection exactly as a shown one does.

## Decisions

- **pion/webrtc** was the daemon's second third-party dependency after `modernc.org/sqlite`. It is
  pure Go (MIT), so `CGO_ENABLED=0` still builds macOS, Linux and Windows from one machine. Measured
  before adopting it: a pion↔pion channel opened in 12 ms with a 0.2 ms round trip for 4 KiB, and a
  Chrome↔pion channel on the same machine opened in 198 ms (through an mDNS host candidate, no
  permission prompt) with a 0.41 ms round trip.
- **Public STUN only, no TURN.** Both ends ask `stun.cloudflare.com` and `stun.l.google.com` for
  their public address so that two networks behind ordinary NATs can still meet. Those servers
  learn each end's public address and nothing else. There is no TURN server: when no direct pair
  works, the relay is the fallback, and running a TURN service would be a second relay.
- **The machine only checks addresses it may reach.** It drops loopback, link-local, multicast and
  unspecified candidates, never gathers on them, and accepts at most 32 candidates per offer.
- **Nothing is sent to the local network** (2026-10-16). macOS asks before a process reaches a device
  on the local network, and the dialog names Clawdline: opening a Session on Cloud asked "allow
  clawdline to find devices on your local network", because the viewer's offer carried its private
  address and the machine sent checks there. So the machine also drops private (RFC 1918,
  unique-local IPv6) candidates and `.local` names, never resolves mDNS (a resolution is a
  multicast query), and leaves its own private host addresses out of its answer. The pair is made
  over the public addresses STUN finds. The cost: a viewer on the same network, or in a browser on
  the same Mac — the 198 ms mDNS pair measured above — reaches the machine only through the
  router's public address, which some routers do not loop back; then the relay carries it, as for
  any viewer without a direct pair. The local console is the fast path on the same Mac. This also
  retires the earlier residual risk that a viewer could make the machine probe private addresses
  of its choosing.
- **Same network is the person's choice, off by default** (2026-10-18). With the trace in place,
  one Mac's log held 41 direct attempts: 39 opened, every one on `srflx/ipv4 > srflx/ipv4`, and 2
  timed out with ICE stuck at checking and only public IPv4 candidates on both sides. The phone's
  `.local` host candidates were dropped (`offer_dropped=name:host/mdns:2`) and the Mac's own
  private host candidates left out of its answer (`answer_dropped=private:host/ipv4:2,…`), so a
  phone on the same Wi-Fi could only reach the Mac by hairpinning through the router. The machine
  setting `cloud_direct_local_network` (absent is off; Settings → Remote, shown only while direct
  connections are on, or `clawdline cloud direct local on|off`) widens the filters by exactly one
  thing: **RFC 1918 IPv4 host candidates**. The machine keeps its own in its answer and accepts the
  viewer's. Link-local, loopback, multicast and unique-local IPv6 stay dropped — nothing measured
  needs them — and so do private server-reflexive candidates. `.local` names stay dropped too:
  resolving one is a multicast query, pion would need its mDNS mode turned on for it, and a phone's
  browser hides its private address behind such a name anyway. What carries the same-network pair
  is the machine's answer: the phone sends checks to the machine's private address, and the machine
  learns the phone's private address from those checks as a peer-reflexive candidate (expected
  from ICE; not yet measured with a phone). The first check sent to the local network is what
  makes macOS show its Local Network prompt naming Clawdline, once; turning the setting on is the
  person inviting it. If the permission is denied, those checks fail and the pair is made over the
  public addresses as before, or the relay carries the connection. Off, both filters answer byte for
  byte what they answered before the setting existed (`TestSameNetworkCandidatesFollowTheSetting`).
  Changing it locally replaces the Cloud line like the direct switch does. The hosted Devices
  sheet can also offer an enable-only action after failed ICE with a shared public IPv4 exit,
  described as a possible same network rather than proof of a LAN. That command requires the
  paired viewer's operation permission and persists the choice without replacing the Cloud line:
  the relay must deliver its setting receipt before the browser retries. Subsequent offer and
  answer filters read the persisted policy, including revocation. The UI explains that the target
  machine may show the macOS permission prompt, and reports setting success separately from ICE.
  The residual risk when it is on: a viewer the person has paired can now name private IPv4 hosts
  in its offer, and this machine sends ICE connectivity checks to them. That is bounded by the same
  admission as every offer — it must decrypt under that viewer's key, at most 32 candidates per
  offer, 6 offers a minute per viewer, 8 peers per machine (`direct.go`) — so a paired viewer can
  make this machine probe at most a few hundred LAN addresses a minute with STUN binding requests.
  Off, it cannot name any.
- **No relay change.** Keeping receipts and registrations on the relay and probing it from the
  machine gives the relay's gate the same reach without a new relay message or deploy.
