# Changelog

Release notes have lived on [the releases page](https://github.com/sainteye/clawdline/releases)
since 0.1.0. This file is where the next one is written before it gets there, and where the
older ones can be found from without leaving the repository.

The entries are prose rather than a list of commits. What belongs in one is **what changed for
somebody using this** — a commit log already exists and is better at being a commit log.

## Unreleased

### Added: the sentence you type several times a day, one press away

Two of them get typed into a phone all day, one thumb at a time: *commit（逐檔指名，不要 git
add -A）、push、deploy。* and *回報你剛剛做了什麼、什麼還沒做、接下來要做什麼。* Now you write one
once and press it.

The project mark on a session's header — the pixel icon that already means *this project* — is a
button of its own and opens that project's snippets, and `⋯` → **Snippets** opens the same sheet.
**A project nobody ever drew an icon for now has a mark anyway**: one generated from its own path,
four rows of seven cells in a colour of its own, so two projects the registry has never seen look
like two projects rather than like two empty boxes. A registered icon still wins. Pressing a row
puts its text in the composer and closes the sheet. **It does not send.** Sending is the second press, on the
button that already says so — a mis-tap on a phone in a pocket cannot run `commit, push, deploy` in
the wrong session, and the ordinary case of *snippet plus one more sentence*, "…, 但先跑測試",
needs no separate feature.

A snippet belongs to one project or to every project, and the Mac decides which project a session
is in by the mark's own rule — so a session running in an isolated worktree sees the snippets of
the checkout it was cut from rather than an empty list of its own. They live beside your schedules,
one file each in `~/.config/clawdline/snippets/`, and the list rides the snapshot the page already
receives — which is what lets a phone on the relay press one too, with nothing to wait for.
Making and changing them stays on the direct path, and a device that may only read still sees the
list and is told why it cannot insert.

**Clawdline never reads what a snippet says.** It puts the words in the box; what they mean is
between you and the assistant. There is no expansion, no placeholder and no per-snippet "send on
tap" — both of those were declined rather than postponed, and the reasons are in
[`docs/snippets.md`](docs/snippets.md).

The sheet speaks all fourteen languages. The two starters an empty list offers are the exception to
how the rest of this app is translated: they are text a person sends to an assistant rather than
the interface talking, so each language says what somebody there would actually type.
### Fixed: a key pressed from a phone could reach the terminal and do nothing at all

The panel asks tmux for a screen **with its colour still on**, because it draws that screen for a
person to read. The parser that decides what a screen is showing strips no escapes of its own — it
was always handed text somebody else had already cleaned. Exactly one caller did that cleaning, the
one that polls every session a second at a time, so every reader that mattered was clean and the
dependency was invisible.

The path that answers a question is the one that did not go through that door. It captured the
screen, handed it over with the colour attached, and `\u{1b}[38;5;153m` sat in the column the
parser looks for a selection caret in. So no row was an option, so there were no options, so there
was no menu — and a key pressed from a phone was written into the terminal and then abandoned,
because the app could no longer see the question it had just answered. Nothing failed: the request
returned success, the keystroke really had been delivered, and the picker sat there.

It only ever showed up where the digit alone was not enough. An ordinary row answers on the digit,
so those taps worked and this stayed invisible; a row with a `preview` panel needs the Return that
follows, and that Return is what was never sent.

The colour now comes off inside the parser, where every caller gets it, and the answering path
writes down what it decided — what it read, where the caret was, whether it confirmed — because
this failure was silent in the one place with no record of itself.

### Fixed: an answer sent from a phone stopped landing when the question carried a preview

Tapping an option on a phone sends that row's digit into the picker, and Claude Code's
`AskUserQuestion` takes a digit two different ways. A plain row reads it as the answer and puts the
next question up. A row drawn beside a `preview` panel reads it as a move: the highlight walks over
and stays there, under a hint that says `Enter to select`. One call draws both shapes, so a single
set of questions can hold one of each.

Clawdline sends the Return that commits an answer only when it can see the picker did *not* move on
by itself — and it read "this question is one of a set" as proof that it had. On a preview question
that is exactly backwards: the highlight had moved, nothing had been committed, and the one thing
withheld was the Return the picker was waiting for. Measured 2026-09-05 against Claude Code
v2.1.261.

**The six unanswered minutes that found this were not this.** They belonged to the entry below,
which was hiding underneath it: the answering path could not read that screen at all, so it never
reached the decision described here. This one is what would have stopped the next tap.

What decides it now is the picker's own tab bar, which moves for exactly one of the two: a digit
that answered ticks its question off, and a digit that only moved a highlight leaves every box as it
was. A question whose box ticked is still left alone, which is what stops a stray Return from
answering a question nobody has read.

### Added: this app can tell you it has been left behind

An installed copy had no way of finding out that a newer one exists. It arrives as a zip from a
release page, it is not in a store, and cutting a release touches no machine that already has one —
so whoever installed it stayed on that version until they happened to look. That failure is quiet
in the worst way: what this reads is another program's screen, so an old build does not break
loudly, it stops reading sessions correctly and says nothing, which looks exactly like the sessions
being idle.

The menu bar now carries one row when there is a newer release, naming it and the build in front of
you, and opening the page it is about. Nothing is downloaded and nothing is replaced: what to do
about a new version is a decision, and installing one closes and reopens the app you are using.
No dependency was taken on for this — no Sparkle, no appcast, no package manager. It is `URLSession`
and the same GitHub endpoint `install.sh` already reads.

It asks once a day. Ten launches in an afternoon cost one request, and a failure is retried after
an hour rather than at once, because an hour is GitHub's own window for an address that has used up
its sixty unauthenticated requests.

**A check that could not be made does not look like a check that found nothing.** Rate-limited,
refused with a status, unreachable, answered with something it could not read, and unable to say
which version it is are five different outcomes, none of which can be spelled "you are up to date".
The menu stays quiet for all of them — a menu that reports its own failures grows a permanent row —
and the reason is in `~/Library/Logs/Clawdline.log` and in
`~/Library/Application Support/Clawdline/update-check.json`, in words.

### Changed: a compatibility note about a newer assistant now has something to act on

Clawdline said nothing when the Claude Code in front of it was *newer* than the one this build was
checked against, and the reason was written down: that assistant updates itself, this does not, and
a line nobody can act on becomes a line nobody reads. The first half of that has stopped being
true.

So the case splits. When this is already the newest Clawdline there is, it stays silent — now
because there is provably nothing to be done rather than because there was nothing to say. When an
assistant has moved past what this was checked against **and** a release is waiting, you get one
line naming all three: what is installed, what this was built against, and the version that catches
up with it. It needs both halves at once, so it cannot become a weekly notice.

### Fixed: a notification could be tapped and leave you exactly where you were

Tapping a push is supposed to open the session it is about. The address it carries is
`/#session=<id>`, and the id went into it as-is. The sessions this app watches under tmux are
panes, so the id is `%141` — and the page at the other end reads that fragment with
`decodeURIComponent`, which does not object: `%14` is a complete escape, so what it went looking
for was the control character U+0014 followed by a `1`. No session has ever had that id. The
lookup found nothing, the first full list quietly let go of the request, and the tap landed on the
session list with nothing on screen to say why.

**It only ever happened on a phone.** iTerm2's ids are `w0t0p0:<UUID>` with no per-cent in them, so
every fixture here passed and every tap on a desk worked; a push naming a tmux pane was the one
case, and a push is what a phone gets.

The address is now written in one place, with the unreserved characters of RFC 3986 rather than the
fragment set — which allows `&`, `=` and `#`, the three characters that would cut the fragment in
half. Notifications already sitting on a phone still work: reading one back tries the raw text as a
second candidate, but **only where the decoded form is impossible**, which is what a control
character is. A link written since the fix decodes to a real id, and there the raw text names a
*different* real session — `%2` is spelled `%252` now, and on a Mac that has reached pane `%252`
the old reading would silently open a stranger. The narrow thing not rescued is an old link naming
`%20`–`%39`, which decodes to something printable: that stays where this was yesterday, "does not
route", never "routes somewhere else".

### Added: a picture on a phone that can be looked at closely

An image in a transcript opened to fit the screen and that was the end of it. On a phone that is
often not enough to read one — a screenshot of a diff, a chart, a stack trace someone pasted — and
the honest workaround was to go and find the Mac.

Four ways in, all of them the ones a hand already tries: two fingers, one finger twice, one finger
dragged, and a wheel. A trackpad pinch arrives as a wheel event with `ctrlKey` set and is read at
its own rate. The frame takes `touch-action: none`, without which Safari claims the two-finger
gesture before the first move arrives. The page-wide `user-scalable=no` is untouched and the suite
asserts it stays that way — this is the picture's own zoom, deliberately, not the browser's.

**A press that moved the picture no longer closes the preview.** Dragging on the backdrop's padding
is delivered as a click on the dialog, which is exactly what the close handler was testing for. The
first fix asked whether the picture had moved and was wrong in a way only a real browser shows: a
16:9 picture a little over the fit in a 4:3 frame has no vertical travel at all, so a drag straight
up moved nothing, and the release on the padding arrived as an ordinary press. What is asked now
is travel rather than effect, from the moment a press begins rather than the moment a drag does,
with a tap's own slop — so a sweep across a picture that is not zoomed still is not a press on
what is behind it, and a hand that is not perfectly still still closes the preview.

### Added: anything on this Mac slow enough to wonder about can draw its own bar

Something started in one tab was invisible from every other one. A test run, a build, an import, a
long encode — the only report was scrolling past in a terminal nobody was looking at, and the
question left over was the one nobody could answer: *how much longer?*

There is a seventh project status file for it, `run-<key>.json` beside the six this app already
reads. Whatever is taking the time writes it, and the bar draws it: a label, a phase, and a bar
from elapsed time against typical time, with a tick or a cross at the end. `./test.sh` and
`./build.sh` write it at every phase boundary they already had, so a suite run is a bar on the Mac
footer and a chip on the page while it happens. A local run takes the chip ahead of a deploy that
is also running, because it is the thing holding up the person at this keyboard.

Two deliberate differences from the deploy file it is shaped after. **It is keyed by working
directory rather than by git remote**, so two worktrees of one repository do not overwrite each
other's progress, which is the ordinary case here. And **a `running` row goes quiet on its own**
after `stale_after` — nothing polls these files, so without a ceiling a run somebody `kill -9`'d
spins in the bar for good. The ceiling lives in the reader rather than in the producer, so every
reader inherits it, including the ones nobody has written yet.

**The format stays open — anything may write this file — and there is now a helper, so that nothing
else has to get the hard half right.** The JSON is the easy half; the shell around it fails in the
direction that looks like success, and two agents each holding the whole written contract got it
wrong on the first attempt on this Mac's `bash 3.2.57`. Measured, one script each: an `EXIT` trap
on its own runs with `$?` of `0` for a script that was killed, so the last thing it writes is `ok`;
`set -e` without `set -E` skips `ERR` entirely for a failure inside a function; and a signal
handler that returns lets the script carry on to its own `exit 0`. `clawdline-progress` is those
traps written once and shipped inside the app bundle. The wrapper form is the whole of what
somebody who just wants a bar has to know — `clawdline-progress run --label lint --typical 120 --
./scripts/lint.sh` — because the command it wraps is not edited and knows nothing about any of
this, and the state the row ends on is that command's own exit status rather than a line somebody
remembered to write. Sourcing the helper instead gives a script `progress_start` and
`progress_phase`, for phases it already knows the names of. Neither form invents a typical time it
was not given — `./build.sh` writes none, because nobody has ever measured a build.

### Fixed: a project with no CI wore a red cross

`{"state":"none"}` means *there is nothing to say*: no workflow, no run on this branch yet, `gh`
not installed. The rule that an unrecognised state must draw nothing has been written down for a
long time, precisely so that a reader which has not heard of `none` does not draw a cross for a
project that simply has no CI — a red mark that is always wrong. The run file honoured it. The
deploy and health readers did not. Counted on this Mac on 2026-09-05: fifteen deploy status files,
**twelve of them exactly `{"state":"none"}`**, each drawing a red ✗ in the footer at that moment.

Both readers have an allow-list now, so a state nobody recognises parses to nothing rather than to
a row that will be drawn wrongly. Finding it turned up the mirror image underneath: two different
tools write health files here and one of them says `online` where the page documents `ok`, so the
footer had been drawing a red dot for a site that was answering perfectly well. Both surfaces read
one answer now instead of two.

### Fixed: bringing a session's tab to the front raised whichever tab you last looked at

Under `tmux -CC`, asking for a session — from the phone, from the bar, from a notification — moved
tmux's own selection and brought iTerm2 forward, and iTerm2 arrived on whatever it had been showing
before. So the thing you asked for was behind the thing you were already looking at, and it was
worse than doing nothing, because the window came forward and looked like an answer.

Two beliefs held that shape in place and both were measured false against tmux 3.6a on this Mac:
that iTerm2 follows tmux's window selection (it does not — tmux's active window moved and iTerm2's
did not, and tmux really did send the notification), and that iTerm2 publishes no mapping from a
tmux pane to one of its tabs (its scripting dictionary carries a per-session variable naming the
pane). So the tab is now selected outright, and when the row cannot be found the application alone
is raised — a window in front of the wrong tab, rather than a press that does nothing.

