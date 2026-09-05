# A guard nobody has seen fail

A check that cannot go red is worse than no check, because it makes people believe somebody is
looking. Two arrived here on 2026-09-05, hours apart:

- `tools/git-hooks/pre-commit` compares a task's declared claims against the paths a commit
  touches. Inside a linked worktree that comparison is **identically true**. The hook said so
  honestly, in a line that looks exactly like a check that passed.
- A staleness gate compared `stale > worst`, which in the situation it was written for is an
  identity. Always true, always green, always agreeing with you.

Neither is a knowledge gap and neither is carelessness. There was nothing on the screen to notice.

`tools/check-guards-go-red.sh` is the mechanism: every `tools/check-*` has a proof in
`Tests/guard-red-proofs/` that puts one defect in front of it and requires it to say so, and the
same script refuses a guard that no proof names.

## The mutation has to be the failure the guard exists for

This is the rule the first pass was missing, and `Tests/changelog-facts.mjs` is why it is here.

That guard takes every HTTP route named in the CHANGELOG's `## Unreleased` block and asserts the
server still answers it. Its own header says so. It has a red proof in the easy sense: invent
`/v1/nothing-answers-this` and it exits 1 and names it.

Measured here on 2026-09-05, in a disposable copy of this tree:

| what was changed | what the guard said |
|---|---|
| `GET /v1/nothing-answers-this` added to Unreleased | **red** — "answers no such path" |
| `GET /v1/orchestrator/tasks/:id/nothing-answers-this` added to Unreleased | green |
| `/v1/orchestrator/tasks` renamed to `/v1/GONE/tasks` in `RemoteServer.swift` | green |

A route counts as answered if **any** run of its literal segments longer than three characters
appears anywhere in the source, so `/v1/orchestrator` answers for everything beneath it. Every
route anybody actually adds lives under a namespace that already exists — so the guard is blind to
its whole subject, and green for the one mutation a proof-writer reaches for first.

Deleting an entire route is not a thing that happens. Naming one that was never wired up is. So a
proof declares what the guard is **for**, and its broken arm has to be an instance of that:

```sh
# prevents: the CHANGELOG's Unreleased block promising an HTTP route the shipped server does not
#           answer — the failure tools/release.sh opens with, where the README described a product
#           the only downloadable build did not contain
```

A guard that cannot say what it prevents is a finding in itself, and the runner refuses a proof
whose `# prevents:` is too short to be a failure mode.

## What a proof is

Two arms over one defect. The runner requires:

- the **broken** arm exits non-zero, and its output contains the sentence the defect should produce;
- the **clean** arm's output does not contain that sentence.

Not "the clean arm is green". The architecture guard is legitimately red for minutes at a time
while a seal window is open, and a mechanism that switched itself off during exactly the changes
that need it would be worth nothing. What the pair proves is narrower and enough: *this sentence
appears when the defect is there and not when it is not.* Where the clean arm was already red for
some other reason the run says so and names it, rather than passing in silence:

```
  red     tools/check-architecture-boundaries.sh — Tests/main.swift grown past the ceiling it is
          held to (differential: the clean arm is already red for another reason: … the seal was
          measured somewhere else …)
```

The **control arm is not the fixed sample.** A verification on this machine once used the
already-repaired file as its control: that sample no longer had the disease, so its passing said
nothing about whether the test could see one. Both arms here come from the same prepared tree and
differ only by the mutation.

## Writing one

A proof is a shell script in `Tests/guard-red-proofs/`. Four headers, which the runner reads:

```sh
#!/bin/bash
# guard: tools/check-something.py
# prevents: the failure this guard exists to stop, in its own terms — a sentence, not a word
# defect: what the broken arm puts in front of it, and it has to be an instance of `prevents:`
# expect: a fragment of the sentence the guard should print about it
```

It is run twice — `"$1"` is `broken` or `clean`, `"$2"` is an empty scratch directory — and it ends
by running the guard, so the guard's exit status and output are the proof's. Two variables are in
the environment:

- `GUARD_REPO` — the real checkout, which is where the guard executable lives.
- `GUARD_BASE` — a copy of the tracked tree, made once per run. Several guards find their subject
  from their own path, so the only way to hand one a defect is to hand it a tree.

Guards that take a root from the environment (`CLAWDLINE_WEB_ROOT`, `CLAWDLINE_VERSION_SCAN_ROOT`,
`CLAWDLINE_CURL_SCAN_ROOT`, `CLAWDLINE_GUARD_ROOT`) need much less than a whole tree; look at the
existing proofs before copying one.

