# Recap: what is Clawdline's own

A recap skill that both Claude Code and Codex share reads this file at the end of a stretch of
work. The skill holds the four questions (what was done, what it bought, which documents were
written, what is still open) and their markers; this file holds only what is particular to this
repository.

## Evidence to collect first

```sh
git fetch -q origin
git log --oneline -15 origin/main             # the work, as main has it
git status --short
clawdline task show <id>                      # each child: state, landing, leftovers
tools/check-worktrees.sh                      # checkouts still owed (a dry run)
curl -s http://127.0.0.1:7727/v1/health       # the commit the local daemon runs
curl -s https://app.clawdline.com/BUILD.json  # the commit the hosted console serves
```

A child's leftovers live only in its task record. Copy them into "still open", or they are gone.
Read numbers from where they are produced (`daemon.log`, `sqlite3 -readonly`, `gh run view`),
not from the conversation.

## Documents to check

| Document | Must change when |
|---|---|
| `docs/design-decisions.md` | a new decision (one D row; two lines of work adding rows at once must not share a number) |
| `docs/interface.md` | behaviour, a setting or a screen a person sees changes |
| `docs/cross-platform.md`, `docs/linux.md` | what a platform does or does not do changes |
| `docs/user/install.md`, `README.md`, `README.zh-TW.md` | installation or a prerequisite changes |
| `docs/releasing.md` | the release process or a workflow changes |
| `internal/domain/capacity`, `docs/limits.md` | a new bound |
| English and Taiwan Traditional Chinese copy | any person-facing string (`docs/localization.md`) |
| Project memory (`clawdline memory add`) | a lesson the code does not record |

## Where open items go

| Who can resolve it | Where it goes |
|---|---|
| The person (a choice, consent to rebuild, deploy or release) | an `answer` Note (`clawdline guide note`) |
| An agent, not yet scheduled | a Board item (`docs/work-system-v2.md`) |
| Decided against | `docs/design-decisions.md` |

The recap ends, like every delivery here, with the two status lines in `AGENTS.md`.