Selecting the wrong tab takes somebody's keyboard away just as surely as raising the wrong
application does, so it is refused twice before it happens: tmux's own client list has to say a
control-mode client is attached to that pane's session and that client's terminal is one of
iTerm2's, and iTerm2 has to agree that the row it is about to select is a tmux mirror rather than
an ordinary tab.

### Added: a page for the work that got finished and never merged

The web interface was one page with things laid over it — Usage hid the app from inside its own
module, Settings was a sheet behind the wordmark — so nothing decided which screen you were on and
nothing could be linked to. The wordmark now opens a drawer that names the screens, `#page=usage`
in a fresh tab arrives on Usage, and a new page costs a row in that drawer instead of its own way
of appearing and its own way of putting the page back.

**The first screen added that way has one subject, and it is work that was delivered and never
landed.** `git worktree list` has fifty-eight answers on this Mac and almost none of them are the
one anybody wants; the question in front of a project is narrower — which of these checkouts
actually finished something, and did it reach the branch. Of this repository's seventy-nine
worktrees carrying an accepted piece of work, **thirty-eight finished and have no landing record at
all**. That is the first number anybody has had for *it gets done and nobody merges it*, so it is
the open block at the top with the branch each one is on, and the other outcomes are folded
underneath it.

Every rung is a stored fact rather than a guess: landed, delivered, still active, or abandoned,
each resting on a row that says so, with liveness asked of the registry rather than inferred from
an age — a task the registry does not hold is certainly not running, and that is the direction an
absence can be trusted in. **An empty answer and an answer that never arrived are drawn
differently**, which is the half of this most pages throw away: every reply carries the count of
rows behind it and that is on screen whenever the read succeeded, a project this Mac has no record
of is a `404` rather than an empty list, two projects sharing a final name are a `409` rather than
whichever the dictionary handed over first, and checkouts that resolve to no project are counted in
a block that says so instead of being dropped.

`GET /v1/orchestrator/usage/project-worktrees` is the read, and it publishes no branch on purpose —
the ledger stores none, and a field present only for recent tasks would be read as evidence about
an old one. Thirty-one new strings, in all fourteen languages. Not on the phone relay: every read a
paired viewer may ask for names a session, and that session is also the channel the answer comes
back on; this one's subject is a project and its answer is about sessions that are mostly over.

### Added: the documents this Mac writes, over HTTP

Three long documents were produced here in one day and none of them could be opened from anywhere
but the Mac: working notes under a project's own `artifacts/`, and the deliverables every
dispatched task writes. The route that served project files could not reach any of them and was
never going to — it took a slot rather than a path, and there was no string a caller could send
that named a third file.

That is exactly what made it safe, so the replacement draws a boundary instead of widening a slot.
`GET /v1/sessions/:id/documents` lists what is there and gives each one an address underneath it.
Two roots, both computed by the server, and a caller's string may only choose *inside* one: the
project's own `artifacts` — with that one symlink followed, because somebody put it there to say
where their documents are kept — and a task's `artifacts`, whose symlinks are not followed, because
the child writing there is the party the boundary bounds. A task's `task.json` and `result.json`
sit one level above its root and are therefore outside it, not filtered out of it.

A hostile path gets nowhere. Absolute paths, empty segments and any segment starting with `.` are
refused before the filesystem is touched; decoding happens after the split and the joined path is
re-split and re-checked, so `..%2F..` dies by the same clause that refuses `../..`. What survives
must resolve under the root, be a regular file, carry one of `md`, `markdown` or `txt`, and be at
most 2 MiB. The listing is built by the same resolver the read uses, so it cannot offer a row the
read then refuses.

**This is a route and not yet a screen.** There is no document view on the page and no row in the
links sheet: a control the client cannot draw an answer for is a promise rather than a feature.
What exists today is something you can call with your token.

### Fixed: a conversation opened with a pasted image was called `Image #1`

Claude Code writes a conversation's name itself, from the material it has, and never revises it. A
conversation that began with nothing but a pasted screenshot gave it nothing to name from, so it
wrote `Image #1` — and this app showed that faithfully, because it had no way to notice the name
said nothing.

It notices now, and falls back to naming the conversation itself. **The judgement is made about the
input, never about the title**: looking for the word *image* in a name would take a real
conversation about images off somebody's screen, so the question asked is the one Claude Code was
answering — did the material it had describe the work? Measured across every transcript on this Mac
— 2,152 with an opening message, 697 of them titled — all five whose opening was empty once
attachment markers came off got a placeholder name, and not one titled conversation with actual
prose in its opening did.

**And the sheet you pick a conversation back up from stopped disagreeing with the list about what
it is called.** That sheet read the transcript's own two names and nothing this app knew, so a
conversation you had renamed here was one thing in the session list and another in the resume
sheet, at the same moment, on the same screen. It reads the same ladder as everywhere else now, as
far as a finished conversation can answer it.

### Changed: a handoff has to say who sent it

Handing a line of work to another session took an optional, free-form field for the sender, which
meant it could not say who sent it. On 2026-09-04 this machine's coordinator sent one naming
nobody: the succession sequence was skipped entirely, and a person had to close the sending tab by
hand before the receiving session could take over.

`POST /v1/orchestrator/handoffs` now requires that field and resolves it to exactly one live
session, with nine named refusals in place of one shared error — so a handoff that cannot be
attributed says which of the nine things went wrong instead of a blank *bad request*. A sender that
is the current coordinator is refused outright and told what to do instead, carrying the fields the
succession request itself takes; one flag waives that single refusal and nothing else.

**Being told "offline" is not proof that a process is gone**, and reading it as proof replayed the
incident this was written to prevent: a status of offline means only that this reading matched no
row on every field, and one of those fields is missing for a perfectly live session whose
transcript could not be located that round. So a coordinator that was alive — same terminal, same
pid — read as offline and its plain handoff was accepted. What is required now is positive absence.
A row still holding the bound terminal means the machine has something there it could not match,
which is *unknown*, not permission.

The other way in was a truthy back door: JSON `1` and JSON `true` arrive as the same kind of
object, so a client serialising booleans as `0` and `1` could waive that refusal by accident while
three separate surfaces promised it needed exactly `true`. It is asked as a type now, the way this
tree already separates the two elsewhere.

### Added: a session in any repository can report that its turn is delivered

Marking a finished turn as delivered — the check on the session's card that means *done, awaiting
approval* — has been a route for a long time, and the guide has carried it in full for just as
long. **What was missing was a reason to go and look.** A session working in a different project
finished, was told to make its work show up here, and probed four addresses that do not exist, read
another session's briefing, and concluded that publishing was something only a dispatched child
could do. Wrong, and supported by every piece of evidence it could reach.

It had seen the skill and decided it was not for it, which was fair: every clause of that
description was about handing work *out* — dispatch, handoff, message — and a session that had just
*finished* something matched none of them. So the shipped skill files now trigger on having finished as
well as on handing work out, including the phrasing that does not require knowing the feature exists
first (*show this session as finished in Clawdline*), and they say plainly that this applies in any
repository Clawdline watches rather than only in its own.

**The noun was the direct cause of the worst of it.** This project already labels a project's
status artifact `milestone`, and repositories keep `*_MILESTONES.md` files. Told its milestone had
not been updated, that session edited the markdown document and reported it done — twice. Every
piece of copy now says *delivery receipt* and keeps the old word only as something to search for.