**A guard with two subjects owes two proofs.** `tools/check-curl-status.py` is named by both
`curl-status.sh` and `curl-status-briefing.sh`: one puts an unchecked call in a shell script, the
other puts one in a child briefing. The register holds names, not counts, so a second proof for the
same guard is ordinary — and a guard that grew a domain nobody proved would otherwise look exactly
like a guard that had not.

**And a fixture has to satisfy the guard's own refusals.** That same guard now refuses an empty
scan in three places, so `curl-status.sh` carries a skill-guide file it does not otherwise need: a
proof whose fixture trips a structural refusal is proving the refusal, and its clean arm goes red
for a reason that has nothing to do with the defect.

**Do not pin a literal the tree also owns.** `Tests/guard-red-proofs/version-strings.sh` reads the
app's version out of the fixture's `build.sh` rather than typing it, because typing it would be a
third place this repository's version is written down — caught by the very guard it is proving.

## When the guard is blind: three arms, and a marker that expires

A proof that does not hold is **two** findings, and they must not be confused: either the guard is
blind to its own subject, or the proof script never applied its mutation. So a proof that expects
not to hold declares `# known-blind: <what was measured>` and owes a third arm — `easy`, the crude
mutation that even a weak guard catches. The runner then requires:

- the **easy** arm goes red for this defect — which is what makes the next line a statement about
  the guard rather than about the fixture;
- the **broken** arm does not.

It reports the guard as `BLIND`, prints the finding, and exits 0. The run stays green because the
finding is about a guard the change was told not to repair — and it cannot rot into folklore,
because **the day somebody fixes the guard the marker is what fails**: an expectation of failure
that quietly starts succeeding is refused, with "take the marker out".

There is one today, `Tests/changelog-facts.mjs`, for the reason in the table above.

## Suites are guards too

`Tests/*.mjs` are guards in every sense that matters, and there are fifty of them. Requiring a proof
for each is a programme rather than a change, so what the runner holds is only that the count never
falls: `MJS_PROOF_FLOOR` in `tools/check-guards-go-red.sh`, the same shape as this repository's
file-size ceilings, raised by whoever adds the next proof. A count that can only go up is the
difference between "we will get to it" and "we got to one and then stopped".

## The meta check, and its own proof

`tools/check-guards-go-red.sh` matches `tools/check-*`, so it is on its own list and needs its own
proof. `Tests/guard-red-proofs/self-meta.sh` is it: a fixture tree carrying one made-up guard, with
a proof registered for it in the clean arm and nothing in the broken one. Pointed at that fixture
with `CLAWDLINE_GUARD_ROOT`, the runner does the meta check alone — what is under test there is the
register, and executing a fixture's scripts is not part of it.

The register rots in two directions and both are refused: a `tools/check-*` no proof names, and a
proof naming a guard that is no longer in the checkout.

## The one exemption, and its shape

`tools/check-assets.sh` builds a contact sheet of cropped screenshots for **a person to look at**.
It has no verdict: every exit it owns is about `ffmpeg` or a missing file, so a proof that it can go
red would only prove that `ffmpeg` can be hidden from it.

That exemption is written inside `tools/check-assets.sh`, next to the sentence that explains why it
cannot pass or fail, as `# red-proof-exempt: <reason>` — and the runner prints it on every run:

```
  exempt  tools/check-assets.sh — it has no verdict to prove. …
```

Not a list of exempt names kept in the runner. A list somewhere else goes stale silently, and it
puts the reason where the next reader is not looking. A marker with nothing behind it is a
silencer, so the runner requires at least sixteen characters of reason and reports a marker that
has none.

**Rename, do not delete.** The two web proofs each rename a name on one side of a contract — an
element id in `index.html` that the registry still looks up, a key in `i18n.js` that the modules
and `/v1/strings` still spell the old way. That is how those actually break. Adding a lookup for an
element nobody ever wrote is not.

**Add, where adding is how it comes back.** `curl-status-briefing.sh` does not take
`--fail-with-body` off a working recipe, because nobody does that. It adds a second recipe beneath
the first, written the way the first one was written before it was fixed — somebody adds a route,
copies the block above it, and the copy is older than the fix. The clean arm is the same tree with
that one block already carrying the flag, so what the pair separates is the flag on a new command
and nothing else.

## What is not covered

`verify_suite_roster` and `verify_swift_source_manifest` are guards too, and good ones — the roster
check already carries a `--verify-suite-roster` mode written so that it *could* be seen to fail.
They are shell functions inside `test.sh` and `tools/swift-source-manifest.sh` rather than files
under `tools/check-*`, and this mechanism is keyed on files. Saying so here is the point: they are
outside it, deliberately, and somebody extending this should start with them.
