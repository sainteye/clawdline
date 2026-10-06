import { test } from "node:test"
import assert from "node:assert/strict"
import {
  actionPictures, conflictViews, driftCount, lineChanges, mayApply, movesSummary, unifyColumns, unifyStatusLine,
// @ts-expect-error -- `.ts` paths let Node's strip-types runner execute this test.
} from "./project-unify.ts"

// A drifting Project as the machine plans it: CLAUDE.md without the import,
// one skill only Codex sees, one only Claude sees, one identical copy, and one
// conflict (lines in CLAUDE.md Codex cannot see).
function drifting(): any {
  return {
    status: "drifting",
    version: "v1",
    links_available: true,
    rules: {
      agents: "present", claude: "present", claude_imports_agents: false,
      claude_only_files: ["CLAUDE.local.md"],
      claude_only_lines: ["Use the staging database.", "Never push on Fridays."],
      now: { claude: ["CLAUDE.md"], codex: ["AGENTS.md"] },
      after: { claude: ["CLAUDE.md", "AGENTS.md"], codex: ["AGENTS.md"] },
    },
    skills: [
      { name: "deploy", place: "agents", linked: false, copy: false, now: { claude: false, codex: true }, after: { claude: true, codex: true }, action: "skill_link" },
      { name: "review", place: "claude", linked: false, copy: false, now: { claude: true, codex: false }, after: { claude: true, codex: true }, action: "skill_move_and_link" },
      { name: "notes", place: "both_same", linked: false, copy: false, now: { claude: true, codex: true }, after: { claude: true, codex: true }, action: "skill_replace_copy_with_link" },
      { name: "style", place: "both_different", linked: false, copy: false, now: { claude: true, codex: true }, after: { claude: true, codex: true } },
    ],
    actions: [
      { kind: "rules_add_import", paths: ["CLAUDE.md"], description: "Add an @AGENTS.md line…",
        edits: [{ path: "CLAUDE.md", before: "Use the staging database.\nNever push on Fridays.\n", after: "@AGENTS.md\nUse the staging database.\nNever push on Fridays.\n" }] },
      { kind: "skill_link", paths: [".claude/skills/deploy"], link_target: "../../.agents/skills/deploy", description: "Link…", edits: [] },
      { kind: "skill_move_and_link", paths: [".claude/skills/review", ".agents/skills/review"], link_target: "../../.agents/skills/review", description: "Move…", edits: [] },
      { kind: "skill_replace_copy_with_link", paths: [".claude/skills/notes", ".agents/skills/notes"], link_target: "../../.agents/skills/notes", description: "Replace…", edits: [] },
    ],
    conflicts: [
      { kind: "claude_only_lines", path: "CLAUDE.md", detail: "Codex does not see these lines; move the shared ones into AGENTS.md." },
      { kind: "skill_differs", path: ".claude/skills/style", detail: "This skill differs…" },
    ],
  }
}

const marks = (column: any) => Object.fromEntries(column.rows.map((row: any) => [row.name, row.mark]))

test("each assistant's column marks what it sees now and after: 不變, 新增, or the gap", () => {
  const [claude, codex] = unifyColumns(drifting())
  assert.equal(claude.title, "Claude")
  assert.equal(codex.title, "Codex")
  assert.deepEqual(marks(claude), {
    "CLAUDE.md": "same", "AGENTS.md": "added", "CLAUDE.local.md": "same",
    deploy: "added", review: "same", notes: "same", style: "same",
  })
  assert.deepEqual(marks(codex), { "AGENTS.md": "same", deploy: "same", review: "added", notes: "same", style: "same" })
  assert.equal(claude.rows.find((r: any) => r.name === "CLAUDE.local.md")?.note, "只給 Claude，unify 不更動")
  assert.equal(codex.rows.find((r: any) => r.name === "style")?.note, "兩邊內容不同")
})

test("a skill neither side will share is marked as the gap, not as unchanged", () => {
  const plan = drifting()
  plan.skills = [{ name: "blocked", place: "claude", linked: false, copy: false,
    now: { claude: true, codex: false }, after: { claude: true, codex: false } }]
  const [, codex] = unifyColumns(plan)
  assert.deepEqual(marks(codex).blocked, "missing")
})