And the instruction goes where a receiver actually reads rather than only where a sender does: into
the handoff document itself, which is the one thing a session picking up work in somebody else's
project is guaranteed to read, alongside the naming step that was already written there for the
same reason. For a session that is neither dispatched nor picking up a handoff, both READMEs now
point at the optional global instruction file, with the text to paste.

### Changed: Homebrew is no longer one of the documented ways to install this

The cask was the only thing that could have told somebody a newer Clawdline had shipped, and that
was the whole of its case. The app checks for itself now, so what the cask is left with — a clean
uninstall, and being the idiom of the terminal — is convenience rather than capability. Against
that, it is updated by hand, the release script has no `brew` in it, and **it had been serving
0.5.0 for seventeen days and 882 commits** while the README listed it as an equal path. Its own
notes still told people to strip quarantine from a build that is notarized and does not need it.

Nothing is withdrawn: the tap still exists and still serves what it has to anybody who already
tapped it, and the uninstall instructions stay for exactly those people. What changed is that the
README no longer sends somebody new down a path that was two and a half weeks behind.

### Changed: automatic names can use the assistant you choose

The automatic-naming switch is now one three-way choice: off, Codex or Claude Code. Existing
configs that enabled `codex_auto_name` still select Codex, while `auto_name_assistant` records a
different choice without making the two controls disagree. Both engines run one tool-free,
low-effort, non-persisted turn and spend the selected assistant's usage.

Clawdline also keeps observing a native Codex name briefly instead of treating the first non-empty
value as final. Codex writes the opening request as a provisional thread name and replaces it with
its concise title a few seconds later; landing in that window no longer leaves a path or the whole
opening request stuck in the Session list.

### Added: a session can be called what you called it

What a session was called was decided entirely by machines: the task title this app pinned on a
tab it opened, the name Codex keeps on a thread, or whatever Claude Code last wrote into the
terminal title. `/rename` reaches that last one and nothing else, and on a tab opened for a
dispatch or a handoff it could not be seen at all, because the task title sat in front of it.

The title on the Session info card is something you can press now. Type a name, send it, and that
is what the list, the panel and every notification call it. Empty the field and the automatic name
comes back. `POST /v1/sessions/:id/title` is the same thing over HTTP, behind the same switch as
sending, and an empty title clears it there too.

A name belongs to the conversation rather than to the tab, which is narrower than it sounds and is
the point: leaving `claude` and starting it again in the same window keeps the window, and the
next conversation gets its own automatic name instead of inheriting the one you chose for the
last one.

`/rename` in the terminal is the same person speaking, so the newer of the two wins: name a session
here, rename it there, and the terminal's name is what shows. What is compared is what the
transcript's last `/rename` said at the moment you chose the name — a rename that happened before
that changes nothing, and a rename after it takes over.

Downstream is told when it can be, and the answer says which happened. Codex takes the name on its
thread straight away. Claude is sent `/rename` only when it is idle and not showing a menu — a
slash command typed into a running turn interrupts it, and one typed into a question answers the
question — so a session that is busy keeps the name here and says the downstream name did not
change, rather than implying it will catch up later.
### Added: a tab that never opened is retried by the broker, not rewritten by you

`POST /v1/orchestrator/tasks/:id/respawn` takes the id of a task whose terminal never came up and
opens it again. The old answer was that whoever dispatched it writes the whole `task.json` out a
second time under a fresh id, because the failed id is finished and re-sending it just hands back
the record of the failure. On the machine this was measured on that was 34 of 206 dispatches — a
sixth of everything, 33 of them Codex — all of them rewritten by hand by the session that could
least afford the words.

The broker already holds everything the original said, so it copies the file, swaps the id, mints a
fresh secret unless you send one, and dispatches the copy through the ordinary gate: same capacity,
depth, claims, quota and serialization rules, same refusals. Only a `spawn_failed` task may be
retried — a failure is an answer, a timeout had a session that read its briefing, a cancellation was
somebody's decision — and **at most two retries descend from one original**, counted across the
whole family below it rather than along any one chain, so neither a retry of a retry nor asking the
original again gets past it. The new task records where it came from, so a retried dispatch reads as
one chain in the list instead of three unrelated tasks with the same title.

### Changed: the briefing stopped teaching dispatching to the children who never dispatch

Every child session was handed the full recipe for opening children of its own: the credential, the
fields, the refusals, the machine's house rules. Measured across 206 dispatches, that was 28,323
characters — about 7,081 tokens — in every direct child's briefing, and **not one of those 206 ever
dispatched anything**. The teaching is not wrong; it was addressed to the rare session that would
use it and charged to all of them.

It was moved out into a `DISPATCHING.md` written into the task's own directory beside `CHILD.md`,
and then the second level of the tree came out and there was nobody left to write it for: nothing
this app opens may dispatch, so no such file is produced at all. A task directory briefed by an
older build has its `DISPATCHING.md` **removed** rather than merely not rewritten, because a
re-briefed child would otherwise sit there reading a recipe it is no longer allowed to follow. The
credential path, the `parent_task` rule and the `curl` are in no briefing this app writes.

`CHILD.md` also asks for something in return: one `/progress` note within about three minutes of
starting, saying what the session has decided to do now it has read the briefing — and says why, so
a session that knows the reason will actually send it. It is the only thing that lets a wrong
direction be stopped at minute three instead of minute twenty-six. The two dearest cancelled tasks
on that same record had burned 18.5M and 16.5M tokens before anybody could tell.

### Added: a dispatch that never said what it writes is told so

60.7% of the dispatches measured here declared no `claims` at all — no list of the files the task
will write. The reply now carries a `claims_missing` warning when the field is absent. It is a
warning and never a refusal, and `"claims": []` is a positive declaration that the task writes
nothing and is not warned about: the difference between "I write nothing" and "I did not say" is the
whole point of the thing.

Declaring costs about twenty tokens. A collision costs a whole task, which on this record runs from
three to eighteen million.

### Fixed: a fresh install started with rules nobody had read for months

`~/.config/clawdline/dispatch-policy.md` — what a machine says about how work should be handed out —
is created on a machine that has none. What it was created from was a copy of the rules pasted into
the source code, which had gone stale against the file this project actually edits and ships. So a
new install began life with an old draft, and the only sign was that its children were following
rules nobody recognised.

The starting rules are now read from the copy in the app bundle, which is the same document that
ships. If it cannot be read there are no house rules, exactly as an empty policy file has always
meant, and nothing is written — a machine that once failed to read the resource would otherwise have
kept an empty policy for good, because this file is never overwritten once it exists. Machines that
already have a policy of their own are untouched, as they always were.

### Added: schedules you can make, instead of only read

A schedule — "every weekday at nine, open a session in this project and tell it to do this" —
could until now only be made by writing a JSON file by hand. There is a + beside the Schedules
list now, and a form behind it with a field for each thing the file holds: a title, a time, which
days, which project, which assistant, the first message, and behind a fold the things with
working defaults.

You can also say it. The microphone that opens a session hears a schedule too — "every weekday at
nine, run the tests in clawdline" — and hands what it worked out to the same form, leaving blank
whatever it could not. That is the point of the form existing first: the parts a machine could not
read are the parts you are shown. A confident draft still does not create anything on its own,
because opening a session now and arranging one for every morning are not the same risk.

What the page sends back is the id the Mac put on the project row, never a path, so a schedule
cannot name a directory this device was never shown. And a schedule never runs for a time before
it was made: it remembers when it was created, so arranging tomorrow's nine o'clock at one in the
afternoon does nothing at all until tomorrow at nine.

One thing worth stating plainly, because it moves a line this project drew on purpose: a paired
phone can now arrange work that runs later, with nobody watching. It goes through the same gate
as dictation — sending has to be switched on, and the device has to be allowed to send.


### Fixed: opening one session card no longer stops everything else on the phone

The remote server read, decided and answered every request on one queue. That is what made its
state safe to touch without a lock, and three routes had already been moved off it for being slow
— but the three slowest ordinary readings were still on it. Measured on this Mac: a session info
card takes 0.531s to gather, because it runs `lsof`, reads a whole transcript, asks iTerm2 for the
visible screen over an Apple event, and shells out to `git status`. Behind five of those, a health
check that takes 0.001s on its own took **3.143 seconds**.

It was worse than a slow page, because the event stream shares that queue: while a card was being
gathered, session states stopped updating, task rows stopped moving, and the heartbeat stopped —
and a stream that goes quiet is a phone that decides the Mac has died.

Those three now read on a queue of their own, with the gates still where the state they read
lives. The same health check behind the same five cards is now **0.0008s**. Four cards opened at
once still take about two seconds between them — the expensive part is an Apple event, and iTerm2
serves one at a time, so a wider queue would only move the line somewhere the app cannot see it.
That cost is paid by the person who opened four cards, which is the right person.

Opening a card can now come back `429 busy` when eight slow readings are already in hand. Reading
a transcript never can: a page refetches it about once a second while a session works, and a
refusal there used to blank the conversation on screen and print the server's English sentence to
whoever was reading it — in whatever language they had not chosen. A refetch that fails now keeps
what is already on screen, which is what the code always claimed it did.

### Fixed: a dispatched session that was working perfectly was reported as never having started

Handing work to another session identified the new session by looking up its terminal. That lookup
is keyed on the tty and says nothing about age, so a child opened in a tab that had held an earlier
conversation inherited **that** conversation's id. The app then looked for its own briefing in
somebody else's transcript, never found it, and four minutes later filed the task as
`spawn_failed` — with a summary blaming a session that never reached a prompt, while the session
in question sat there working on exactly what it had been asked to do.

Three of these are on disk, the worst pointing at a transcript from three and a half hours
earlier. Identity now refuses anything older than the spawn it belongs to, and the transcript
search refuses a file whose contents stopped changing before that spawn. What proves a briefing
arrived is unchanged — it is still the marker in the assistant's own record, never a guess from
the screen. What changed is which file gets searched for it.

### Fixed: a task handed to Codex waited in the composer for somebody to press Return

The first message arrived in the input box and stopped there. Every dispatched Codex session
needed a person to press Return before it would start, which for a feature whose whole point is
that nobody is watching that tab is the same as not working.

The cause could not be reproduced on demand. Measured against a real iTerm2 across 40-byte,
1KB and 5KB messages, both assistants, warm and cold-started, at gaps from 60ms to 600ms, the
Return landed every single time — and yet three dispatches in a row were found waiting. What could
be reproduced, every time it was needed, was the cure: one more Return, and it goes.

