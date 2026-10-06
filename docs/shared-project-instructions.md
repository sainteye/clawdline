# Shared project instructions: one source a Project's Claude and Codex sessions both read

Status: **proposed design, 2026-10-06.** Nothing here is built. Claims about how Clawdline behaves
today cite the file and line they were read from; anything not checked is labelled *assumption*.

## 1. The problem

A person runs one Project with both Claude Code and Codex sessions and expects them to know the same
things: the same rules, the same skills, the same lessons learned. Today each assistant reads its own
files, and nothing keeps them together:

| What | Claude Code reads | Codex reads | What Clawdline does today |
|---|---|---|---|
| Project rules | `CLAUDE.md` (and `.claude/CLAUDE.md`, `CLAUDE.local.md`) | `AGENTS.md` | Lists and edits both side by side (`docs/project-files.md:3-9`); never relates them |
| Project skills | `.claude/skills/<name>/SKILL.md` | `.agents/skills`, `.codex/skills` | Browses each provider's folders separately to build the squad catalog (`internal/transport/http/squad_skill_sources.go:55-82`); copies nothing across |
| The Clawdline skill | `~/.claude/skills/clawdline/SKILL.md` | — | Installed for Claude only (`internal/adapters/skillfile/skillfile.go:87-89`) |
| Role / persona prompt | System prompt via `--append-system-prompt-file` (`internal/adapters/projects/launch.go:194,288`) | A `-c developer_instructions` sentence telling Codex to read the file (`launch.go:198,279-285`), which also replaces the person's own `developer_instructions` for that session (`docs/personas.md:206-209`) | Same file, two delivery strengths |
| Project memory | Claude's own auto-memory under `~/.claude/projects/<slug>/memory/` (*assumption*: Claude Code behaviour, not Clawdline's) | Nothing equivalent found | None. No route, store or launch input (only the ledger classifies reads of `MEMORY.md`, `internal/adapters/transcript/ledger.go:1171-1172`) |

The result, seen on this repository: `CLAUDE.md` is a pointer to `AGENTS.md` by hand convention
(`CLAUDE.md:1`), there is no `.agents/` directory, and whatever Claude's auto-memory has learned
about the Project is invisible to a Codex session on it. A rule learned twice costs twice; a rule learned
once is followed by half the sessions.

## 2. What "integrated" means

A Project is **integrated** when all four hold, and Clawdline can check each one without asking an
assistant what it loaded:

1. **One rules file.** `AGENTS.md` is the source. `CLAUDE.md` contains the import line `@AGENTS.md`
   and only what is Claude-specific below it. (Claude Code expands `@path` imports in `CLAUDE.md` —
   *assumption* to verify on 2.1.291 with a ten-line experiment before building.)
2. **One skills directory.** `.agents/skills/<name>/` is the source. `.claude/skills/<name>` is a
   link to it (or a generated copy — decision P2), so both assistants see the same skill set.
3. **One memory.** Lessons about the Project live in one Clawdline-owned store outside the
   repository, and every Claude and Codex session launched for the Project is given its index the
   same way (decision P1, decided: a Clawdline-owned store).
4. **No drift.** A check reports any skill that exists for one assistant only, any rule in
   `CLAUDE.md` that contradicts `AGENTS.md`, and any memory entry not reachable by both.

Things deliberately left alone: the person's home-scope files (`~/.claude/CLAUDE.md`,
`~/.codex/AGENTS.md`) stay theirs — Clawdline reads them for the report and never writes them
(home files are read-only today, `docs/project-files.md:5`). Clawdline's own role skills
(`internal/domain/squad/builtin_skills/`) stay a catalog, not provider installs
(`docs/persona-skill-decisions.md`).

## 3. The flow: integrate one Project

Run per Project, by hand, from the Project gear (Settings and instructions) or
`clawdline project integrate <place>`. It is a Board Feature with the usual planning gate, so the
person approves before anything is written.

1. **Inventory (read-only, no child).** Reuse the existing inventory
   (`GET /v1/projects/{place}/files`) and add: skills present for one assistant only, and the
   Project's memory entries from every source found (Clawdline store; the Claude auto-memory
   directory for this Project, read-only). Output: one table, like §1, for this Project.
