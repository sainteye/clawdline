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

A proof is a shell script in `Tests/guard-red-proofs/`. Three headers, which the runner reads:

```sh
#!/bin/bash
# guard: tools/check-something.py
# defect: what the broken arm puts in front of it, in a few words
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
five existing proofs before copying one.

**Do not pin a literal the tree also owns.** `Tests/guard-red-proofs/version-strings.sh` reads the
app's version out of the fixture's `build.sh` rather than typing it, because typing it would be a
third place this repository's version is written down — caught by the very guard it is proving.

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

## What is not covered

`verify_suite_roster` and `verify_swift_source_manifest` are guards too, and good ones — the roster
check already carries a `--verify-suite-roster` mode written so that it *could* be seen to fail.
They are shell functions inside `test.sh` and `tools/swift-source-manifest.sh` rather than files
under `tools/check-*`, and this mechanism is keyed on files. Saying so here is the point: they are
outside it, deliberately, and somebody extending this should start with them.