So this stops trying to out-wait the problem and looks instead. After sending the Return it reads
the composer — only the composer, since an assistant that did accept the message often echoes it
back above the prompt, where a match would mean the opposite — and sends another only while the
text is demonstrably still sitting there. The ordinary case costs a quarter of a second, submits
once, and sends no second Return.

### Changed: the tick is a tick, and the end of the list says what is not in it

A project's whole history is on the phone before the list is drawn — up to two hundred
conversations in one reply — so it is all on screen and scrolls, with the filter box searching
every row of it. (A *Show 25 more* button lived here for an afternoon. It was asking somebody to
authorise work that had already been done: redrawing all two hundred rows costs under nine
milliseconds, which is a keystroke in the filter box inside one frame.) Where the Mac itself
stopped short of the end, the reply says so and the last line of the list says so — including
while filtering, which is the one moment the filter cannot be trusted to have looked everywhere.

And the tick in the switch above it was two CSS boxes: a rounded pseudo-element, and a checkmark
made from two borders of an empty box turned forty-five degrees and pulled back over it by a
percentage of its own size. That trick's two strokes are the same length and meet at a right
angle, where a real checkmark's do neither, and where it lands depends on rounding a translate
against a rotate. It read as leaning. It is one small SVG now — box and mark on one grid, at the
coordinates they were drawn at.

### Fixed: the list of conversations to pick back up was mostly not conversations

The first version of it listed what was in the project folder, newest first, capped at forty. In
this repository's own folder that is a hundred and one transcripts — of which **fifty-two were
sessions Clawdline itself dispatched** to do one task, and **eleven were `claude -p` one-shots**
sitting there under names like `Test`, `Hello` and `What is 2+2?`. Thirty-five were the work.

So the cap fell in the middle of the plumbing. Everything real older than the fortieth row was
invisible — and stayed invisible when you typed, because a filter box can only narrow what was
loaded. The two complaints, *there is test rubbish in here* and *the ones I want are missing*, were
one bug.

Both kinds are now left out, and neither judgement is a guess about what is in the file: a
dispatched session's first turn begins with the briefing line this app wrote into it, and a `-p`
run is marked by Claude Code as `entrypoint: sdk-cli` / `promptSource: sdk`. Across every project
on the machine this was measured on — three hundred and thirteen transcripts — that second pair
occurred thirty-one times and every one of them was a probe. The cap went to two hundred, which
with the rubbish gone is no longer where the list ends.

The briefing test is a **prefix on the first turn** rather than a search of the file. A
conversation held *about* this feature quotes that line repeatedly; under the looser test it
filtered itself out of the list it was being read in.

### Fixed: naming forty conversations took nine seconds

Reading a transcript's title reads the whole file when it has to look for a rename made before the
tail. It did that by turning the file into a string and splitting it into lines — two hundred and
forty megabytes and a hundred thousand throwaway substrings, to find a key that two transcripts of
a hundred and one even have. It is now a byte search over a mapped file, backwards, decoding only
the line that matched: the same answer, **nine seconds down to one and a half**, and nothing after
the first read of a file until that file changes. A transcript smaller than the tail window is not
scanned twice at all.

### Added: say what to start, and the Mac works out where

There is a microphone beside the plus in the session list now. Press it and say what you want
done — *go run the tests in clawdline and paste anything red* — and the sentence comes back on
screen as text you can correct. Press Start and the Mac plans from it: which project you meant,
which assistant, and what the first message should say.

That draft is shown before anything is opened, and the sentence in it stays editable, because it
is not what you said — it is what a model wrote out of what you said, and it is the thing that
will actually be typed into the agent. A planner asked to open a session somewhere that is not
one of your projects will refuse the project and still write the request into that sentence,
which is exactly why it is put in front of you rather than used.

When the planner is confident it carries on and opens the session itself. When it is not — no
project named, or one that does not exist here — it stops and asks, with the list underneath.
Either way Cancel stays live the whole time: reading the draft and deciding against it works
right up until the message lands.

It plans with Claude Code when the Mac has it and falls back to Codex when it does not. Nothing
new is opened up: the page starts the session and sends the message with the two permissions a
paired device already had, rather than a route that would do both at once.


### Fixed: a hole in the middle of every sheet with chips in it, on a phone

The session list's row class is `row`, and the phone breakpoint gives it thirteen points of padding
and a margin. That selector was never scoped to the list, so it also landed on the chip rows inside
sheets — *Start with*, the Transcript order in Settings, the command sheet's assistant row — each of
which quietly carried twenty-six points of padding it was never meant to have. One such row looked
like generous spacing; two of them stacked looked like something had failed to draw.

### Added: picking a conversation back up from the phone

*Start a session* could only ever begin a new one. The conversation you were in an hour ago — the
one with the context, the plan and the half-finished thing in it — was reachable from the Mac and
nowhere else, and the honest workaround from a phone was to start something fresh and re-explain.

The sheet now has a tick box. With it on, pressing a project shows what Claude Code has already
recorded there instead of starting anything: its conversations, newest first, **under the names it
gave them**, with a box to find one by typing part of the name. Press a row and a tab opens in that
project running `claude --resume` on that conversation.

The names are read, never invented — a rename you typed beats the title Claude Code wrote, and a
transcript with neither is listed by the first thing a person typed into it. A conversation with
nothing in it at all is left out rather than shown as an untitled row somebody has to guess at.

A conversation something is writing to **right now** says so and is not resumed: two processes on
one transcript is a corrupted record, so that row goes to the session instead, which is what
pressing it meant anyway.

Claude Code only. Codex keeps the same conversations somewhere else and its thread names live in a
process this list will not start once per listing, so the tick box says so and shuts rather than
offering something it cannot fill.

Behind it, two routes with the same shape as the one that starts a session:
`GET /v1/places/:id/sessions` to read the list, and `POST /v1/places/:id/resume/:session` to open
one. Neither reads a request body. The conversation is a path segment checked twice before it
becomes part of a command line — once for being a lowercase UUID, once for being one this Mac just
listed for that directory — so an id nobody was handed is a `404` rather than a string on a command
line. `docs/api.md` said for a year that if `claude --resume` were ever wanted it would be a second
named action with its own literal and not a field on the first one. It is.

### Fixed: every reading could stop, minutes after the app started

The change that stopped a subprocess wait from running the app underneath itself did it by waiting
on a thread borrowed from the global pool — which is where the caller usually already is. That is a
deadlock as soon as the pool is full: the waiter holds a thread the block it is waiting for needs.
The one place that reads every terminal runs on that pool and shells out from inside it, so once
it happened nothing was read again. Sessions kept whatever state they were last seen in, a phone or
a browser was sent a snapshot on connect and then nothing at all, and a tab opened or closed after
that never appeared or disappeared.

The wait now happens on a thread of its own, which the pool cannot starve, and the test fills the
pool before asking for one so that it fails if this is ever written that way again.

### Added: a session with a command still running no longer reads as finished

`Bash` with `run_in_background` starts something that outlives the turn that started it — a build,
a dev server, a test suite. Claude Code says so once, on the line where the turn ends: *Cooked for
1h 25m 13s · 1 shell still running*. Then it draws an ordinary prompt and says nothing more about
it, for however long the command takes.

Everything in this app reads that prompt as what it looks like. The session list drew the row with
nothing after its title, the phone drew it the same way, and the fleet count called it quiet — so
the one session that still had work in flight was indistinguishable from the four that were done.
That is the wrong answer in the direction that costs something: you close the laptop on a build.

