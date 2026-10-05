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

<!-- filled in as the implementation lands -->
