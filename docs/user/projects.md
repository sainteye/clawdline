# Projects, and bringing them to another machine

After this page you can make a directory appear as a project to start sessions in, read a project's
worktrees and undelivered work, and give a second machine the same project names, icons and
untracked skills as the first.

The console's interface is in Traditional Chinese, the only language it ships so far. Labels below
are given as they appear, with their meaning in parentheses.

## Add a project

A project is a directory an assistant has run in. Clawdline finds them by itself from Claude Code's
and Codex's own records and from live sessions. To add a directory before any agent has run there:

```sh
./bin/clawdline project add ~/code/my-app ~/code/another-app
./bin/clawdline project list
./bin/clawdline project remove ~/code/another-app
```

These write the state directory directly and work without the daemon. There is no button for this
in the console.

**Check:** the directory is offered when you start a session ([sessions.md](sessions.md)).

## The Projects page

Open **專案** (Projects). It lists existing, durable directories explicitly added with
`clawdline project add` or found in assistant history and live Sessions. The retired Board catalog
does not decide whether a Project appears. Press a Project row to open its repository detail.
Under **版本庫生命週期** (repository lifecycle) each repository shows its **工作樹**
(worktrees), work that is **做完了，沒有落地** (delivered, not landed) and what is **已落地**
(landed). Worktrees that belong to no project are listed apart. Press **重新觀測** to look again.
**查看工作項目** opens the current Board filtered to that Project; Board membership and counts do
not control the Project list.

Each project has a pixel icon, used in the session list and on the Board. Icons are described in
[project-icons.md](../project-icons.md).

## Set up how a project appears

At the top of **專案** (Projects), **專案能力健檢** (Project capability check) shows four separate
facts for every project: whether its icon was chosen or only generated from the path, whether local
deploy-progress evidence is connected, whether `.devstack.json` declares development services,
and whether a git origin can identify the same project on another machine. The deploy row separates
"progress is connected" from the current run's outcome, so a failed run asks for attention without
being misreported as missing configuration. A non-GitHub remote is marked not applicable and is
not counted as unfinished GitHub-workflow setup.

The gear beside a project's **工作樹** button opens the same check for that project alone.

This check is local and read-only. It runs no repository command, probes no port, reaches no
network and starts or deploys nothing. An older daemon that does not send the evidence is shown as
unknown rather than guessed. Loading, empty and unreadable answers each remain visible and offer
**重新讀取** (read again).

Press **補齊設定**, **檢查設定** or **重新檢視** on a project. Clawdline opens its ordinary
new-Session review with that project already chosen and a first message covering the Project name
and pixel icon, deploy and long-running progress, and the development-server list. You can edit the
message, assistant and model before pressing Start; no Session is created until that final press.

The new Session reads the project-specific section compiled into the Clawdline binary:

```sh
./bin/clawdline guide project
./bin/clawdline guide zh-TW project
```

That guide separates display configuration from operational changes. It does not authorize the
Session to deploy, start or restart anything, and it requires unknown, stale and unreadable states
to remain visible rather than being reported as success.

## Bring your projects to another machine

A second machine — a Linux server on the same Cloud account, say — can have the repositories and
still lack what git does not carry: each project's name and icon in the console, the skills,
commands and agents you keep untracked under `.claude/`, and `CLAUDE.local.md`. One machine owns
those settings; the others mirror them read-only. Projects are matched by their git `origin`, so
the checkouts may sit at different paths.

### Before you start

- Both machines run this version. An older one is listed with **需要更新 Clawdline** (needs a newer
  Clawdline) and cannot be chosen.
- For the Cloud route: the browser is paired with both machines, and the receiving machine lets it
  act (`./bin/clawdline cloud commands on` there; [remote-access.md](remote-access.md)).
- Each project has an `origin` remote the other machine can reach. A project with no origin, or a
  folder that is not a git repository, is listed as skipped with its reason.

### Through Clawdline Cloud

1. In the hosted console, open the **receiving** machine and go to **專案** (Projects).
2. Open **專案設定同步** (project settings sync) and choose the source under **主要機器** (main
   machine).
3. Press **讀取它的專案** (read its projects), untick anything you do not want, and, if the receiving
   machine lacks some repositories, tick the line that clones them. It names the directory they go
   into: the one most of that machine's projects already sit in, or `CLAWDLINE_PROJECTS_ROOT` if you
   set it for the daemon.
4. Press **同步所選的 N 個專案** (sync the selected projects). Each project reports what happened:
   files written, files kept because they were edited on this machine, or a clone in progress.

From then on, change a project's icon or skills on the source; opening **專案** on the receiving
machine applies whatever changed. To make a project the receiving machine's own again, press
**改回本機設定** (use this machine's settings) on its row.

### Without Cloud

```sh
./bin/clawdline project export --out projects.json          # on the source machine
./bin/clawdline project import --clone projects.json        # on the receiving machine
```

The file holds the contents of those skills and notes; move it the way you would move the files
themselves, and delete it afterwards. `--clone` is optional; without it a repository the machine
does not have is reported as missing. `--replace-source` takes over projects that another source
already mirrors here.

### Check

On the receiving machine, **專案** shows each project with the source's name and icon, and a
mirrored skill is in that checkout's `.claude/skills/`. Changing the icon there is refused
(`project_mirrored`): the source owns it.

### What is never copied

`.claude/settings.local.json` (it holds the source's permission grants), dot files such as `.env`
and build caches inside those directories, and your personal `~/.claude/skills`.

## Troubleshooting

| You see | Do this |
| --- | --- |
| `mirror_source_mismatch` | Another machine already owns that project's settings here. Choose that one as the source, or import with `--replace-source` |
| `project_mirrored` | Change it on the source, or press **改回本機設定** first |
| `clone_failed` | The receiving machine could not reach the remote with its own credentials. Clone it by hand into the named directory, then sync again |

## Deeper

- [project-sync.md](../project-sync.md) — identity by git origin, what is never copied, Cloud and
  file transport.
- [getting-started.md §7](../getting-started.md#7-bring-your-projects-to-another-machine) — the same
  steps in the longer guide.