The row now says `1 shell running`, on the Mac and on the phone, the header can no longer call a
fleet "all quiet" over a build, and the strip above the transcript lists the commands next to the
background agents with the last line each one printed. It is worked out from what Claude Code
already writes down — the output file a command prints into, which gets `[exited with code 0]` under
it when a background one ends, and the line in the transcript that says an id was backgrounded in
the first place. Both are needed: a foreground command that somebody interrupted leaves its file
behind looking exactly like a build still going, which is how the first version of this spent an
afternoon reporting a cancelled `curl` as work in flight. Nothing has to be installed and nothing
has to be restarted. `GET /v1/sessions` carries it as `shells`; see
[`docs/api.md`](docs/api.md#the-session-object).

**And the rows open.** A background command has no conversation to read — it was given its words
when it was started and is not listening for more — so what it has to show is the file it is
printing into, and pressing its row puts that in the transcript's space, re-read while it is open,
pinned to the newest line, and stopping when the command does. The row and the panel both lead
with **the command itself**, joined from the call that started it, because nine random characters
of task id are Claude Code's word for a command and not anybody else's.
`GET /v1/sessions/:id/shells/:shellId` is the same thing for anything else that wants it.

**And they can be stopped.** Claude Code has always let the person at the keyboard do this —
`/tasks` — and a phone could only watch. The panel now has a Stop, behind the two gates that
ending a session is behind: a device allowed to write, and a second press over a sheet naming the
command line rather than its id. What makes it safe enough to offer at all is that the app
identifies the process before it signals one: the id has to be a command this session announced,
something has to still be holding its output file open, and that holder has to be a child of this
session's Claude Code. The signal goes to that process's group — a one-liner is a shell and
whatever it is running — and never to Claude Code's own. If any of it cannot be established,
`409` says so and nothing is signalled. `SIGTERM`, then `SIGKILL` five seconds later only if it
is still there.

### Changed: reloading the web interface no longer means a second and a half of black

The page is a document, fourteen stylesheets and forty modules, and until now every one of them
was sent `no-store` — so a reload fetched all fifty-five again, and through a tunnel a request
costs the same half-second whether it returns three kilobytes or three hundred. Worse, they did
not arrive together. The browser learns about the stylesheets from the document, about `main.js`
from the document, and about the other thirty-nine modules only after `main.js` has arrived and
been parsed; then the page asked for its own words, which it could not do until all forty modules
had run, and it is deliberately blank until those land. Four round trips, in a line, in front of
a dark rectangle — measured at about 2.5 seconds, which is past the two-second fallback that
gives up and draws the interface in English.

Three changes, and none of them touches what the page does:

**Every stylesheet and module URL now carries the build in its path** — `/app/v1756100000/js/…` —
and is served with a year of `immutable` cache. The stamp is the executable's modification time,
the same one `/v1/health` reports, so a rebuilt Mac serves a document naming *different* URLs and
the old ones are simply never asked for again. There is no version of this that can hand somebody
a stale stylesheet, because the document itself stays `no-store`. A reload now asks for the
document and nothing else.

**The interface's words are written into the document** instead of fetched from `/v1/strings`.
That request was the worst-placed one on the page: last to be sent, first thing the paint waits
for. It is now a line of script in the head, and the fallback fetch stays for the dev server and
for a copy opened off a disk.

**Every module is named in the head**, as `modulepreload`, so all forty are asked for at once
instead of thirty-nine of them a round trip behind `main.js`. The list is read out of the bundle
rather than copied from `main.js`'s imports, so there is still only one manifest.

The file on disk is unchanged by all this — the two slots are HTML comments, and a page served by
`tools/web-serve.py` or opened as `file://` still works exactly as before.

### Fixed: shelling out could let the app re-enter itself

Waiting for a subprocess is supposed to be the most boring thing a program does. On macOS it is
not: `waitUntilExit()` polls the run loop while it waits, so on the main thread every timer and
every queued block runs *inside* the wait. Any function here that shelled out was therefore a
function that could be re-entered halfway through, at a point nobody writing it had to think
about.

That is how one walk of the dispatched-task list came to start inside another one. The outer walk
typed a briefing into a terminal through `osascript`, the timer fired during that wait, and the
second walk carried on from a copy of a task the first was about to advance — which is what
reported a task as failed while the child it opened was doing the work and finished it.

Every wait for a subprocess now happens where a run loop turning costs nothing, and the twelve
places that shell out — for git status, the assistant versions, tmux, the terminal, dev-stack
commands, transcription — go through it. Measured before the change, a one-second wait on the
main thread let a timer fire five times; after it, none.

### Changed: a session waiting for you says so itself, instead of being caught at it

Until now, "this session is waiting for an answer" was something Clawdline worked out by looking:
it captured the terminal and recognised the shape of a menu — numbered options, a caret on one of
them. That works, it works on sessions that were open before this app existed, and it has two
costs. It only knows what it has looked at, and away from the panel it looks once every twenty
seconds. And a question drawn in a shape it does not recognise is a question it never reports.

Claude Code has been writing the answer down the whole time. Every session keeps a small file
about itself under `~/.claude/sessions/`, and the status in it — idle, busy, **waiting** — is
rewritten the moment it stops being true. Clawdline now reads those files. **Nothing to install
and nothing to restart**: unlike the hooks, the files are already there, for every session already
open, whether or not you ever let this app near your `settings.json`.

What changes in front of you. A session that stops for a permission dialog, an MCP server's
question or a sandbox request is marked as waiting straight away, rather than when its dialog
happens to be recognised — including the ones drawn in shapes the screen reader has never been
able to tell apart from ordinary output. A session that has just been given work looks busy in the
two seconds before it draws its first line, instead of looking idle. A spinner Claude Code forgot
to erase after a fast turn no longer keeps a finished session looking busy. And ⌘J finds the right
conversation without matching on tab titles and timestamps, because the file names it.

The screen still has the last word on one thing, deliberately: a menu actually recognised on the
terminal is never overwritten by a session that says it is merely busy. The file can be a beat
behind a dialog that has just been drawn, and of the two ways to be wrong for that beat, only one
hides the row you have to act on.

Everything here degrades to exactly what this app did before, and it does so quietly. An older
Claude Code that writes no such files, a backend that does not carry them, a status word this
version has never heard of, a file left behind by a session whose process is gone and whose
number has since been handed to somebody else — each of those falls back to reading the screen,
and Codex, which writes nothing of the kind, was never anywhere near this path. If you would
rather it did not read them at all, `session_registry` in `~/.config/clawdline/config.json` turns
it off in one word.

### Changed: five children belong to a session rather than to the Mac, and the tree has a bottom

How many dispatched sessions may be out at once is now counted **per session** rather than per
Mac, which is the part worth knowing if you had ever raised it: five is what one conversation may
have out, not what the machine may. Several conversations share this Mac, so over all of them
there is a ceiling nobody sets — twenty by default, four roots' worth — because the per-session
cap is the one a caller could sidestep by claiming to be somebody else. `orchestrator_max_children`
in Settings → Remote is the number you set; there is no second row under it.

**A child still opens nothing.** A second level was built during this cycle and taken out again
before it shipped, so the tree is one deep exactly as it was in 0.6.0 — and the depth is now a
constant in the code rather than a number in a file. `orchestrator_max_grandchildren` is still
sitting in every `config.json` this app has ever seeded, saying `3`, and is read by nothing: the
file is written once and never migrated, so a changed default would have reached none of them, and
a rule a hand-edit can undo is a preference rather than a rule. An unknown key is preserved when
the file is saved, so an old config keeps loading exactly as it did. A dispatch from a child is
refused with `depth_exceeded`, and `CHILD.md` tells each child plainly that it is the bottom and
that work too big for one session belongs to its own assistant's subagents — spelled out rather
than pointed at a skill, since half of these sessions are Codex and Codex has no skills.

Closing a session still takes the work it dispatched with it, deepest first, including work from a
child that had already reported; cancelling one task does the same on a smaller scale.

### Changed: a child no longer stops at every permission prompt

Dispatched sessions ran in whatever the CLI's default permission mode is, which means they stopped
and asked. **Nobody is watching a child's tab.** A session that stops for approval there does not
stop for a moment — it stops until the task times out, and afterwards it reads as work that
silently did not happen.

`orchestrator_permission` is the new setting, and `full` is the default — arrived at by trying the
narrower ones and watching each of them fail against a real task. A dispatched session's whole job
is running commands and writing files, so every stop short of the last one stops it somewhere:
`ask` on the first thing it does, which is reading its own briefing; `edits` past writing a result
but not past `cat`, `mkdir`, `curl` or `sleep`, which is most of what handing work on consists of.
What it does not widen is who may dispatch — still a `0600` file — or what a child could reach,
since it already has a shell.

It is a ceiling as well as a default. A task can name `permission_mode` and get less than the
setting; asking for more gets the setting instead, because the session doing the asking is not the
one that lives with the consequences. The record and the audit line both say what was actually
used.

**There is no `auto`, and the reason is worth knowing before you go looking for it.** Claude Code
has an `auto` mode and `--permission-mode auto` selects it — on Sonnet and on Opus. On Haiku the
same flag produces `manual`, everything asked, with no error. A word a task fills in has to mean
the same thing to every session that task can name, and one that quietly becomes the *strictest*
setting on the cheapest model is the failure nobody catches.

Two doors no setting here reaches, now written down in
[`docs/dispatch-permissions.md`](docs/dispatch-permissions.md) along with the rest of this: the
trust prompt on a directory this Mac has never run that assistant in, and Claude Code's command
screening, which refuses a `jq -n '{…}'` line on its shape alone and offers no "always allow". The
briefing a child reads was itself telling it to write files in the refused shape; it now says to
use the file tool and a heredoc.

### Fixed: a failed spawn used to leave a live assistant sitting in a tab

A `spawn_failed` that never reached briefing now closes its tab at once, where before every failed
spawn kept one. That was not free: each is a live assistant holding a slot, and the usual reason a
tab fails to reach a prompt is that too many sessions were starting at once — so the failure fed
itself. A `timeout` still keeps its screen, which is the case where something is written on it.

The window for reaching a prompt is four minutes rather than two. Two was measured against a
single session starting on a warm Mac; several starting within seconds of each other is the
ordinary case, and each of them is a real assistant cold-starting on the same machine.

### Added: a task can name its model, and this Mac can say how work should be handed out

`task.json` takes a `model` — `haiku` for a mechanical pass, `opus` for a judgement somebody will
act on — and a `plan`, the whole graph the task is one node of. The plan goes near the top of
every child's briefing, leaves included: a child that knows what its answer feeds writes
something joinable, one that does not writes a report.

`~/.config/clawdline/dispatch-policy.md` is the house rules — which assistant, which model, what
shape the graph should be, and how work gets handed out here. Its optional sibling
`dispatch-policy.local.md` beside it holds the facts that are true only on this machine, and the
app never seeds, writes or syncs that one. Both are read fresh on every dispatch, so an edit
reaches the next task rather than the next launch, and both are composed into the briefing of
**every** child rather than only the ones that could hand work on: a sentence saying what this
machine's sandbox can and cannot reach is what stops a leaf spending a turn on a call that could
never have connected. The first file arrives with opinions in it and Settings → Remote has a
button that opens it; delete the contents and the whole section disappears from every briefing.

The mechanics in there were each paid for. Stagger dispatches by 30–45 seconds, because every
child is a real assistant cold-starting on this Mac and four of them started together compete
until one misses its window. A `spawn_failed` retry needs a fresh id, since the old one is
terminal. And a child that fell back to doing the work itself has to say so, because the reader
is weighing evidence rather than just reading an answer.

The one string a dispatch now puts on a command line is the model name, and it is a name out of a
closed alphabet rather than a fragment of a command: `[a-z0-9._-]`, at most 64, never opening with
`-`. Nothing that admits is a character a shell reads. The route a paired phone can reach still
passes nothing.

### Fixed: a task could be reported as failed while its child was working

A dispatched task was marked `spawn_failed` with "the task's secret was lost before briefing",
while the child it had opened sat there doing the work and finished it. Both things were true.
The record was walked twice: one walk copied the task while it was still starting up, the other
briefed it and spent the secret, and then the first walk carried on from its copy and found the
secret gone. Nothing had gone wrong with the child; the broker had lost track of it.

A task's state can now only move forward. A copy that was taken before somebody else advanced the
record is refused rather than written, so a briefed task cannot become a starting one again and a
finished task cannot come back to life. Each walk also re-reads a task at the moment it advances
it, rather than trusting the list it started from.

The overlap that made this possible should not be reachable — every caller runs on the same
thread — so this release counts it rather than preventing it: a walk that begins while another is
still running writes a line to the audit log naming both, and a refused write does the same. The
next occurrence should say who the second walker is, which is the one thing the first occurrence
could not.

### Fixed: a slow-starting child could miss its briefing forever

Opening a child and seeing the assistant process was not the same thing as seeing somewhere to
type. A Claude Code session still starting slow MCP servers could already have a readable banner
without a spinner or a menu; that absence was mistaken for an idle prompt, so the briefing was
sent into startup, silently dropped, and marked delivered. The child stayed open at an empty
prompt until its task timed out.

The orchestrator now waits for the assistant's actual composer before typing. Sending bytes to a
terminal is no longer treated as delivery either: the task remains in startup until Claude Code's
transcript or Codex's rollout records that task's first user turn. If the named record still has
no such turn after the receipt window and the empty composer is back, the app retries under a
fixed attempt limit; once a turn is recorded, that receipt closes the retry gate before the child
can execute it twice. Trust prompts are still answered automatically, and a child that never
becomes ready still times out after two minutes.

### Fixed: ending a session from a phone could freeze every page in the house

Ending a session types the assistant's quit word and then takes the tab away. The pause between
the two was a fixed 1.2 seconds — fine when the word lands at an idle prompt, wrong the moment it
does not. A session in the middle of a tool call *queues* `/exit` and keeps working, so the tab
still had a job in it when the close arrived, and iTerm2 does what a terminal should do about
that: it puts up a sheet and asks.

A sheet is modal. The Apple event never came back, `osascript` never exited, and because every
remote request is answered on one queue, one unanswered dialog on the Mac stopped the web page,
the phone and the panel until somebody walked over and clicked a button they could not see.

The pause is now an answer rather than a guess: the session's tty is watched until the process is
actually gone, and only then does the tab go. One that will not leave on the word is asked with a
signal and then told — which is the same ending the sheet was offering, minus the waiting, and
gentler than the tab close it replaces. The ordinary case got quicker too, closing in a few
hundred milliseconds instead of sitting out the second and a bit.

Every round trip to iTerm2 now has a deadline as well, so a dialog this app did not raise cannot
wedge it either. When one is up, whatever asked says so — *iTerm2 is waiting on a dialog — answer
it on the Mac* — instead of the app going quiet.

### Fixed: the app could stop answering when the panel went away

Putting the panel away asks the dictation engine to stop, and stopping it reached for
`AVAudioEngine.inputNode` whether or not anything had ever been recorded. Reading that property
is not free — it builds the input node and allocates render resources against the audio HAL, on
the calling thread, with no timeout — so when the HAL was wedged the main thread went in and did
not come back. The app kept its window and answered nothing: not the bar, not the hotkey, not
HTTP. It now reaches for the node only when this session actually put a tap on it.

### Dictating to a session from a phone

The composer on the page took typing and pictures, which is the wrong shape for what a phone is
actually for here: answering a session in one sentence on the way out of the building. Every phone
can already dictate — and every phone's dictation hands the sentence to whoever wrote the
recogniser, which is the one thing this app spends its whole design not doing.

- **A microphone beside the send button.** Press it, talk, press it again. The recording goes to
  the Mac, the Whisper already installed there reads it, and the words arrive in the box where you
  can edit them. Nothing is sent until you send it.
- **The audio stops at your Mac.** Same binary, same model, same `voice_language` and
  `voice_vocabulary` as the bar's own dictation. The phone is not asked which language it is
  speaking and cannot name one, so this project's own names come out spelled the same on both
  screens.
- **`POST /v1/voice`, behind the switch that already governs sending** — and not because it writes
  anything. A device that may only read has nowhere to put a sentence once it has one, and
  transcribing spends ten-plus seconds of every core this Mac has on demand. Read-level access is
  meant to be cheap to grant; this is the one read-shaped thing here that is not.
- **No live text on a phone, and the page says so rather than pretending.** whisper.cpp cannot
  stream and Apple's recogniser runs on the Mac, so a phone records, waits, and gets the whole
  sentence at once. It counts the seconds while it waits, and adds that the first one after a
  restart loads the model first — twelve seconds of nothing looks exactly like a hang.
- **One at a time, one more in the queue, and the third is told to come back.** Transcription runs
  on a queue of its own, so a dictation cannot hold the event stream and every other page in the
  house for as long as it takes. Recording stops at three minutes on its own.
- **Every way it can refuse says which one it was, in all fourteen languages.** A microphone that
  was refused says where it is switched back on; a page opened over plain `http` says that a
  microphone needs https; a Mac with `whisper-cli` and no model says which of the two is missing,
  rather than "dictation failed".
- **`voice.transcribe` in the audit log** — device, seconds, milliseconds, characters, ok. How long
  the recording was and how long the transcript came out, and not a word of either.

Needs Whisper on the Mac ([docs/whisper.md](docs/whisper.md)) and an https address, which a tunnel
already gives you ([docs/remote.md](docs/remote.md)).

### Following a background agent into its own conversation

The strip that says *three agents are out* was the end of the road: it named them, said what each
had last reached for, and stopped there. What an agent actually did was on disk the whole time —
Claude Code writes each one a transcript beside the session's own — and nothing in the app or on
the page could open it.

- **Every row leads somewhere now.** Click an agent in the composer's strip on the page, or its
  tab above the ⌘J pane on the Mac, and the pane you are already reading swaps to that agent's
  conversation: same blocks, same folds, same reading order, because it is the same kind of
  record. `‹ Session` on the page and `← Session` in the pane come back; so do Escape, ⌘J and a
  phone's back gesture, one step each.
- **The session does not close to show it.** It stays open underneath — the row keeps updating,
  the list keeps its place — and an agent that is still working refreshes while you read it.
- **The strip is a tree, and it moved.** `main` at the root with a filled dot, a ring per agent
  under it, the kind of agent in a column of its own; the row you are reading is the lit one. It
  now sits above the composer rather than inside it, with the live line, so it is still there
  while an agent is on screen — as a line inside the box it vanished at exactly the moment you
  were navigating by it.
- **What it cost, which nothing was showing.** The header above an agent's transcript carries how
  long it ran, the tokens it drew and how many tools it used. The app has read those numbers since
  agents first appeared in the strip and had nowhere to put them.
- **`GET /v1/sessions/:id/agents/:agentId`**, the same shape as `…/transcript` plus the agent's
  own row. The id is checked before it names a file: anything that is not one of Claude Code's is
  a `404`, including anything shaped like a path.
- **Fixed: an agent's transcript read as empty.** Every record in one is marked as a sidechain,
  which is precisely what the session's own reader drops — so a busy agent came back with nothing
  at all. `Transcript.parse` now takes which of the two files it is reading.

### Codex sessions, in the same list

`isClaude` was a boolean for as long as there was only one thing it could be about. Codex draws a
different screen, keeps its record somewhere else and leaves on a different word, so what a session
is running became something it **has** rather than something the app assumes.

- **They are just sessions.** A tty running `codex` is in ⌘K next to the Claude Code ones, says
  what it is doing, takes a prompt, answers a question with a digit, and can be ended. Nothing is
  installed into Codex; it is read off what it already draws and already writes.
- **A row says which only when it matters.** With one assistant on the machine the word would be
  on every row and separate nothing, so it appears the moment the list is holding both.
- **⌘J reads the rollout**, `~/.codex/sessions/YYYY/MM/DD/rollout-….jsonl`, and lays it out as the
  same conversation a transcript becomes. Codex's own vocabulary comes through: `shell` for a
  command, `edit` for a file change, `web.search` for a plugin, `server.tool` for MCP.
- **Which file belongs to which session is a fact rather than a guess.** A Codex process holds its
  rollout open, so `lsof` names it outright. This was not theoretical: two sessions started seconds
  apart in this repository were shown each other's conversation by the version that matched on the
  directory and the clock. Its subagents write rollouts in the same folder within the same second,
  and `thread_source` in the first line is what tells those apart.
- **What it reads, observed rather than assumed.** Codex's live line is `• Working (10s • esc to
  interrupt)` — a bullet and a clock, where the bullet alone proves nothing because Codex prefixes
  everything it says with one. Its dialogs put the caret in **column zero**, which is also where it
  draws the composer's, so the rule the Claude Code reader leans on says nothing here; what
  separates them is that a dialog takes the composer away, so the last caret on the screen decides.
- **`codex exec`, `mcp-server` and the two servers are left out.** Same binary, and not somewhere
  you can type — a row that accepts your sentence and drops it is worse than no row.
- **Start either one.** *Start a session* offers whichever of the two this Mac has a home directory
  for. From a phone the assistant is a name in the path — `POST /v1/places/:id/start/codex` —
  matched against a two-case list; the body on that route is still not read at all.
- **`/quit`, not `/exit`.** Each refuses the other's word, so *End* asks the session which it is.
- Background agents stay a Claude Code row: the count comes from a directory only Claude Code
  writes, so a Codex session with three out looks like one thinking hard.
- `codex_home` in the config, for a Codex that does not live in `~/.codex` — an app launched from
  Finder inherits no login shell and cannot see your `CODEX_HOME`.

### A Codex session can name itself

A Codex row was the directory it was opened in and nothing else, which is fine until three of them
are open in one repository and the list stops distinguishing anything.

- **Off by default, and it says why.** *Settings → Name new Codex sessions*. Each title is a real
  Codex turn against your account, so this is not something to switch on for somebody.
- **One turn, after the first request.** The helper run is ephemeral, uses low reasoning with tools
  disabled, and asks `codex_auto_name_model` — `gpt-5.6-luna` unless you name another.
- **A name you chose is never overwritten.** Only a session that has never been titled is titled.
- **It is the third thing here that can use the network**, and the privacy section says so now
  rather than leaving the count at two.

### The `/` menu, in the bar and on the phone

Typing `/` in the bar used to be typing a character. It now opens the same nine-row surface the
session and stack lists use, filtered as you type, with <kbd>↑</kbd><kbd>↓</kbd> and <kbd>Tab</kbd>
to accept.

- **The list is what that working directory can actually reach** — project skills, personal skills
  and installed plugin skills, in the precedence a typed command would get: a personal skill
  replaces a project one of the same name, and a plugin skill keeps its namespace and so collides
  with neither. Skills switched off in settings are not offered.
- **Read off `SKILL.md`, not asked for.** Claude Code has no read-only way to ask a running session
  for its slash menu; its Agent SDK publishes one while starting a *new* session, and doing that on
  every `/` would be a model-shaped side effect for an autocomplete. The stable local half is enough
  to be useful and honest.
- **Metadata only.** The name and the description; never the body of a `SKILL.md`. Reading a menu
  must not execute the dynamic commands a skill is allowed to contain, and a prompt box that loaded
  skill bodies would be a second skill runtime with a second set of rules to get wrong.
- **A space ends completion.** From there the words are arguments and the ordinary Return-to-send
  path owns them.
- **The catalog is read once per session**, not once per keystroke, and a slow lookup for the tab
  you just left can never paint its skills under the new one's prompt.
- **The phone gets the same list** — `GET /v1/sessions/:id/skills`, `read` capability, metadata
  only, no local path in the reply. A Codex session answers it with an empty list for now.

### A notification says which session, not just which project

A push carried the project and the state — *"clawdline — waiting for you"* — on the reasoning that
the task title is the embarrassing half on a lock screen somebody else can read. That holds until
three sessions are open in one repository, which is the normal way this gets used: every
notification then reads the same and none of them says which tab to go to.

So the task becomes the title and the project moves down beside the state: **"fix the webhook"**
over *"clawdline is waiting for an answer"*. **This is a deliberate reversal of a documented privacy
decision**, and the prose that argued the old way has been rewritten rather than left standing — the
README, `docs/remote.md` and the comment on `WebPush.send` all say what is actually sent now. The
line that has not moved is the one under it: prompt text and transcript contents still never leave
the machine in a notification.

### The agents a session sent away

Everything here is learned by looking at a screen, and that stops working the moment the work moves
somewhere Claude Code does not draw. A session with three agents out searching a codebase painted
exactly the same spinner as one thinking about a sentence.

- **The conversations are already on disk** — `subagents/agent-<id>.meta.json` beside the
  transcript, written at the spawn — so they are read rather than guessed at. There is no record
  saying "started" and none saying "still going": an ending is a `<task-notification>` in the
  parent's transcript, so **running is the absence of one**, and the work is in establishing that
  absence cheaply enough to ask once a second. A session that has never spawned one costs a cached
  lookup and a single failed `stat`.
- The list row gets a count, the strip above the transcript gets what is happening away from it,
  and the phone gets a row per agent.
- **Quiet in all three.** An agent explains why a session is busy and never asks anything of you,
  and the one state allowed to be loud here is a session waiting for an answer.

### Everywhere a project opens

`GET /v1/sessions/:id/links` gathers the health endpoint from the icon registry, the run from the
deploy status, the servers from the project's own status command, and the backlog page. **None of it
is invented** — each is a URL some other tool already wrote into a file this app reads. The
contribution is that they are in one list on a phone, rather than four places on a Mac in another
room. On the Mac it is a Links sheet in the transcript header, in all fourteen languages; sort moved
into the settings sheet, where the rare controls live.

- **A route rather than a field on the session.** Working these out costs a `git` invocation plus a
  handful of file reads, and the session list goes out on the event stream every time anything
  moves — free when a menu is opened, a subprocess per session per second on the stream.
- **Rows are anchors only for `http(s)`**, a whitelist rather than a blacklist: those strings come
  out of a repository's own `devstack.json`, and `javascript:` in an `href` is script on that page
  with that page's cookie.
- **A `file://` row is not a link at all** — a path, a copy button, and a sentence saying it opens
  on the Mac. A link that does nothing when tapped is worse than text that explains itself.
