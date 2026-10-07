import { localizedLiteralMap } from "../../catalog.js"
import { catalogFormat } from "../../catalog.js"
import { catalogWord } from "../../catalog.js"
import { labelled, listSeparator } from "../../punctuation.js"
// What the unify preview shows, computed from the machine's plan and nothing
// else (docs/project-files.md, Unify). Kept out of JSX so each picture can be
// tested as data: who sees which file now and after, what each action does to
// which path, and what unify leaves for a person to decide.
//
// The sentences are the console's own, composed from each action's kind and
// paths: the plan's `description` and `detail` are English for a terminal, and
// a person reading this screen reads Traditional Chinese.
import type {
  ProjectUnifyAction, ProjectUnifyConflict, ProjectUnifyPlan, ProjectUnifyReads,
} from "@clawdline/contract"

export type Assistant = "claude" | "codex"

/** 不變 / 新增（套用後才看得到）/ 看不到（這是落差）; `lost` is a plan that would hide something, which unify never plans. */
export type SeenMark = "same" | "added" | "missing" | "lost"

export const MARK_WORDS: Record<SeenMark, string> = localizedLiteralMap({
  same: "920a83370e34",
  added: "fef5e42dbb55",
  missing: "bd3837042be2",
  lost: "8f94e36cd835",
})

export interface SeenRow {
  key: string
  kind: "rules" | "skill"
  name: string
  now: boolean
  after: boolean
  mark: SeenMark
  /** One short phrase under the name, or "". */
  note: string
}

export interface SeenColumn { assistant: Assistant; title: string; rows: SeenRow[] }

function markOf(now: boolean, after: boolean): SeenMark {
  if (now && after) return "same"
  if (!now && after) return "added"
  if (!now && !after) return "missing"
  return "lost"
}

function rulesRows(assistant: Assistant, now: ProjectUnifyReads, after: ProjectUnifyReads): SeenRow[] {
  const names: string[] = []
  for (const name of [...now[assistant], ...after[assistant]]) if (!names.includes(name)) names.push(name)
  return names.map(name => {
    const n = now[assistant].includes(name), a = after[assistant].includes(name)
    return { key: `rules:${name}`, kind: "rules", name, now: n, after: a, mark: markOf(n, a), note: "" }
  })
}

/**
 * The two columns: every rules file and every skill by name, as each assistant
 * sees it now and after apply. A skill one assistant cannot see now and will
 * not see after (a conflict) is the gap the person has to resolve.
 */
export function unifyColumns(plan: ProjectUnifyPlan): SeenColumn[] {
  const differs = new Set(plan.skills.filter(s => s.place === "both_different").map(s => s.name))
  return (["claude", "codex"] as const).map(assistant => {
    const rows = rulesRows(assistant, plan.rules.now, plan.rules.after)
    if (assistant === "claude") {
      for (const file of plan.rules.claude_only_files) {
        if (rows.some(row => row.name === file)) continue
        rows.push({ key: `rules:${file}`, kind: "rules", name: file, now: true, after: true, mark: "same", note: catalogWord("literal", "de4050822523") })
      }
    }
    for (const skill of plan.skills) {
      const now = skill.now[assistant], after = skill.after[assistant]
      const note = differs.has(skill.name) ? catalogWord("literal", "e83e668845a4")
        : catalogWord("literal", "a24619afa16e")
      rows.push({ key: `skill:${skill.name}`, kind: "skill", name: skill.name, now, after, mark: markOf(now, after), note })
    }
    return { assistant, title: assistant === "claude" ? "Claude" : "Codex", rows }
  })
}

export type UnifyTone = "ready" | "attention" | "unknown"

/** Why a plan is unknown, in words: the paths unify could not read. */
export function unknownReason(plan: ProjectUnifyPlan): string {
  const paths = plan.conflicts.filter(c => c.kind === "unreadable" || c.kind === "too_large").map(c => c.path)
  for (const [file, state] of [["AGENTS.md", plan.rules.agents], ["CLAUDE.md", plan.rules.claude]] as const) {
    if (state === "unreadable" && !paths.includes(file)) paths.push(file)
  }
  if (paths.length === 0) return catalogWord("literal", "67ba4a3f606c")
  if (paths.length === 1) return catalogFormat("template", "bf557dd5d65c", [paths[0]])
  return catalogFormat("template", "1971a299186d", [paths[0], paths.length])
}

/** How many things differ: each planned change and each conflict counts once. */
export function driftCount(plan: ProjectUnifyPlan): number {
  return plan.actions.length + plan.conflicts.length
}

