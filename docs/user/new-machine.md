# Set up another machine with an Agent

A second machine starts empty: no git, no credentials, no repositories, and none of the project
settings git does not carry. Doing that by hand is a long afternoon of small commands. This page is
one prompt you paste into a Session on the machine that already works, and the Agent finishes the
rest on the new machine.

The prompt says the goals, not the steps. An Agent reads the machine in front of it — its package
manager, its shell, what is already installed — and picks the commands itself; a script written
here would be wrong on the first machine that differs.

## Why the Agent runs on the machine you already use

Clawdline Cloud deliberately has no machine-to-machine keys: one machine cannot command another,
and the new machine may not be running Clawdline at all yet. So the Agent runs where it can already
work — the machine with your repositories — and reaches the new one over access **you** give it in
the conversation: an `ssh` host, a `docker exec` into a container, a cloud shell. That access is the
one thing the prompt cannot guess, and it is the first thing the Agent asks for.

## Before you start

- A way to run a command on the new machine, and the ability to paste it into the conversation.
- An account that can read the repositories you want there (GitHub or another host).
- Anything needing a password on the new machine: either give the Agent a way to use `sudo`, or
  expect to run two or three commands yourself when it hands them to you.

## The prompt

```text
Set up another machine so it can work on my projects the way this one does.

You are on the machine that already works. The new machine is reachable only through access I give
you: ask me how to reach it — an ssh host, a `docker exec`, a cloud shell — before anything else,
and prove the access by running `uname -a; whoami; pwd` there and showing me what it printed.
Clawdline carries no commands between machines, so everything on the new machine goes through that
access.

Finish these, in order, and after each one tell me what the machine actually printed:

1. git. If `git --version` fails there, install it with that system's package manager. If that needs
   a password you do not have, give me the exact command to run and wait.
2. Credentials to clone with. Prefer generating a new key on the new machine and giving me its
   public half to add to my account or as a deploy key; do not copy a private key off this machine
   unless I tell you to, and never print a secret into this conversation. Prove it with a real
   clone, not with a login test.
3. Clawdline on the new machine. `clawdline version` there should answer and match this machine's
   build; if it is missing, install it and start it as that user's service. Check `tmux` too —
   assistants need it there.
4. The repositories. Ask me which projects, clone them into one directory on the new machine, and
   tell me that directory.
5. The project settings. Run `clawdline project export --out projects.json` here, move that file
   over the same access, run `clawdline project import --clone projects.json` there, and delete the
   file on both sides. It carries the project names, icons, untracked `.claude/skills`,
   `.claude/commands`, `.claude/agents` and `CLAUDE.local.md`. It deliberately leaves out permission
   files, dot files and secrets — do not copy those by hand to make up for it.
6. Show me it worked. On the new machine, list the projects and give me one line per project: its
   path, and the files the import wrote or kept. If I say I want to open that machine in the hosted
   console later, also run `clawdline cloud login` and `clawdline cloud commands on` there.

Rules: nothing is done because it should have worked — only because the machine printed something
that says it did. Collect everything that needs my hands into one message, and if you are blocked
on me and it cannot wait, send it with `clawdline notify` rather than only writing it here.
```

## 中文版本

```text
請幫我把另一台機器設定好，讓它能跟這一台一樣處理我的專案。

你現在在已經能用的那一台上。新機器只能透過我給你的管道連上：在做任何事之前先問我怎麼連（ssh 主機、
`docker exec`、雲端主機的 shell），然後在那台上跑 `uname -a; whoami; pwd`，把輸出給我看，先證明管道
是通的。Clawdline 不會在機器之間傳遞指令，所以新機器上的每一件事都要走那個管道。

依序完成下面幾件事，每做完一件就告訴我機器實際印出了什麼：

1. git。如果那台的 `git --version` 失敗，就用它自己的套件管理器裝起來。需要密碼而你沒有的時候，把
   要我執行的指令原樣給我，然後等我。
2. 可以 clone 的憑證。優先在新機器上產生一組新的金鑰，把公開的那半給我，我去加到帳號或當成 deploy
   key；除非我叫你複製，不要把這台的私鑰搬過去，也絕對不要把任何密鑰印在對話裡。要用一次真正的
   clone 來證明，不要只做連線測試。
3. 新機器上的 Clawdline。那台的 `clawdline version` 要答得出來，而且跟這台同一個版本；沒有就裝起來，
   並以那個使用者的服務啟動。順便確認 `tmux`，助理在那台上需要它。
4. 專案的 repository。先問我要哪些專案，把它們 clone 到新機器的同一個目錄下，並告訴我那個目錄。
5. 專案設定。在這台跑 `clawdline project export --out projects.json`，用同一個管道把檔案送過去，在
   那台跑 `clawdline project import --clone projects.json`，然後兩邊都把檔案刪掉。它會帶走專案名稱、
   圖示、未被 git 追蹤的 `.claude/skills`、`.claude/commands`、`.claude/agents` 以及
   `CLAUDE.local.md`；它刻意不帶權限設定檔、dot 檔與任何密鑰——不要為了「補齊」而手動複製那些。
6. 給我看它成功了。在新機器上列出專案，每個專案一行：路徑，以及 import 寫入或保留了哪些檔案。如果我
   說之後要在 hosted console 開那台機器，就再在那台跑 `clawdline cloud login` 和
   `clawdline cloud commands on`。

規則：沒有一件事可以因為「應該會成功」就算完成，只有機器印出來的東西能算。需要我動手的事情集中成
一則訊息給我；如果你被我卡住而且不能等，用 `clawdline notify` 送出來，不要只寫在對話裡。
```

## How to tell it worked

Open **專案** (Projects) on the new machine — through its own console, or by selecting it in the
hosted console. Each project appears with the name and icon the source machine draws, and a
mirrored skill is a real file in that checkout's `.claude/skills/`. Changing a mirrored project's
icon there is refused (`project_mirrored`): the machine you already use owns those settings.

## What this does not do

- It does not pick your access for you, and it cannot create it. If you have no way to run a
  command on the new machine, there is nothing for the Agent to drive.
- It does not copy `.claude/settings.local.json`, dot files, or anything holding a secret. Those
  are excluded by the export itself ([projects.md](projects.md#what-is-never-copied)).
- It does not keep the two machines in step afterwards. From then on, settings travel when somebody
  looks: [Projects](projects.md#bring-your-projects-to-another-machine) for the console path, and
  [project-sync.md](../project-sync.md) for the design.