- An untrusted dev stack stays silent rather than being probed.

### The notch, all day

`IslandMode` gains `.resting` as its floor: the character alone, breathing with its eyes shut, ears
the same width as one running session so waking moves the animation and not the shape. A `sleep`
routine is authored for both shipped packs, and a pack without one falls back to its own `idle`,
slowed, with the eyes held shut — which also suppresses `idle`'s random blink, since a sleeper does
not blink.

Because it is on screen all day, the bar is different from the states that last seconds: anything
catching the eye every few seconds is wrong. The breath is a sub-pixel swell over five seconds and
the loop closes exactly. `notch: false` still means nothing in the notch, and a screen with no
camera housing is left alone — the pill under a menu bar is fine for the minute a job runs and quite
another thing parked there all day.

**Drawing all day cost 3.6% of a core, continuously.** Throttling the redraw to 10fps measured no
difference at all and was reverted rather than shipped with a confident comment. The real cost was
building an `NSColor` from a hex string once per pixel cell per frame; memoised, 3.82% → 0.67%, with
the rendered frame byte-identical.

### From the page

- **End a session.** `exit` sent from the page never worked and could not have: it arrives at the
  prompt as a *message*, and once the assistant has gone the tab drops off the list, so the shell
  that could have taken it was unreachable from the moment it became a shell.
  `POST /v1/sessions/:id/end` sends the assistant's own word, waits, then closes — **in that order**,
  because the transcript is appended to right up to the moment the process ends and it is the thing
  you would still want tomorrow. It closes the *session*, not the tab: an iTerm2 tab can be split
  and the panes beside it belong to work nobody asked about; when it was the only one, iTerm2
  removes the tab, which is what the person pressing this expects. tmux gets `kill-pane` for the
  same reason. **No new capability** — a device that may type could already send `/exit` and then
  `exit`. Audited as `session.end`, and every test of it is a refusal, because a suite that
  occasionally ends somebody's session is a suite people stop running.