/** The block's one line: 已共用 / 有落差（n 項）/ 無法判斷（原因）. */
export function unifyStatusLine(plan: ProjectUnifyPlan): { tone: UnifyTone; text: string } {
  switch (plan.status) {
    case "unified": return { tone: "ready", text: catalogWord("literal", "97db2287057d") }
    case "drifting": return { tone: "attention", text: catalogFormat("template", "56c4350d4c99", [driftCount(plan)]) }
    default: return { tone: "unknown", text: catalogFormat("template", "c21648fe8edd", [unknownReason(plan)]) }
  }
}

/** Apply is offered only for a plan that has something to do and could be fully read. */
export function mayApply(plan: ProjectUnifyPlan): boolean {
  return plan.status !== "unknown" && plan.actions.length > 0
}

// ---- the actions, as pictures

export type LineChange = "same" | "added" | "removed"
export interface PictureLine { text: string; change: LineChange }
/** A run of unchanged lines folded away, so a long CLAUDE.md does not bury the one inserted line. */
export interface FoldedLines { folded: number }
export interface FilePicture { path: string; created: boolean; lines: (PictureLine | FoldedLines)[] }

export type ArrowKind = "link" | "move" | "replace" | "copy"
export interface SkillPicture { name: string; from: string; to: string; arrow: ArrowKind; caption: string }

export interface ActionPicture {
  key: string
  sentence: string
  files: FilePicture[]
  skill: SkillPicture | null
}

const KEEP_CONTEXT = 2
const FOLD_AT = 6

function splitLines(text: string): string[] {
  const lines = text.split("\n")
  if (lines.length > 1 && lines[lines.length - 1] === "") lines.pop()
  return lines
}

/**
 * Before and after as one list of lines: the common head and tail are
 * unchanged, the middle of `before` was removed and the middle of `after`
 * added. That is exactly the shape of both rules edits — an import inserted
 * at the top, or the whole file replaced by the import — so no general diff is
 * needed. Long unchanged runs fold to a count.
 */
export function lineChanges(before: string | null, after: string): (PictureLine | FoldedLines)[] {
  const b = before === null ? [] : splitLines(before)
  const a = splitLines(after)
  let head = 0
  while (head < b.length && head < a.length && b[head] === a[head]) head++
  let tail = 0
  while (tail < b.length - head && tail < a.length - head && b[b.length - 1 - tail] === a[a.length - 1 - tail]) tail++
  const lines: PictureLine[] = [
    ...a.slice(0, head).map(text => ({ text, change: "same" as const })),
    ...b.slice(head, b.length - tail).map(text => ({ text, change: "removed" as const })),
    ...a.slice(head, a.length - tail).map(text => ({ text, change: "added" as const })),
    ...a.slice(a.length - tail).map(text => ({ text, change: "same" as const })),
  ]
  const out: (PictureLine | FoldedLines)[] = []
  for (let i = 0; i < lines.length;) {
    if (lines[i].change !== "same") { out.push(lines[i]); i++; continue }
    let j = i
    while (j < lines.length && lines[j].change === "same") j++
    const run = lines.slice(i, j)
    const keepHead = i === 0 ? 0 : KEEP_CONTEXT
    const keepTail = j === lines.length ? 0 : KEEP_CONTEXT
    if (run.length >= FOLD_AT && run.length > keepHead + keepTail) {
      out.push(...run.slice(0, keepHead), { folded: run.length - keepHead - keepTail }, ...run.slice(run.length - keepTail))
    } else out.push(...run)
    i = j
  }
  return out
}

/** A new file that would be shown whole is cut to its first lines and a count. */
const NEW_FILE_LINES = 12

function filePictures(action: ProjectUnifyAction): FilePicture[] {
  return action.edits.map(edit => {
    let lines = lineChanges(edit.before, edit.after)
    if (edit.before === null && lines.length > NEW_FILE_LINES) {
      lines = [...lines.slice(0, NEW_FILE_LINES), { folded: lines.length - NEW_FILE_LINES }]
    }
    return { path: edit.path, created: edit.before === null, lines }
  })
}

const baseName = (path: string) => path.split("/").filter(Boolean).pop() ?? path

function skillPicture(action: ProjectUnifyAction): SkillPicture | null {
  const [first, second] = action.paths
  switch (action.kind) {
    case "skill_link": {
      // The link's own place is the only path; where it points is under .agents/skills.
      const name = baseName(first)
      return { name, from: first, to: second ?? `.agents/skills/${name}`, arrow: "link", caption: catalogWord("literal", "0e65ca077795") }
    }
    case "skill_move_and_link":
      return { name: baseName(first), from: first, to: second, arrow: "move", caption: catalogWord("literal", "71669d600a21") }
    case "skill_replace_copy_with_link":
      return { name: baseName(first), from: first, to: second, arrow: "replace", caption: catalogWord("literal", "e9fdcd2c600d") }
    case "skill_copy":
      return { name: baseName(first), from: first, to: second, arrow: "copy", caption: catalogWord("literal", "dc5f1e4f6042") }
    default:
      return null
  }
}

