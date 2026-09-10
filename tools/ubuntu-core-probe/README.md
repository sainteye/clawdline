# Ubuntu Core compile probe

This is the W0-F boundary probe, not Clawdline's production package graph or a headless daemon.
It materializes a temporary SwiftPM executable from the two non-empty production sources named in
`core-sources.txt`, plus the probe harness, and then compiles and runs it on Ubuntu 24.04 amd64.
The root `Package.swift`, `build.sh`, `test.sh`, and the macOS application target are unchanged.

The CI job and local command use this exact image:

```text
swift:6.1.3-noble@sha256:1d10836ca929943d70b2254e555b17247b132f6eed984654351cb6a8cc531613
```

Swift Crypto is declared at exact version 4.5.2 and locked to revision
`da9d28d69ebe3894b18376c8f2395c2f37b8448f`. The full revision was verified against the upstream
`refs/tags/4.5.2` before it was recorded in `Package.resolved`. Its transitive Swift ASN.1
dependency is also locked, at version 1.7.2 revision
`d9a5b37470adc940d22c3bcd5ca6953a516b727f`.

Run it from the repository root:

```bash
docker run --rm --platform linux/amd64 \
  -e UBUNTU_CORE_PROBE_IMAGE='swift:6.1.3-noble@sha256:1d10836ca929943d70b2254e555b17247b132f6eed984654351cb6a8cc531613' \
  -v "$PWD:/workspace:ro" \
  swift:6.1.3-noble@sha256:1d10836ca929943d70b2254e555b17247b132f6eed984654351cb6a8cc531613 \
  bash -lc 'cd /workspace && ./tools/ubuntu-core-probe.sh'
```

On a shared Clawdline development Mac, put that Docker command behind
`bash tools/with-compile-lock.sh` so it uses the same machine-wide compiler slot as `test.sh`.

The executable reports exactly 17 checks covering canonical JSON bytes and strict failures,
signing and byte charging, Swift Crypto SHA256, clock calibration/admission/discontinuity behavior,
and a real `FoundationNetworking.URLSession` request to a short-lived loopback HTTP server. The
wrapper rejects the wrong OS/architecture, an empty/missing/changed source manifest, missing or
empty listed inputs, checked-in unlisted probe sources, a different dependency pin, any check-count
drift, or a missing completion receipt.

The red proof changes only the temporary copy of `CloudClock.swift`, moving the required stable
duration from 60 to 61 seconds. Check 15 must then fail:

```bash
# Use the same docker command above, changing only the final invocation to:
bash -lc 'cd /workspace && ./tools/ubuntu-core-probe.sh --red-proof'
```

This probe proves only that these two production Core sources compile and their selected contracts
run in the pinned Ubuntu environment. It does not claim that `ClawdlineCore` or
`ClawdlineApplication` packages exist, that the complete source graph compiles on Linux, that a
daemon or tmux/systemd lifecycle exists, or that Ubuntu is ready for production.