- **Bring a session's tab to the front**, without the page having to say where it is.
- **Answer a menu from the phone.** `isChoosing` parsed every option in order to count them and then
  returned a `Bool`, so a phone could be told a question was waiting and never told what it was. It
  returns the options now. `POST /key` had existed the whole time; what was missing was seeing what
  you were answering. **The number drawn is the number sent, never the position** — renumbering rows
  to make them tidy is how a button comes to answer a different question than its label.
- **A bare URL is a link.** Written links already worked; an address on its own did not, which is
  most of them. Both are handled in one pass with `[label](href)` first in the pattern, and the
  order is the whole trick — it is consumed whole, so the bare rule never sees the URL inside it.
  Trailing punctuation goes outside the link, and a closing bracket only if the address did not open
  one: `…/Foo_(bar)` keeps its paren, `(https://example.com)` does not.
- **A conversation says whether it is still running.** The Mac has the notch and the footer; a phone
  has neither once the list is a different screen. The live line now sits above the composer while a
  session works — dim and monospace, not another coloured panel competing with the warning that
  sometimes sits beside it.
### Fixed

- **Codex sessions had started disappearing from the list.** Interactive Codex now runs
  `codex app-server --listen stdio://` beside its own UI, and the rule that keeps `codex exec` and
  the servers out of the list was refusing the whole tty on account of the child. Refusal now flows
  **down the process tree** on `ppid` instead of sideways across the tty, so a server descendant no
  longer disqualifies the interactive parent that spawned it — while `codex exec`'s own native child
  is still kept out by its refused ancestor. `codex sandbox` joined the list of subcommands that are
  not somewhere you can type.
- **Each row wears the assistant's product mark**, Claude's coral and OpenAI's green, drawn as SVG
  so an 11-point mark stays sharp. It still appears only when the list is holding both — but when it
  does, the split is visible before the word beside it has been read.
- **A new Codex session no longer borrows the previous one's transcript.** A rollout that predates
  the process holding it is not that process's rollout.
- **A numbered list you typed was read as a menu.** `❯` is both the glyph a dialog marks its
  current row with and the one Claude Code puts in front of the line you type, so a message opening
  with a numbered list echoed back as character-for-character the shape of a menu with its first row
  selected. The session went to *waiting*, the phone raised "this session is waiting for an answer",
  and that notice says sending from here confirms the highlighted option rather than typing — so
  somebody who sends lists, which is most people, was told a question existed and warned off
  answering it, leaving nothing they could do.
- **Every page decided it was out of date the moment the stream connected.** A page identifies a
  build from `build|version|protocol`; `build` had been added to `/v1/health` and not to the `hello`
  event, so the two sources disagreed about which fields exist and the stamps differed *by
  construction*, immediately, on every page. Reloading could not clear it, because the fresh page
  computed the same mismatch a second later. Both send the same fields now.
- **A phone already holding a stale page had no way to learn otherwise.** Serving `no-store` fixed
  every load after the fix and did nothing for a device that already had the old copy — it never
  asks again, so it never finds out. The service worker now claims open tabs and fetches the page
  with `cache: "reload"` rather than letting the HTTP cache answer.
- **A black screen shipped**, from `git add -A` in a worktree shared with other agents: it picked up
  an `index.html` that was midway through having its sort control removed, so the markup was gone
  and the listener binding to it was not. The script died on the first line that touched it.
- **Every rebuild was a coin flip on leaving a crash report behind.** `pkill` asks; it does not
  wait, and the next line deleted the bundle a process on its way out was still reading — AppKit's
  teardown asks CoreFoundation for the bundle identifier, which then reads freed memory. `build.sh`
  waits now, and it stopped printing "relaunched" the instant `open` returned, which said nothing:
  a build that killed the app and failed to restart it used to report success while the person
  watching saw their bar vanish with no reason given.
- **The release script deleted the build before checking it** — it removed the worktree and then
  looked for the app inside it, so the check meant to catch an empty build was the thing
  guaranteeing one. The build lands beside the worktree now.
- **One build that timed out could wedge every build after it.** `build.sh` asks the running app to
  pause while it is replaced, and it waited five seconds for the answer. A drain that took 146
  seconds therefore printed `refused` and stopped — while the pause it had just registered went on
  and closed the door behind it. Nothing was left that could reopen it: the id needed to cancel was
  generated inside the script, never printed, and its only copy was in a directory the script
  deletes on the way out. Every later build was refused, and it took a hand-written request to
  recover. The wait is now one budget shared by the request and the polling that follows it, the id
  is printed and written down before the request goes out, the script ends a pause whenever it
  holds one rather than only when it saw a reply, and **"no answer" and "refused" are two
  different sentences** — a client that heard nothing is not a server that said no.
- **The "this page is older" notice can be dismissed.** It was correct and unclearable, and
  reloading to silence a banner is what somebody halfway through a sentence is trying not to do.
- The sleepy-tuna mascot pack failed its own validator.

## 0.6.0 — 2026-08-19

The release the README had been describing. 0.5.0 was cut by hand two hours before the remote half
landed, so for a day the only build you could download did not contain the thing half the README is
about — which is why [tools/release.sh](tools/release.sh) exists and why nothing is cut by hand any
more: it builds from a clean worktree at HEAD, runs the tests, checks the built app's own version
string, and does not publish anything until every one of those has passed.

### Answer a session from your phone

