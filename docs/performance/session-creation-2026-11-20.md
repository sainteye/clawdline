# Creating a Session · 2026-11-20

This path begins when a person selects a Project in the Start Session sheet. The
daemon admits the request, resolves the Project and starts the assistant on the
chosen backend. A successful reply identifies what was created. Discovering the
new execution and making its composer usable are separate milestones.

## Observations

The local API history returned 28 successful Codex starts and five successful
Claude starts in the last 24 hours on one machine. Thirty were in buckets at or
below one second. The largest recorded durations were 3,432 ms for Codex and
9,911 ms for Claude. These counts measure HTTP requests through completion, and
the retained history can include more than one daemon build. They do not include
the time before a browser sends the request or after the reply.

A separate read-only loopback sample measured five new Session list scans at
244–292 ms and five ordinary list reads at 2–5 ms. An immediate list read and a
fresh scan are different observations. Three direct local CLI authentication
checks each took 120–130 ms for Claude and 40 ms for Codex on this machine.
The 29 visible Projects included 18 paths in the explicit registry. That
measures eligibility in the list, not the fraction of launch requests.

## Bottleneck and change

Before preparing the provider, the start route obtained a fresh whole-machine
inventory and merged it with registered and provider-history directories. For
an ordinary explicitly registered directory, the inventory cannot establish
anything the route needs to resolve the selected ID. It can still make that
request wait for unrelated discovery sources while the opening slot is held.

The start route now resolves an exact saved ID against the current registration
and hidden paths, checks the selected directory and applies the normal workspace
and scratch exclusions. The list's recent-first display cap does not revoke a
saved Project. Discovery-only and protected paths still use the existing lookup.
Machine Session starts, resume, provider checks, the bounded opening queue,
idempotency and Cloud execution confirmation use their existing rules.

## Controlled comparison

An isolated HTTP test ran the actual start route and its receipt store, with a
local synthetic provider and one 250 ms inventory scan. No prompt was sent.
Three sessions per build measured request entry through a successful creation
reply.

| Route with a registered Project | Samples (ms) | Median (ms) | Scans per fresh request |
|---|---|---:|---:|
| Before | 290.8, 291.6, 292.7 | 291.6 | 1 |
| After | 72.2, 81.3, 66.2 | 72.2 | 0 |

The median fell by 219 ms for this setup. The regression checks that the saved
Project launches without the scan, an idempotent retry returns the same identity,
a live-only Project still needs discovery, and a removed Project stays refused
even if a Session still runs there. It avoids gating behavior on timing.

The sample does not explain the full slow tail. Terminal initialization and
provider readiness can still affect the actual launch. A Cloud browser can also
wait for the exact execution to appear after the daemon confirms creation. Those
steps were traced in code and lacked paired click-to-composer timings.

Run the comparison with
`go test ./internal/transport/http -run '^TestStartRegisteredProjectDoesNotWaitForSessionDiscovery$' -count=3 -v`.
