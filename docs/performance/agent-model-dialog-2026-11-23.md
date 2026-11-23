# The Bot model dialog waited for a new Agent process on every open

Measured 2026-11-23 on an Apple M4 Pro, darwin/arm64, at base commit
`7388204fb`, with the installed Claude Code and the supplied Claude ACP
adapter. Background load was shared and uncontrolled.

## The action and its impact

In a Bot chat the person taps "Model · Agent default"; the "Change model"
dialog shows "Reading Agent configuration…" until
`GET /v1/project-assistants/{bot_id}/configuration` answers. The person
reported, from a phone through Clawdline Cloud, that it stayed on that line for
a long time. Nothing in the dialog is selectable until the reply arrives, so
the whole reply time is on the person's critical path.

## Where the time went

The console makes exactly one request per open (`useModelCatalog` in
`RuntimeSettings.tsx`); it does no other work before showing the choices.
On the daemon, `Service.Configuration` checks authority and resolves the
Project, then calls the host, which for Claude Code:

1. re-reads the adapter manifest, hashes its artifacts and finds the person's
   Claude Code and Node (`ensureInstallation` inside `prepare`), and creates an
   isolated scratch directory;
2. starts a disposable Node adapter process, which starts Claude Code, then
   runs ACP `initialize` and `session/new` to read the offered models, and
   closes the process.

Nothing was remembered between opens, so every open repeated both steps.

A temporary in-package harness (not committed) called those two steps directly
with the real tools, 12 times in two rounds of six:

| Step | Samples (ms) |
| --- | --- |
| `prepare` (manifest, digests, tool discovery, scratch dir) | 409, 508, 435, 411, 373, 367, 357, 403, 367, 567, 416, 429 |
| probe (process start, `initialize`, `session/new`, close) | 767, 850, 2245, 1212, 1870, 584, 1262, 702, 1024, 642, 1268, 917 |
| **daemon total per open** | 1176, 1358, 2680, 1623, 2243, 951, 1619, 1105, 1391, 1209, 1684, 1346 |

Daemon total per open: **median 1.38 s, maximum 2.68 s**. The first
`ensureInstallation` on a fresh state directory took 3.4 s (one-time adapter
setup; production state is already set up, so it is excluded).

Not measured, and why:

- **Through Cloud.** The relay adds at least one round trip from the phone to
  the relay and back to this machine on top of the daemon time. The child had
  no permission to read the running daemon's credential, so no authenticated
  timing through Cloud or against the running daemon was taken.
- **The running daemon's history.** Its persisted API performance window
  (one hour, read-only) contained no configuration request, so it could not
  confirm the production distribution.

## Root cause in plain words

Every time the dialog opened, the daemon started a brand-new Agent program
just to ask it which models exist, waited for it to boot, and threw it away.
That costs about one and a half seconds on this machine, up to nearly three,
before any network time, and repeats on each open although the answer almost
never changes.

## The change

`Service.Configuration` now remembers the catalog the Agent reported
(`configurationCache`, `internal/app/projectassistant/configuration_cache.go`).

- **Key:** Agent, login profile, requested model and Project directory — every
  input the probe receives. Saved option choices are cleared before a catalog
  read, so they are not part of it.
- **One source of truth:** an entry is the Agent's own answer, copied
  verbatim; nothing is synthesized.
- **Invalidation:** an entry expires after `MaxConfigurationCacheSeconds`
  (600 s); at most `MaxConfigurationCacheEntries` (32) are kept, the oldest
  dropped first. Failed reads are never kept. Saving a model still validates
  it in its own native session; when the Agent refuses it, every entry for
  that Agent account and directory is dropped, so the dialog asks again.
- **Authority:** feature, source and epoch checks still run on every read,
  including a remembered one.
- Both bounds are registered in `internal/domain/capacity` (the guard went
  red first, then green).

Rejected alternatives:

- *Refresh in the background on each hit.* The host allows one discovery
  probe machine-wide (`MaxConfigurationProbes = 1`); a background refresh
  would make another dialog's first open fail with "busy".
- *Remember only in the console.* It would not survive a reload or help a
  second device, and each console would hold its own copy.

## After

| Open | Daemon time |
| --- | --- |
| First open of a key, or after 10 minutes | unchanged: median 1.38 s, max 2.68 s |
| Reopen within 10 minutes | the service read took median 2.6 µs, max 3.7 µs (7 samples, in-memory fixture); no process is started |

The remaining cost of a remembered open is the route's ordinary authority and
Project reads, the same work as other Bot reads, plus Cloud's round trip when
used. The first open of each key in a 10-minute window still waits for the
Agent; removing that would need the running Bot session to publish its catalog,
which lives in files another worker owns today.

Correctness risk: within 10 minutes, a change of the Agent's offered models
(a CLI upgrade, a different account behind the default login) is shown late.
Saving a stale choice is still refused by the Agent, and that refusal clears
the entry.