test("the status line counts every change and conflict, and an unknown plan says what could not be read", () => {
  assert.deepEqual(unifyStatusLine(drifting()), { tone: "attention", text: "有落差（6 項）" })
  assert.equal(driftCount(drifting()), 6)
  assert.deepEqual(unifyStatusLine({ ...drifting(), status: "unified", actions: [], conflicts: [] }), { tone: "ready", text: "已共用" })
  const unknown = { ...drifting(), status: "unknown", conflicts: [{ kind: "unreadable", path: ".agents/skills/odd", detail: "x" }] }
  assert.deepEqual(unifyStatusLine(unknown), { tone: "unknown", text: "無法判斷（讀不到 .agents/skills/odd）" })
  assert.equal(mayApply(unknown), false)
  assert.equal(mayApply(drifting()), true)
  assert.equal(mayApply({ ...drifting(), actions: [] }), false)
})

test("a rules edit is drawn with the inserted @AGENTS.md line marked, and nothing else changed", () => {
  const [rules] = actionPictures(drifting())
  assert.match(rules.sentence, /最上面加一行 @AGENTS\.md/)
  assert.equal(rules.skill, null)
  assert.deepEqual(rules.files[0].lines, [
    { text: "@AGENTS.md", change: "added" },
    { text: "Use the staging database.", change: "same" },
    { text: "Never push on Fridays.", change: "same" },
  ])
})

test("moving rules into a new AGENTS.md shows the new file and CLAUDE.md shrinking to the import", () => {
  const plan = drifting()
  plan.actions = [{ kind: "rules_create_agents", paths: ["AGENTS.md", "CLAUDE.md"], description: "", edits: [
    { path: "AGENTS.md", before: null, after: "Rule one\nRule two\n" },
    { path: "CLAUDE.md", before: "Rule one\nRule two\n", after: "@AGENTS.md\n" },
  ] }]
  const [picture] = actionPictures(plan)
  assert.equal(picture.files[0].created, true)
  assert.deepEqual(picture.files[0].lines, [{ text: "Rule one", change: "added" }, { text: "Rule two", change: "added" }])
  assert.deepEqual(picture.files[1].lines, [
    { text: "Rule one", change: "removed" }, { text: "Rule two", change: "removed" }, { text: "@AGENTS.md", change: "added" },
  ])
  const moves = movesSummary(plan)
  assert.deepEqual(moves.created, ["AGENTS.md"])
  assert.deepEqual(moves.moved, ["CLAUDE.md 的規則 → AGENTS.md"])
  assert.equal(moves.sentence, "不會刪除任何東西。")
})

test("a long unchanged CLAUDE.md folds to a count after two lines of context around the inserted line", () => {
  const before = Array.from({ length: 20 }, (_, i) => `line ${i}`).join("\n") + "\n"
  const lines = lineChanges(before, "@AGENTS.md\n" + before)
  assert.deepEqual(lines[0], { text: "@AGENTS.md", change: "added" })
  assert.deepEqual(lines.slice(1), [
    { text: "line 0", change: "same" }, { text: "line 1", change: "same" }, { folded: 18 },
  ])
})

test("skills are drawn as from → to, with what the arrow means", () => {
  const [, link, move, replace] = actionPictures(drifting())
  assert.deepEqual(link.skill, { name: "deploy", from: ".claude/skills/deploy", to: ".agents/skills/deploy", arrow: "link", caption: "連結" })
  assert.match(link.sentence, /Claude 也看得到.*「deploy」/)
  assert.deepEqual(move.skill, { name: "review", from: ".claude/skills/review", to: ".agents/skills/review", arrow: "move", caption: "搬過去，原處留連結" })
  assert.equal(replace.skill?.arrow, "replace")
})

test("the summary names which files move and that the only removal is an identical copy", () => {
  const moves = movesSummary(drifting())
  assert.deepEqual(moves.moved, [".claude/skills/review → .agents/skills/review"])
  assert.deepEqual(moves.removedCopies, [".claude/skills/notes"])
  assert.match(moves.sentence, /唯一會移除的是 1 份.*完全相同的副本（\.claude\/skills\/notes）/)
})

test("each conflict says what it is and what the person can do, with the lines Codex cannot see", () => {
  const [lines, differs] = conflictViews(drifting())
  assert.equal(lines.sentence, "CLAUDE.md 有 2 行只有 Claude 看得到，Codex 看不到。")
  assert.deepEqual(lines.lines, ["Use the staging database.", "Never push on Fridays."])
  assert.match(lines.remedy, /AGENTS\.md/)
  assert.match(differs.sentence, /skill「style」/)
  assert.match(differs.remedy, /\.agents\/skills\/style/)
})
