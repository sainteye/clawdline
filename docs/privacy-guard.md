# The privacy guard: what a public repository publishes is every moment it ever had

This repository is open source. Nothing of the person's may appear in it: no names of their
businesses, no tokens, no paths into their accounts, no content from their conversations.
`tools/check-private.sh` is the thing that notices. `internal/domain/privacy` holds the rules;
`tools/check-private/` finds the bytes to run them over.

There are two scans, and the second one exists because of a measurement.

## Why the tree scan is not enough

`tools/check-private.sh` reads the **working tree**: every file `git` would commit. On
2026-09-20 somebody scanned the 289 commits behind it by hand — nothing asked them to — and
found this:

- `docs/cutover.md` gained three lines at commit `ef067d70` (2026-09-18) carrying a word from the
  person's own list.
- `7eb88286` took the same lines out the next day.
- So the working tree is clean, **and the guard was green** — while the three lines are still in
  the history, and `git push` publishes the history.

That is not a bug in a rule. It is the scan reading one moment of a repository that publishes all
of them. It was caught because a person thought to look; the next one will not be.

## The two scans

```sh
tools/check-private.sh                        # the working tree
tools/check-private.sh -history               # the commits, from the last checkpoint
tools/check-private.sh -history -new          # red only for one today added
tools/check-private.sh -history -full         # every commit, whatever the checkpoint says
tools/check-private.sh -history -revs=--all   # every ref, which is what a push sends
tools/check-private.sh -rules                 # what each rule catches, and what passes it
```

`-history` reads git objects, not files. The unit of work is a **blob**, not a commit: the same
blob sits in every tree after the commit that wrote it, so scanning per commit would read this
repository's `internal/contract/zz_generated.go` three hundred times. `git rev-list --objects`
deduplicates them, and the commit is put back onto a finding afterwards.

`-revs` is what a push would send. The default is `HEAD`, which is what you want before a commit;
`--all` is what you want before making the repository public, because every branch and tag goes
with it.

## Four answers, and why the fourth one exists

| Exit | Answer | What it means |
| --- | --- | --- |
| 0 | clean | Everything in scope was read; no rule fired |
| 1 | found | At least one finding, printed with where it is |
| 2 | cannot check | The run could not start: no repository, a failed build, git did not answer |
| 3 | undetermined | It read what it could, and the answer is not known |

A run that read no files is 2, never 0, and a run with **no private-word list** is 3, in both
modes. The word list is not in git — it cannot be, see below — so on a fresh clone and on a CI
runner there is no list, the `private-word` rule cannot fire at all, and a checker that answers
"clean" there is answering a question it did not ask. Exit 3 is the same refusal as
`check-legacy-css.sh`'s "the original is not on this machine": **the third answer is not a pass.**

An object too large to read (`privacy.MaximumObjectBytes`, 16 MB) is also 3, naming the object.
"Too big to read" and "nothing in it" are the two answers this whole guard exists to keep apart.

When a run both finds things and cannot decide, it exits 1 and says on the summary line that this
is not the whole answer. Both are red; "there are 558 of them" is the one somebody can act on.

## Two gates, because this history is already red

`ef067d70`'s three lines are in this repository's history for good, unless somebody rewrites it —
which is a decision about the whole repository and not a patch. A check that is red for ever is a
check people stop reading, and then it may as well not exist.

So there are two gates, and they ask different questions.

**Before a commit: `-history -new`.** It prints every standing finding, marks the ones the
checkpoint had not already recorded with `[new]`, and is red only for those. 3.9 seconds. The
question it answers is the only one a commit can act on: *did what I am about to add put
something else in?* Without a checkpoint it believes nothing is standing, so everything is new
and it is red — the first run of the day on a fresh clone is the full answer, not a free pass.

**Before publishing: `-history -full -revs=--all`, and read all of it.** Every ref, every commit,
nothing skipped, no notion of standing. What comes back is the list of what a `git push` to a
public remote would put in front of everybody, and every line of it is a decision somebody has to
take before the push and not after.

## What a finding says, and what it never says

```
ef067d70 2026-09-18 docs/cutover.md:81: private-word — history only, not in the working tree
```

The commit, its date, the file, the line, the rule, and what the working tree does about it:

- **still in the working tree** — the same thing is at that same path today. Fix the file first.
- **gone from this file; the working tree carries it in `<paths>`** — that line is clean and the
  same thing is still published somewhere else. Fix that file.
- **history only, not in the working tree** — nothing on disk to edit. Only rewriting the history
  takes it out, which is a decision about the whole repository and not a patch.

