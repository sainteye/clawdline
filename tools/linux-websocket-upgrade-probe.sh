#!/usr/bin/env bash
set -euo pipefail

image='swift:6.1.3-noble@sha256:1d10836ca929943d70b2254e555b17247b132f6eed984654351cb6a8cc531613'
repo_root=$(cd "$(dirname "$0")/.." && pwd)

docker run --rm --platform linux/amd64 \
  -v "$repo_root:/workspace:ro" \
  "$image" bash -lc '
    set -e
    swiftc /workspace/tools/linux-websocket-upgrade-probe.swift -o /tmp/linux-websocket-upgrade-probe
    /tmp/linux-websocket-upgrade-probe wss://relay.clawdline.com/v1/connect?role=machine
    /tmp/linux-websocket-upgrade-probe https://relay.clawdline.com/v1/connect?role=machine
  '