2. **Proposal (one child, read-only).** A `technical-writer` child reads both rules files and every
   memory entry and writes a plan document on the Feature:
   - the merged `AGENTS.md` (only rules that apply to any assistant), and the slim `CLAUDE.md`;
   - each skill's destination, and same-named skills that differ (listed for the person, not merged
     silently);
   - each memory entry: keep in the shared store / move into `AGENTS.md` because every session needs
     it / drop because it is stale or contradicted by the code, with the evidence;
   - contradictions it found, each as a question for the person.
3. **Approve (the person).** At most three `answer` Notes for real conflicts. Nothing is written
   before the person's message.
4. **Apply (one child, in a disposable worktree).** Writes the repository side (`AGENTS.md`,
   `CLAUDE.md`, `.agents/skills`, links) through the Project's own commit and check flow; writes the
   memory store outside the repository. Personal memory never enters a repository; for a public
   repository the Project's privacy check applies as usual.
5. **Verify.** `clawdline project integrate --check <place>` re-runs step 1 and must report no
   drift. Then one Claude and one Codex session are launched on the Project with the same question
   whose answer is only in a memory entry and only in a skill; both must answer it. This is the
   end-to-end proof; the file check alone does not show an assistant loaded anything.
6. **Stay integrated.** The Project card shows **Instructions: shared / drifting (n)**. Drift is
   found by the same check on the existing Project status refresh; it never fixes anything itself.

## 4. Launch changes (the product side)

- **Memory index in every launch.** Clawdline writes the Project's memory index to a private file
  (beside the squad launch files) and passes it through the channel each assistant already has:
  Claude `--append-system-prompt-file`, Codex the existing `developer_instructions` value
  (`launch.go:302-308` already joins several instructions into one). Both get the same file.
- **Writing memory.** `clawdline memory add|update|forget --project <place>` writes the store; the
  `clawdline` skill tells both assistants to use it instead of their own memory for Project lessons.
  This is what makes a lesson learned in a Codex session reach the next Claude session.
- **Clawdline skill for Codex.** `clawdline skill install` also writes `~/.agents/skills/clawdline/`
  (*assumption*: Codex 0.160.1 loads home skills from there, as the skill browser assumes at
  `squad_skill_sources.go:80`).
- **Codex delivery strength.** Codex is told to read the role and memory files rather than given
  them. Whether it reliably does is not measured; the end-to-end step in §3.5 measures it per
  Project.

## 5. Decisions for the person

- **P1 — where Project memory lives. Decided 2026-10-06: (a).**
  (a) A Clawdline-owned store per Project, both assistants write through `clawdline memory`
  (chosen: it is the only option that is the same for both and works on another machine via
  Cloud later). Cost: Claude's built-in auto-memory keeps working beside it, so the skill must steer
  Project lessons to the shared store; the first integration imports existing entries once.
  (b) Claude's auto-memory stays the source, Clawdline only mirrors its index into Codex launches.
  Cost: Codex can read but never add; ties the design to one vendor's private layout.
- **P2 — skills in `.claude/skills`: link or copy.** Link: one copy, no drift, but a link inside a
  repository may not survive every checkout or Windows (*assumption*). Copy: works everywhere, needs
  the drift check to catch edits made to the copy.
- **P3 — what goes in `AGENTS.md` vs the memory store.** Proposed rule: a fact every session needs
  on every task goes in `AGENTS.md` (it is committed and public for a public repository); a lesson
  needed only in some situations, or private to the person, goes in the store.

## 6. Open hypotheses and how to settle each

| Hypothesis | How to confirm or refute |
|---|---|
| Claude Code expands `@AGENTS.md` inside `CLAUDE.md` | Ten-line experiment in a scratch Project: a rule only in `AGENTS.md`, ask a Claude session |
| Codex loads `~/.agents/skills` and project `.agents/skills` | Same experiment with a one-line skill, Codex 0.160.1 |
| Codex reliably reads a file it is only told to read | Twenty launches with a memory-only fact; count correct answers |
| Linked skill directories survive clone and Windows | Clone the scratch Project on the Windows build target |

## 7. Order of work

1. The four experiments in §6 (cheap; they decide P2 and the shape of §4).
2. Memory store + `clawdline memory` + launch delivery (§4) — the one change that gives both
   assistants the same memory.
3. Inventory additions and `--check` (§3.1, §3.5).
4. The integrate flow and the Project card status (§3.2-§3.6).
5. Integrate this repository first as the pilot.

<!-- clawdline-doc: kind=spec audience=agent -->
