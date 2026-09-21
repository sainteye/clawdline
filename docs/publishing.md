# CI, the public remote, and publishing rewritten history

The public repository and the development repository intentionally do not have
the same history. The public history has been rewritten to remove private
names, home-directory paths, and network addresses. A normal push from the
development checkout can therefore disclose material that does not exist in
the public repository.

## Give a development checkout a readable remote

Run this once from the development checkout:

```sh
tools/configure-public-remote.sh
```

It adds `origin` with the public fetch URL and sets this checkout's
`core.hooksPath` to `tools/git-hooks`. The tracked `pre-push` hook rejects an
ordinary push and explains why. `tools/test-public-remote-guard.sh` proves the
rejection against a disposable local bare repository and also proves that no
remote ref was created.

The hook prevents an accidental ordinary push; Git's deliberate
`--no-verify` option can bypass any pre-push hook. Do not use that option from
the development checkout. Fetching is unaffected.

The setting is local Git configuration. A clone does not inherit it, which is
what keeps the publication path below usable.

## CI and what its green check means

`.github/workflows/ci.yml` runs the repository's checks on `ubuntu-latest` and
compiles and tests Darwin-only paths on a macOS runner. The Linux job also
cross-builds and vets Windows.

Two local checks cannot produce their strongest answer in GitHub Actions:

- `check-legacy-css.sh` compares copied files with the retired application's
  source tree, which is not part of this repository. CI invokes the explicit
  `--allow-missing-source` mode and prints a named skip. A local run without
  that flag remains strict and exits 2 when the source is absent.
- `check-private.sh` reads a private word list that must not be committed.
  `check-private-ci.sh` still runs the fixed privacy rules over the tree. Exit
  3 becomes a GitHub warning saying the private-word rules are undetermined.
  For history it builds a temporary fixed-rule checkpoint at the event's base
  commit, then runs `-history -new` at `HEAD`: an old finding stays standing
  and a finding introduced by the change fails CI. The temporary sentinel is
  not a substitute private-word list, and the workflow says that explicitly.
  A green CI result is not publication approval.

`tools/test-go-ci.sh` names four host-integration exclusions in every run and
first verifies that every named test still exists:

- `TestADaemonThatCannotBindNamesWhoHasThePort` and
  `TestTheSystemNamesThisProcessAsTheListener` require the host process table
  to identify and timestamp a live listener.
- `TestReclaim` must prove a live child command from the host process table
  before it signals that process.
- `TestTheRealListingCarriesWhateverItCouldNotRead` needs both the live
  process table and an iTerm2 Apple Event.

Hosted runners cannot prove those host identities and have no iTerm2 session.
All fixture-backed process and iTerm tests still run on their supported
platforms.

The repository does not need a second full Windows runner: the Linux job
cross-builds the static Windows daemon and runs `GOOS=windows go vet ./...`,
which compiles Windows-only source and tests. The macOS job is retained because
Darwin-only source cannot be covered by either Linux execution or a Windows
cross-check.

## Publish from a disposable filtered clone

Never publish from the development checkout. Use a new disposable clone and
keep the replacement rules and private-word list outside the repository:

1. Record the current public `main` commit with `git ls-remote`. This is the
   expected value for the later force-with-lease.
2. Clone the development repository into a new temporary directory with
   `--no-local`. Do not run `tools/configure-public-remote.sh` there.
3. Put the private replacements in an untracked `git filter-repo
   --replace-text` file outside the clone. It must cover the private business
   names, home-directory paths, and network addresses for this publication.
4. Run `git filter-repo --force --replace-text <outside-file>`. The rewrite
   removes the clone's origin by design; do not restore it yet.
5. Put the complete private-word list in
   `.git/info/private-words`, then run every check in `AGENTS.md`. In
   particular, run:

   ```sh
   tools/check-private.sh
   tools/check-private.sh -history -full -revs=--all
   ```

   Both must be determinate and clean. Read the complete history report.
6. Add the public repository under a publication-only remote name, fetch it,
   and inspect the exact ref update. Push the filtered `HEAD` to `main` with
   `--force-with-lease=refs/heads/main:<recorded-public-commit>`. Do not use a
   bare `--force`, and do not publish any other ref.
7. Fetch the public `main` again and verify that its commit is the filtered
   commit that passed the full privacy scan.

The replacement file is deliberately not in this public repository: spelling
the private terms here would publish the material the rewrite exists to
remove.

## When the GitHub status appears

The status-line producer is
[`claude-bestiary`](https://github.com/sainteye/claude-bestiary), not
Clawdline. Its `statusline.py` reads the cache on each redraw and starts
`gh-run-status.py` when a completed/non-running entry is older than 90 seconds,
or every 5 seconds while a run is marked running. The poller asks for the 15
most recent runs on the current local branch. After it writes `ok`, the status
line redraws on its normal cadence; the success mark is eligible for 15
minutes from the run's start. A failure remains visible for 6 hours before the
poller replaces it with `{"state":"none","why":"stale-fail"}`.

Clawdline reads the same file. Its project-links cache is fresh for 30 seconds,
so a links request may serve one old reading while it refreshes in the
background.

With a matching local and GitHub branch, the conservative path from a
completed run to the terminal status line is therefore about 90 seconds plus
one GitHub CLI request and a redraw; if the running state was already seen, it
is about 5 seconds plus the request and redraw. Clawdline's links/status
surface can add up to another 30 seconds after the next request.

There is one current branch-name limitation: this development checkout uses
`master`, while the public repository runs CI on `main`. `gh-run-status.py`
passes the local branch name directly to `gh run list --branch`, and all
worktrees of the same remote share one `ghrun-<owner>-<repo>.json` file. A
`main` checkout can write the real verdict, but a simultaneously rendering
`master` checkout can later overwrite it with `no-runs`. CI and the remote are
necessary, but they do not by themselves make the shared cache stable until
the producer resolves the local branch to its upstream/default GitHub branch.