/** The person's sentence for one action, from its kind and paths. */
export function actionSentence(action: ProjectUnifyAction): string {
  const skill = skillPicture(action)
  switch (action.kind) {
    case "rules_create_agents":
      return catalogWord("literal", "0a9316607f27")
    case "rules_add_import":
      return catalogWord("literal", "c08f09bed8ec")
    case "skill_link":
      return catalogFormat("template", "04eb2d98c178", [skill!.from, skill!.to, skill!.name])
    case "skill_move_and_link":
      return catalogFormat("template", "34222c185d1a", [skill!.from, skill!.to, skill!.name])
    case "skill_replace_copy_with_link":
      return catalogFormat("template", "61465ed010f6", [skill!.from, skill!.to, skill!.from, skill!.to])
    case "skill_copy":
      return catalogFormat("template", "3b2adf429bd8", [skill!.from, skill!.to])
    default:
      return action.description
  }
}

export function actionPictures(plan: ProjectUnifyPlan): ActionPicture[] {
  return plan.actions.map((action, index) => ({
    key: `${index}:${action.kind}:${action.paths.join(",")}`,
    sentence: actionSentence(action),
    files: filePictures(action),
    skill: skillPicture(action),
  }))
}

/**
 * Which files move, and whether anything is removed. Unify deletes nothing; the
 * one exception is an identical copy replaced by a link, and it is named.
 */
export function movesSummary(plan: ProjectUnifyPlan): { moved: string[]; created: string[]; removedCopies: string[]; sentence: string } {
  const moved: string[] = [], created: string[] = [], removedCopies: string[] = []
  for (const action of plan.actions) {
    if (action.kind === "skill_move_and_link") moved.push(`${action.paths[0]} → ${action.paths[1]}`)
    if (action.kind === "rules_create_agents") moved.push(catalogWord("literal", "bf74f7ef840d"))
    if (action.kind === "skill_replace_copy_with_link") removedCopies.push(action.paths[0])
    for (const edit of action.edits) if (edit.before === null) created.push(edit.path)
  }
  const sentence = removedCopies.length === 0
    ? catalogWord("literal", "74df92b721ee")
    : catalogFormat("template", "712e3e1175ee", [removedCopies.length, removedCopies.join(listSeparator())])
  return { moved, created, removedCopies, sentence }
}

// ---- what unify will not decide

export interface ConflictView { key: string; path: string; sentence: string; remedy: string; lines: string[] }

export function conflictViews(plan: ProjectUnifyPlan): ConflictView[] {
  return plan.conflicts.map((conflict, index) => ({
    key: `${index}:${conflict.kind}:${conflict.path}`,
    path: conflict.path,
    ...conflictWords(conflict, plan),
  }))
}

function conflictWords(conflict: ProjectUnifyConflict, plan: ProjectUnifyPlan): { sentence: string; remedy: string; lines: string[] } {
  const path = conflict.path
  switch (conflict.kind) {
    case "claude_only_lines": {
      const lines = plan.rules.claude_only_lines
      return { sentence: catalogFormat("template", "e08a0f1557ad", [lines.length]),
        remedy: catalogWord("literal", "a35624ea0763"), lines }
    }
    case "import_without_agents":
      return { sentence: catalogWord("literal", "c39a8e57dbfc"), remedy: catalogWord("literal", "ad1250f9023e"), lines: [] }
    case "rules_link":
      return { sentence: catalogFormat("template", "395d57f8f142", [path]), remedy: catalogWord("literal", "6aa72e4fdef8"), lines: [] }
    case "skill_differs":
      return { sentence: catalogFormat("template", "c79d0687f211", [baseName(path)]), remedy: catalogFormat("template", "74b6e4bba30b", [baseName(path)]), lines: [] }
    case "skill_link_elsewhere":
      return { sentence: catalogFormat("template", "676bac47a5f4", [path]), remedy: catalogWord("literal", "292b7755b937"), lines: [] }
    case "skills_directory_link":
      return { sentence: catalogFormat("template", "b9db3e2cd844", [path]), remedy: catalogWord("literal", "e094f3526875"), lines: [] }
    case "name_taken":
      return { sentence: catalogFormat("template", "c77dd85e5dac", [path]), remedy: catalogWord("literal", "9dd0e6117357"), lines: [] }
    case "too_large":
      return { sentence: catalogFormat("template", "f56ee5f2d93c", [path]), remedy: catalogWord("literal", "062b9a10a6f1"), lines: [] }
    case "unreadable":
      return { sentence: catalogFormat("template", "aaa57deb124f", [path]), remedy: catalogWord("literal", "8f8136f76819"), lines: [] }
    default:
      return { sentence: labelled(path, conflict.detail), remedy: catalogWord("literal", "6e9be990a363"), lines: [] }
  }
}
