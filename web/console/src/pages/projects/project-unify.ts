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

export const MARK_WORDS: Record<SeenMark, string> = {
  same: "不變",
  added: "新增（套用後才看得到）",
  missing: "看不到（這是落差）",
  lost: "套用後看不到",
}

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
        rows.push({ key: `rules:${file}`, kind: "rules", name: file, now: true, after: true, mark: "same", note: "只給 Claude，unify 不更動" })
      }
    }
    for (const skill of plan.skills) {
      const now = skill.now[assistant], after = skill.after[assistant]
      const note = differs.has(skill.name) ? "兩邊內容不同"
        : skill.copy ? "以副本共用（這台機器不能建立連結）"
          : ""
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
  if (paths.length === 0) return "有檔案讀不到"
  if (paths.length === 1) return `讀不到 ${paths[0]}`
  return `讀不到 ${paths[0]} 等 ${paths.length} 個項目`
}

/** How many things differ: each planned change and each conflict counts once. */
export function driftCount(plan: ProjectUnifyPlan): number {
  return plan.actions.length + plan.conflicts.length
}

/** The block's one line: 已共用 / 有落差（n 項）/ 無法判斷（原因）. */
export function unifyStatusLine(plan: ProjectUnifyPlan): { tone: UnifyTone; text: string } {
  switch (plan.status) {
    case "unified": return { tone: "ready", text: "已共用" }
    case "drifting": return { tone: "attention", text: `有落差（${driftCount(plan)} 項）` }
    default: return { tone: "unknown", text: `無法判斷（${unknownReason(plan)}）` }
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
      return { name, from: first, to: second ?? `.agents/skills/${name}`, arrow: "link", caption: "連結" }
    }
    case "skill_move_and_link":
      return { name: baseName(first), from: first, to: second, arrow: "move", caption: "搬過去，原處留連結" }
    case "skill_replace_copy_with_link":
      return { name: baseName(first), from: first, to: second, arrow: "replace", caption: "相同的副本換成連結" }
    case "skill_copy":
      return { name: baseName(first), from: first, to: second, arrow: "copy", caption: "複製" }
    default:
      return null
  }
}

/** The person's sentence for one action, from its kind and paths. */
export function actionSentence(action: ProjectUnifyAction): string {
  const skill = skillPicture(action)
  switch (action.kind) {
    case "rules_create_agents":
      return "把 CLAUDE.md 的規則搬到新的 AGENTS.md，CLAUDE.md 只留一行 @AGENTS.md 引用它。規則沒有刪掉，只是換了位置，兩邊都讀得到。"
    case "rules_add_import":
      return "在 CLAUDE.md 最上面加一行 @AGENTS.md，讓 Claude 也讀 AGENTS.md；CLAUDE.md 其他內容不動。"
    case "skill_link":
      return `在 ${skill!.from} 建一個連結指向 ${skill!.to}，讓 Claude 也看得到 Codex 已經看得到的 skill「${skill!.name}」。`
    case "skill_move_and_link":
      return `把 ${skill!.from} 搬到 ${skill!.to}，原處留一個連結，讓 Codex 也看得到 skill「${skill!.name}」。`
    case "skill_replace_copy_with_link":
      return `${skill!.from} 和 ${skill!.to} 內容完全相同；把 ${skill!.from} 這份副本換成指向 ${skill!.to} 的連結。`
    case "skill_copy":
      return `把 ${skill!.from} 複製到 ${skill!.to}（這台機器不能建立連結），之後的檢查會比對兩份是否一致。`
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
    if (action.kind === "rules_create_agents") moved.push("CLAUDE.md 的規則 → AGENTS.md")
    if (action.kind === "skill_replace_copy_with_link") removedCopies.push(action.paths[0])
    for (const edit of action.edits) if (edit.before === null) created.push(edit.path)
  }
  const sentence = removedCopies.length === 0
    ? "不會刪除任何東西。"
    : `唯一會移除的是 ${removedCopies.length} 份與 .agents/skills 完全相同的副本（${removedCopies.join("、")}），原處換成連結，內容仍在。`
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
      return { sentence: `CLAUDE.md 有 ${lines.length} 行只有 Claude 看得到，Codex 看不到。`,
        remedy: "把兩邊都需要的內容搬進 AGENTS.md；只給 Claude 的可以留著。", lines }
    }
    case "import_without_agents":
      return { sentence: "CLAUDE.md 引用了 AGENTS.md，但 AGENTS.md 不存在。", remedy: "把共用的規則寫進 AGENTS.md，再重新檢查。", lines: [] }
    case "rules_link":
      return { sentence: `${path} 是一個連結，unify 不會更動它。`, remedy: "在機器上確認它指向哪裡；要共用的話改成一般檔案，再重新檢查。", lines: [] }
    case "skill_differs":
      return { sentence: `skill「${baseName(path)}」在 Claude 和 Codex 兩邊的內容不同。`, remedy: `留一份在 .agents/skills/${baseName(path)}，移除另一份，再重新檢查。`, lines: [] }
    case "skill_link_elsewhere":
      return { sentence: `${path} 是連到別處的連結，unify 不會跟著它或改它。`, remedy: "在機器上檢查這個連結；要共用就讓它指向 .agents/skills 裡的同名資料夾。", lines: [] }
    case "skills_directory_link":
      return { sentence: `${path} 整個資料夾是一個連結，unify 不會更動它。`, remedy: "在機器上檢查這個資料夾連結。", lines: [] }
    case "name_taken":
      return { sentence: `${path} 已經有別的東西用了這個名字。`, remedy: "先把它移走或改名，再重新檢查。", lines: [] }
    case "too_large":
      return { sentence: `${path} 太大，unify 讀不完。`, remedy: "在機器上檢查這個檔案或資料夾。", lines: [] }
    case "unreadable":
      return { sentence: `讀不到 ${path}。`, remedy: "在機器上檢查它的權限或編碼（需要 UTF-8 文字）。", lines: [] }
    default:
      return { sentence: `${path}：${conflict.detail}`, remedy: "在機器上檢查。", lines: [] }
  }
}
