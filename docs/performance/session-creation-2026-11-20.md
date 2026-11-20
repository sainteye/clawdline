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

## Browser follow-up on the tmux path

Chrome created six new Sessions against the running local daemon at `04d2735ea`,
alternating three Codex and three Claude starts in a registered Project.
Chromium network timing put the successful creation reply at 462–600 ms.
Sampling the newly created tmux pane found its native prompt at 1,517–2,853 ms.
The browser received the exact new execution in a Session event at 2,496–4,118 ms.
No prompts were submitted during these runs.

Three separate stages contribute to the visible wait.

- A tmux launch waited at least 250 ms after its shell received the first Enter.
  The shared send path uses that wait to check whether an assistant composer
  still holds a pasted message. A new login shell now returns after the launch
  line is visible and the single Enter succeeds.
- The native assistant initializes after the terminal accepts the command.
  That work remains on its own clock.
- The periodic list could miss a new process or represent it as unknown while
  the first screen capture ran in the background. Confirmed launches now share
  one early fresh inventory read when a console is watching. A launched tmux
  pane uses the existing two-second live screen window, so that first read can
  return its current prompt or menu. Outside this window and after a failed
  read, ordinary discovery and screen capture continue.

### Controlled browser comparison

The same built console, registered Project, private tmux socket configuration
and native assistant binaries were used for each variant. Each variant opened
three Codex and three Claude Sessions through fresh Chrome instances. The
baseline daemon was `04d2735ea`. Each test began when Chrome activated the
Project button. The network column used the launch POST's resource timestamps.
The native prompt was sampled on the exact pane returned by that POST. The last
two rows used the first browser Session event matching that pane and its first
idle state. Every test ended through the ordinary close route.

| Milestone | Baseline median | Candidate median |
|---|---:|---:|
| Creation reply | 498 ms | 207 ms |
| Editable browser composer | 542 ms | 254 ms |
| Native prompt observed | 646 ms | 634 ms |
| First Session list event | 1,886 ms | 1,389 ms |
| Session list reports idle | 3,930 ms | 1,389 ms |

All six baseline list entries first reported unknown. All six candidate list
entries first reported idle. The candidate's idle observations ranged from
1,318 to 1,532 ms versus 1,968 to 5,857 ms on the baseline. The native prompt
measurements overlap. The browser wait improved through the receipt and
discovery paths. These are local results with low background load. The
production Cloud relay, other terminal backends and sending an actual prompt
remain separate measurements.

The early-read test covers a single coalesced fresh pass and waking its stream.
The terminal tests exercise the verified single-Enter launch and the ordinary
composer path. The first-read test checks that a confirmed pane can read its
current screen and that arbitrary unobserved pane IDs cannot seed the cache.