Your Mac serves a page; your phone opens it on your own domain through your own Cloudflare tunnel.
Every session with its state and its transcript, a box to type into, and Web Push when one starts
waiting for you — signed on your Mac with CryptoKit. **There is no account, no relay and no server
of mine in the path**, which also means it works when your Claude account uses an API key.

It is not a terminal in a browser and deliberately not. It answers one question: which session
wants you, and can you answer it from here.

- **Off until you switch it on**, and typing into a session is a second switch after that.
- Pairing is a six-digit code shown on the Mac; a device gets read, or read-and-send.
- Loopback only. `Host` headers are validated so a hostile page cannot reach it by DNS rebinding,
  and cross-site requests are refused.
- A multiple-choice question can be answered from the phone, by the option's own number — the
  picker discards a typed answer, so `/send` refuses one outright rather than answering the wrong
  thing quietly.
- Start a new session in any project this Mac has worked in — the client sends an opaque id, never
  a path, and the command is the literal `claude`.

### Claude Code can say so itself

Everything here works by looking, and looking has one cost it cannot avoid: it only knows what it
has looked at. With the bar away that is once every twenty seconds — long enough for a permission
dialog to sit there through a whole train of thought.

- **Optional hooks, off until you press a button.** *Settings → Claude Code hooks → Install* puts
  five entries in `~/.claude/settings.json`. After that, the moment a turn starts, ends or needs
  an answer, a two-line note lands in a directory the app is watching, and the reading that would
  have happened twenty seconds later happens in under a second instead. Measured on three
  sessions: 20s → 0.8s.
- **The polling does not change.** Same three round trips a minute; a note moves one of them to a
  moment worth taking it rather than adding one.
- **The screen is still the authority, and that is the design.** `Notification` fires both for a
  permission request and for a session that has merely been quiet for a minute, so a note asks
  for a reading and `SessionState` still decides what is on the screen. **No note asserts that a
  session is working.** Measuring is what settled that: Claude Code draws its live line about 2.1
  seconds after you press Return and then removes it again while the answer streams, so a claim
  short enough to be safe would cover almost none of a turn — and a long one could not be
  retracted, because pressing Esc to cancel fires no hook at all. A nudge looks twice instead,
  immediately and again 2.5 seconds later, which is the same information with nothing claimed.
- **The one thing a note does settle** is something the screen gets wrong rather than misses: a
  live line that was never erased after a fast turn. A `Stop` overrides it for ten seconds.
- **Five events, all rare.** `PreToolUse` is deliberately not among them — it fires hundreds of
  times an hour to say something `UserPromptSubmit` and `Stop` already bracket. `SubagentStop` is
  left out because a subagent finishing is not the session finishing.
- **Your settings file is a guest room.** Everything already in it is read, changed and written
  back, a copy is kept once as `settings.json.before-clawdline`, and removing the hooks leaves
  the file reading as though this had never touched it.
- **⌘J finds the transcript by name.** A hook carries the session id, which is what Claude Code
  names the transcript file after — so the matching by title and start time is only needed when
  there are no hooks.
- `clawdline://hooks?install=1` and `install=0`, for setting a machine up from a script.
- `"hooks": false` in the config ignores the notes without touching anybody's settings file.
- **And the Mac tells you.** Notifications when a session starts waiting, when a turn over two
  minutes ends, and — if you switch it on — when a deploy stops running.

The contract, including what a note is and is not allowed to change, is in
[docs/hooks.md](docs/hooks.md).

### Everywhere else

Fourteen languages, on the Mac and on the page. A transcript pane that folds a finished run of tool
calls to one line. On-device dictation through Whisper. A settings window that looks like it belongs
to this project, and an app icon. A backlog the status line can draw.

### Security

A route that took a directory and a command out of the request body and ran the second in the first
has been removed, along with the code behind it. **It was never in a release** — 0.5.0 predates the
whole remote feature — but it was on `main` for a day, and it is named here rather than left in a
diff. What replaces it has no field a path or a command can be written into.
[docs/remote.md](docs/remote.md) has the threat model in full, including what it does not defend
against.

### Requires

macOS 13+, Apple silicon. iTerm2 directly, every other terminal through tmux. Built against Claude
Code 2.1.235 — see [docs/compatibility.md](docs/compatibility.md). Swift and AppKit, no
dependencies, no package manager. 1205 tests.

## 0.5.0 — 2026-08-18

### The bar knows what every session is doing

Not looking at the terminal worked for one session. With four, you were back to going round the
tabs to find out who had finished — so the thing that made the bar worth having stopped scaling
at exactly the point you started needing it.

- **⌘K names what each session is doing.** A row that is working carries the line Claude Code
  draws for itself, quietly; a session with a question on screen and nobody answering it says
  so, loudly, because that is the only state that costs you something for every second it goes
  unnoticed. Nothing is installed into Claude Code to know this — it is each session's own
  screen, read the same way the ⌘J pane reads it, and a screen that cannot be read leaves the
  row exactly as plain as it was rather than guessing at it.
- **The menu bar ✳ carries it too.** Nothing running and it is the character it always was;
  things running and it carries a count; something waiting for an answer and it says so in the
  accent. It is the one piece of screen this app owns all day and it used to say nothing.
- **One reading serves all of it.** The session list, the strip above the transcript, the menu
  bar and the island are four consumers of one set of terminal round trips — 1.2s while the
  panel is up, once every twenty seconds while it is not, and a single `ps` and nothing else on
  a machine with no Claude Code running.

### The servers a project runs

⌘S lists every project that describes a dev stack, whether or not a session is open in it —
because the project whose servers have quietly fallen over is exactly the one you have no
session in. It reads a `.devstack.json` out of the repository and runs the commands that file
names; **Clawdline never starts a process of its own**, so the servers outlive the app rather
than dying with it on the next quit or update. A row can start, restart and stop a stack, and
show what its processes printed. The format is documented in
[docs/devstack.md](docs/devstack.md), so anything can produce one — process-compose, Overmind,
pm2, Docker Compose, a Makefile with PID files.

A stack whose status command has never been agreed to is drawn as its own thing rather than as
"down": a grey square next to a green one reads as an outage, and the first day that shipped it
sent somebody looking for one that was not happening.

### A character in the notch

Play, and meant to read that way — it tells you nothing the menu bar mark does not. Your mascot
lives in the menu bar band beside the camera housing: it leans out while something is running,
says which session wants you when one does, and dances when a long job finishes. How hard it
appears to be working is how much you have running.

Clicking the character opens the bar; clicking the words goes to that terminal tab. When the
number stands for more than one session, it offers a menu rather than picking for you.

`"notch": false` turns the whole thing off — no window, no observer, nothing drawn.

### Settings, as controls

Menu bar ✳ → **Settings…** has a control for everything worth changing, and every control
applies the moment you move it. The hotkey is recorded by pressing it rather than spelled into a
text field; the pane's font list offers only monospaced faces, because that is a setting you can
only get wrong. `config.json` is still the truth, still hand-editable, and there is a button in
the window that opens it.

### Switching sessions got about five times faster

Measured on a real 29 MB transcript, per press of ↓: **443 ms → 86 ms**, and to roughly nothing
for a session you have already looked at.

- `Transcript.parse` read the whole tail and threw away all but the last four hundred entries.
  It reads backwards now and stops when it has enough — and walks the UTF-8 view rather than
  building an array of every line, which was 140 ms of the 268 on its own.
- `Transcript.locate` was calling `stat` inside a sort comparator, so a project with fifty-six
  transcripts in it spent several hundred of them to order fifty-six names.
- Transcript titles are remembered against each file's size and mtime, so the six megabytes of
  reading that picked one file happens once rather than on every switch.
- Laid-out transcripts are kept, keyed by the same signature that decides whether a repaint is
  needed, and the sessions either side of the selected one are laid out before you ask for them.

**Fixed: switching quickly could paint the wrong session's conversation**, under the next
session's name — nothing checked that the reader you started was still the reader you wanted by
the time it finished.

### The terminal's tab follows the bar

The bar's target and the tab in front of you were free to be two different sessions, and the
moment you closed the panel you were looking at the wrong one. They are now the same session by
construction. Selecting is not the same as activating and only the first one happens, or every
press of Tab would take the keyboard out of the box you are typing into.
`"follow_target": false` restores the old behaviour.

### Fixed

- **tmux found no Claude Code at all.** The pane's process name is the basename of the
  executable, and the current installer symlinks `claude` at
  `~/.local/share/claude/versions/<version>` — so every pane announced itself as a version
  number and every tmux session was listed as an ordinary shell. That is the one path the README
  promises for Terminal.app, Ghostty, Warp and the rest. `ps` reads argv, which still says
  `claude`, so the tty is asked as well as the name.
- **A tmux session never once reported being busy.** Its captures arrive with the colours still
  in them, and a line that begins with a colour code does not begin with the character it looks
  like it begins with.

### For contributors

- **A string that is left in English now fails the build.** The check that catches "copied the
  reference file and translated half of it" used to sample fifteen strings by hand, so a new one
  was by definition not in it — a whole settings window shipped with thirty-two strings that
  nothing looked at. It reflects over every stored string now, and the handful that legitimately
  read the same in two languages are exempted one at a time, per language, with a reason.
- **`/recap` is in the repo.** Four questions at the end of a stretch of work — what changed,
  what it is worth, what has gone out of sync, and is it in version control — with the checks
  that this project in particular keeps forgetting: the fourteen languages, the two READMEs, and
  the test count that three files claim.

## 0.4.0 — 2026-08-17

Dictation that hears two languages in one sentence and does not need an account, images that
arrive as images, and thirteen languages of interface.
→ [Full notes](https://github.com/sainteye/clawdline/releases/tag/v0.4.0)

## 0.3.0 — 2026-08-15

Every terminal, through tmux: Terminal.app, Warp, Tabby, Ghostty, Alacritty and Kitty all work
as long as Claude Code runs inside tmux.
→ [Full notes](https://github.com/sainteye/clawdline/releases/tag/v0.3.0)

## 0.2.0 — 2026-08-15

Mascots became a browsable, swappable format, with a second pack to prove the format was one.
→ [Full notes](https://github.com/sainteye/clawdline/releases/tag/v0.2.0)

## 0.1.0 — 2026-08-15

First public release: a Spotlight-style prompt bar that floats at eye level and sends what you
type straight into a Claude Code session, without bringing the terminal to the front.
→ [Full notes](https://github.com/sainteye/clawdline/releases/tag/v0.1.0)
