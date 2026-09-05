---
name: progress
description: |
  Use **before** starting or writing anything that will take more than about half a minute — a
  build, a test suite, a lint pass, a data import, a migration, a batch job, a deploy, a video
  encode, a long fetch loop, a script whose one job is to run one of those — so that the person
  who started it can watch it get there instead of guessing. One line wraps a whole command:
  `clawdline-progress run --label lint --typical 120 -- ./scripts/lint.sh`, and the run appears
  in Clawdline's bar and in a terminal status line with elapsed time and, when it has been
  measured, how far through it is. Triggers on "run the suite", "build it", "start the import",
  "kick off the deploy", "this is going to take a while", "how much longer", "is it still
  running", and 「跑一下測試」「開始編譯」「這要跑很久」「還在跑嗎」「大概還要多久」. Also use when
  adding a slow step to a script, a Makefile or a CI job. Do not use for a command that finishes
  at once, and not for reading or explaining a status file — this produces the progress, it does
  not consume it.
---

# Progress, for a run somebody is waiting on

**What the reader gets.** A long command normally says nothing until it is over, and the person who
started it is either sitting in front of that terminal or has no idea. This writes one small file
while the run happens, and Clawdline's bar and claude-bestiary's terminal status line draw it: what
is running, in which tree, since when, which phase it is in, and — when a measurement exists — how
far through it is. **The person watching from a phone stops having to guess how much longer.**

It is not specific to tests or builds. `label` and `phase` are free text, so a lint run, a data
import, a migration and a video encode all fit the same record.

## The one line

```sh
clawdline-progress run --label lint --typical 120 -- ./scripts/lint.sh
```

**Reach for this form.** The helper is the parent process, so the exit status is its own and the
signals are its own: it exits on the command's own status, and a run that is killed is written down
as killed. You install no traps and cannot get them wrong.

- `--label` is what a person reads. Two or three words, the thing being done: `lint`, `import`,
  `staging deploy`. With none, the command's own file name is used.
- `--typical <seconds>` is **optional, and only for a duration somebody has actually measured**. It
  is what turns the bar from elapsed time into a proportion. With no measurement, pass nothing: the
  field is then absent rather than invented, which is the whole point — to a reader an invented
  number is indistinguishable from a measured one. `./build.sh` in this repository passes none for
  exactly that reason.
- `--stale-after <seconds>` (default 900) is when a reader should stop believing a `running` row
  whose writer has stopped. `--log <path>` names a log worth opening.

Where the file is, in order: `$CLAWDLINE_PROGRESS` if it is set; otherwise
`/Applications/Clawdline.app/Contents/Resources/clawdline-progress.sh`; otherwise the same path
under `$HOME/Applications`; otherwise, inside a checkout of this repository,
`Resources/clawdline-progress.sh`.

## Inside a script that has phases of its own

```sh
. "$CLAWDLINE_PROGRESS"
progress_start --label test --typical 288
progress_phase guards
progress_phase compiling
# no explicit finish: the traps progress_start installed decide from the exit status
```

`progress_start` arms ERR, INT, TERM **and** EXIT. `progress_phase <words>` moves what the bar
shows — the words are drawn verbatim, so write them for a person. `progress_clear` takes the row
away; there is no finish to call.

**If the script keeps an EXIT handler of its own**, bash keeps exactly one, so a second `trap … EXIT`
silently replaces the helper's. Compose instead:

```sh
cleanup() {
  local status=$?
  # …whatever this script already cleans up…
  if declare -F clawdline_run_file_exit >/dev/null 2>&1; then clawdline_run_file_exit "$status" || true; fi
  return "$status"
}
trap cleanup EXIT
```

## Why this is a helper and not four lines of documentation

The record is simple enough to hand-roll, and the traps around it are not. Two agents with a full
brief got them wrong independently, on the same machine, and both had to measure their way out:

- **`EXIT` alone reads a killed run as a clean one**, because `$?` inside that handler is 0.
- **`set -e` does not fire `ERR` for a failure inside a function**, and no ERR trap ever sees a
  deliberate `exit 1`.
- **A handler that returns instead of exiting** lets a `TERM`ed script carry on from where it was
  interrupted and finish by declaring success.

All three end the same way: the bar says the run succeeded when it did not. `Tests/progress-helper.mjs`
drives every one of them against a copy of the helper with the line taken out, so the fix is known
to be doing something.

## What it will not do

It never fails the run it is reporting on: a missing `$HOME`, an unwritable cache directory or a
full disk costs the bar and nothing else. It writes one file, keyed by the working directory with
nothing truncated, through a temporary name and a rename, so a reader sees a whole file or the
previous whole file and never half of each. It polls nothing, starts nothing, and needs no running
app — the file is read by whoever happens to look.
