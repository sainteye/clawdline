# clawdline-go

A parallel rebuild of Clawdline: the same product, with its core in Go, its
interface the existing web console, and macOS / Windows / Linux supported from
the first day.

**This repository does not modify `~/code/clawdline`.** That tree is read, never
written. Both applications can be installed and run at the same time: this one
uses its own bundle id, its own config directory (`~/.config/clawdline-next`),
its own port (7727) and its own token.

## Status

P0. The daemon owns `/v1/health` and proxies every other route to the Swift app
on 7717, so the console works in full while routes move across one subsystem at
a time. The proxy is scaffolding; P4 removes it.

## Run

```sh
go run ./cmd/clawdline serve     # listens on 127.0.0.1:7727
go run ./cmd/clawdline doctor    # prints the resolved paths and ports
```

Open <http://127.0.0.1:7727> with the Swift app running.

## Documents

- [`docs/plan.md`](docs/plan.md) — architecture, milestones, coexistence rules
- [`docs/coordination.md`](docs/coordination.md) — the Clawdfather redesign