**It never prints what the rule matched.** A privacy report is a document somebody keeps, pastes
into a task, and eventually publishes; a report that quotes the word it caught has moved the leak
rather than closed it. `git show <commit>:<file>` reads the line locally, which is where it
belongs. (The working-tree scan does print the match — it is telling you about a file open in
front of you — and masks a credential either way.)

## The checkpoint

A full read of this repository's history costs about **17 seconds**. Measured 2026-09-21, on the
same machine, same word list:

| Scope | Commits | Objects | Read | Time |
| --- | --- | --- | --- | --- |
| The whole history | 364 | 2,599 | 51.9 MB | 16.6s |
| The first 289 of them (`9dec9d9e`) | 289 | 2,180 | 40.0 MB | 13.4s |
| Nothing new, from a checkpoint | 0 | 0 | — | 3.9s |
| The working tree alone | — | 949 files | 11.6 MB | 3.7s |

Both full reads come out at about **3 MB/s**, which is the shape of the cost: it follows the
total bytes the repository has ever held, not how much changed today, and it only ever goes up.
At 289 commits it was 13 seconds and nobody would notice; the interesting number is that four
days of work added 12 MB and 3 seconds to a check meant to run before every commit. So a run
remembers what it read, in
`$(git rev-parse --git-common-dir)/info/private-history` — beside the word list, in the one
directory git never commits and every worktree of the clone shares. The next run is
`git rev-list --not <those tips>` over it: **3.9 seconds** when nothing new has been committed,
and it still reports everything the skipped commits held.

It carries the findings forward on purpose. This repository's history has one that cannot be
taken out without rewriting it, and a checkpoint that refused to advance past a standing finding
would make every run a full one for ever.

**The checkpoint is a cache and never an authority.** The only thing it can do is let a run skip
commits, and a run that skips says so and names the tip it skipped from. Every way of not
believing it ends in the same place — a full read — and says which way it was:

| Reason | What happened |
| --- | --- |
| absent | no checkpoint yet, or `-checkpoint=-` |
| `malformed` | not this format, truncated, a line it does not know, a repeated field |
| `unsealed` | the seal is not over these bytes: corrupted, or edited |
| `rules-changed` | the rule set moved, so the earlier scan answered a different question |
| `words-changed` | the word list moved, likewise |
| `missing-tip` | a recorded tip is not in this clone: the history was rewritten |
| `-full` | you asked |

The seal is a SHA-256 over the file's own body. It catches corruption, truncation and an edit;
it is not keyed, and it is not meant to defeat somebody who wants a green board — that person can
delete the guard. What it buys is that **skipping cannot happen quietly.**

The word-list digest is salted with a random value kept in the same file. Without the salt it
would be a SHA-256 of a handful of short words, which is a word anybody with a dictionary can
read back — a digest of the private list is not a place to be relaxed about that.

## The word list

The person's own words — their project names, a client, their real name — cannot be written into
a public checker without publishing them. They live outside the repository, one per line:

```sh
$EDITOR "$(git rev-parse --git-common-dir)/info/private-words"   # or $CLAWDLINE_PRIVATE_WORDS
```

`.git/info/` is never committed and is shared by every worktree of the clone. A line starting
with `#` is a comment. Without this file the guard answers 3, in both modes.

## The bounds

Two, both on the capacity guard's baseline (`internal/domain/capacity/testdata/baseline.txt`)
rather than in `capacity.Register`: the register is what the *daemon* holds and measures, and a
row for a checker that runs before a commit would have to claim `/v1/health` reports it.

| Bound | Value | At the limit |
| --- | --- | --- |
| `privacy.MaximumObjectBytes` | 16 MB | the object is not read, and the run answers undetermined naming it |
| `privacy.MaximumCheckpointBytes` | 1 MB | the checkpoint is refused rather than parsed, which costs a full read |

## Where the code is

| Part | File |
| --- | --- |
| The rules, and what each one lets through | `internal/domain/privacy/privacy.go` |
| The four answers, the checkpoint, the digests | `internal/domain/privacy/history.go` |
| Finding the files of the working tree | `tools/check-private/main.go` |
| Reading the objects of the history | `tools/check-private/history.go` |
| The entry point the other checks are run like | `tools/check-private.sh` |

The tests drive each half against a repository built for it. The live case — `docs/cutover.md` at
`ef067d70` — is what the work was measured on and cannot be a fixture: a test that spelled the
person's word would publish the thing the guard exists to stop.
