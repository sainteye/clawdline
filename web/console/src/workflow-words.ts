/** The Playbook surface's English source and Taiwan Traditional Chinese copy. */
import type { Snippet } from "./legacy/snippets-bridge.js"
// @ts-expect-error Node's strip-types tests import this module directly.
import { catalogWord, type CatalogTag } from "./catalog.ts"
const en = {
  reload: "Reload", intro: "Save a launch message for this Project or every Project on this machine. Linked Skills and commands supply their Playbook name and launch message.",
  projectUnavailable: "Could not read Projects. Try again.", workflowUnavailable: "Could not read this Project's Playbooks or Skills. Try again.",
  loading: "Loading…", new: "Create Playbook", fromSkill: "Create from Skill", chooseSkill: "Choose a Skill", backToWorkflows: "Back to Playbooks",
  empty: "This Project has no Playbooks yet.", edit: "Edit Playbook", name: "Name", message: "Launch message",
  emptyTitle: "Give this Project a repeatable start", emptyDetail: "Save a launch message once, then start it from any Session in this Project.",
  emptyGlobalTitle: "Create a Playbook for every Project", emptyGlobalDetail: "Save a launch message once, then start it from a Session in any Project on this machine.",
  importSkill: "Import a Skill or command", editAction: "Edit", workflowCount: "Saved Playbooks", projectLabel: "Project", scopeLabel: "Playbook scope",
  skillListTitle: "Choose a Skill or command", skillPreviewTitle: "Skill preview", commandPreviewTitle: "Command preview", claudeCommand: "Claude command", skillNoDescription: "No description provided",
  skillReadFailed: "Could not read this Skill. Try again.", backToSkills: "Back to Skills", useSkill: "Use this Skill",
  sourceLabel: "Imported from", sourceDetail: "Original source", sourceUnavailable: "This source can no longer be opened. The saved Playbook is still available.",
  sourceChanged: "This source has changed since it was linked. The latest source is used when starting.", noSource: "Independent Playbook", linkSource: "Link a Skill or command", changeSource: "Change Skill or command", unlinkSource: "Remove link", replaceLinkText: "Link and replace text",
  sourceTextMismatch: "The linked source or launch message has changed. Update from source before starting this Playbook.",
  sourceNeedsSync: "This linked Playbook must be updated from its source before it can be saved or started.",
  sourceScopeConflict: "A Project-only source cannot be linked to a Global Playbook. Remove the link or create a Playbook for that Project.",
  sourceChecking: "Checking linked source…", sourceReady: "Linked source checked", sourceStartUnavailable: "The linked source could not be checked. Open the Playbook editor to update or remove its link.",
  sourceChangedReview: "The source changed since this preview. Review the updated message, then start again.", linkedPickerHint: "Uses the latest linked source when started.",
  linkedNameHint: "You can rename this Playbook. Changing its linked source replaces the name and launch message.",
  linkedMessageHint: "The launch message follows the linked source and cannot be edited here. Remove the link to write your own.",
  linkHint: "Linking replaces the Playbook name and launch message with text for this source.",
  skillImportHint: "This creates a linked Playbook with a fixed launch message using the Skill name. Remove the link to customize it.",
  commandImportHint: "This creates a linked Playbook that asks the assistant to read this Project command. The command file is still required; remove the link to customize the launch message.",
  global: "Global", projectOnly: "This Project", availability: "Available in", globalHint: "Available in every Project on this machine.",
  globalSourceHint: "This Project-only source cannot be used by a Global Playbook. Remove the link or create a Playbook for that Project.",
  chooseWorkflow: "Choose a Playbook to start", reviewLaunch: "Ready to start", noSkills: "No available Skills or commands to import for this Project", noGlobalSkills: "No global Skills or commands available to import", skillsUnavailable: "Could not read Skills or commands. Reload to try again.",
  save: "Save", cancel: "Cancel", delete: "Delete", needsText: "Enter a name and launch message.",
  saveUnknown: "The save result is unknown. Reload the list before trying again.",
  deleteUnknown: "The delete result is unknown. Reload the list.",
  tooLong: "The name or launch message is too long. Keep the name under 200 bytes and the message under 4,000 bytes.",
  limitReached: "This Project has reached the Playbook storage limit.",
  saveRefused: "This Playbook could not be saved. Check the name and launch message.",
  deleteRefused: "This Playbook could not be deleted. Reload the list.",
  confirmDelete: "Delete Playbook ‘{name}’?",
  skillPrompt: "Use the {name} Skill, complete its workflow, and report the result.",
  commandPrompt: "Read the Project's Claude command /{name}, follow its instructions to complete the workflow, and report the result.",
  noSessionWorkflows: "This Project has no Playbooks yet. Create one from the sidebar.",
  projectResolveUnavailable: "This machine could not identify the Session's Project. Project Playbooks and the built-in review need it; other Global Playbooks are available.",
  start: "Start Playbook", starting: "Starting…", startUnknown: "The start result is unknown. Check Session to-dos before sending again.",
  alreadyRunning: "A Playbook is already running in this Session. Mark it done in Session to-dos before starting another.",
  workflow: "Playbook", inProgress: "In progress", finished: "Done",
  running: "Playbook · In progress", done: "Playbook · Done", sent: "Sent", queued: "Ready to send",
  complete: "Mark done", reopen: "Reopen", send: "Send",
  zeroReviewTitle: "Zero-Based Review",
  zeroReviewBody: `Run a zero-based review of this Project. Create an Epic to track it, then assign independent reviews from the relevant roles: product, UX, architecture, code quality, efficiency, documentation, and evidence.
Require evidence for every finding. Compare the current design with mature expert practice, and distinguish deliberate trade-offs from accumulated complexity.
Find confusing behavior, redundant steps or reads and writes, and missing capabilities.
Synthesize a report and a target workflow. Turn improvements into role-owned child items, and list decisions that need my input separately.
Collect shared facts once and include them in each review brief so reviewers do not repeat the same reading.`,
} as const

const zhHant: Record<keyof typeof en, string> = {
  reload: "重新讀取", intro: "為這個 Project 或這台機器上的所有 Project 儲存啟動訊息。連結 Skill 或指令時，名稱與啟動訊息由來源決定。",
  projectUnavailable: "無法讀取 Project，請重試。", workflowUnavailable: "無法讀取此 Project 的 Playbook 或 Skill，請重試。",
  loading: "讀取中…", new: "建立 Playbook", fromSkill: "從 Skill 建立", chooseSkill: "選擇 Skill", backToWorkflows: "返回 Playbook 列表",
  empty: "這個 Project 還沒有 Playbook。", edit: "編輯 Playbook", name: "名稱", message: "啟動訊息",
  emptyTitle: "為這個 Project 建立 Playbook", emptyDetail: "儲存完整的啟動訊息，之後就能從這個 Project 的 Session 啟動。",
  emptyGlobalTitle: "建立全域 Playbook", emptyGlobalDetail: "儲存一次啟動訊息，這台機器上任何 Project 的 Session 都能啟動。",
  importSkill: "從 Skill／指令帶入", editAction: "編輯", workflowCount: "已儲存的 Playbook", projectLabel: "Project", scopeLabel: "Playbook 範圍",
  skillListTitle: "選擇 Skill 或指令", skillPreviewTitle: "預覽 Skill", commandPreviewTitle: "預覽指令", claudeCommand: "Claude 指令", skillNoDescription: "未提供說明",
  skillReadFailed: "無法讀取這個 Skill，請重試。", backToSkills: "返回 Skill 清單", useSkill: "帶入這個 Skill",
  sourceLabel: "建立來源", sourceDetail: "原始來源", sourceUnavailable: "已無法開啟這個來源；已儲存的 Playbook 仍可使用。",
  sourceChanged: "這個來源在連結後已有更新；啟動時會使用最新來源。", noSource: "獨立的 Playbook", linkSource: "連結 Skill／指令", changeSource: "更換技能／指令", unlinkSource: "移除連結", replaceLinkText: "連結並替換文字",
  sourceTextMismatch: "連結來源或啟動訊息已有變動。啟動前請先從來源更新。",
  sourceNeedsSync: "這個連結 Playbook 必須先從來源更新，才能儲存或啟動。",
  sourceScopeConflict: "Project 專屬來源不能連結全域 Playbook。請移除連結，或為該 Project 建立 Playbook。",
  sourceChecking: "正在檢查連結來源…", sourceReady: "已確認連結來源", sourceStartUnavailable: "無法確認連結來源。請到 Playbook 編輯頁更新或移除連結。",
  sourceChangedReview: "來源在預覽後已有變動。請確認更新後的訊息，再按一次開始。", linkedPickerHint: "啟動時使用最新的連結來源。",
  linkedNameHint: "你可以自行命名這個 Playbook。更換連結來源時，名稱與啟動訊息會一起更新。",
  linkedMessageHint: "啟動時會讀取最新的連結來源，因此無法在這裡編輯啟動訊息。移除連結後即可自行撰寫。",
  linkHint: "連結後會以這個來源替換 Playbook 的名稱與啟動訊息。",
  skillImportHint: "帶入後會建立以 Skill 名稱啟動的連結 Playbook；移除連結後才能自訂文字。",
  commandImportHint: "帶入後會建立請助理讀取此 Project 指令的連結 Playbook；執行時仍需原始指令檔案。移除連結後才能自訂文字。",
  global: "全域", projectOnly: "這個 Project", availability: "使用範圍", globalHint: "這台機器上的所有 Project 都能使用。",
  globalSourceHint: "Project 專屬來源不能用於全域 Playbook。請移除連結，或為該 Project 建立 Playbook。",
  chooseWorkflow: "選擇要啟動的 Playbook", reviewLaunch: "確認啟動", noSkills: "這個 Project 沒有可帶入的 Skill 或指令", noGlobalSkills: "沒有可帶入的全域 Skill 或指令", skillsUnavailable: "無法讀取 Skill 或指令，請重新讀取。",
  save: "儲存", cancel: "取消", delete: "刪除", needsText: "請填寫名稱與啟動訊息。",
  saveUnknown: "儲存結果無法確認；請重新讀取清單後再試。",
  deleteUnknown: "刪除結果無法確認；請重新讀取清單。",
  tooLong: "名稱或啟動訊息太長。名稱最多 200 位元組，訊息最多 4,000 位元組。",
  limitReached: "這個 Project 的 Playbook 儲存數量已達上限。",
  saveRefused: "無法儲存這個 Playbook，請檢查名稱與啟動訊息。",
  deleteRefused: "無法刪除這個 Playbook，請重新讀取清單。",
  confirmDelete: "確定刪除 Playbook「{name}」？",
  skillPrompt: "請執行「{name}」Skill，依照其中流程完成工作並回報結果。",
  commandPrompt: "請讀取這個 Project 的 Claude 指令「/{name}」，依照其中內容完成流程並回報結果。",
  noSessionWorkflows: "這個 Project 還沒有 Playbook。請從側邊欄建立。",
  projectResolveUnavailable: "這台機器無法辨識此 Session 所屬的 Project。專屬 Playbook 與內建審查需要這項資訊；其他全域 Playbook 仍可使用。",
  start: "開始 Playbook", starting: "啟動中…", startUnknown: "啟動結果無法確認。請到 Session 待辦查看紀錄，再決定是否重送。",
  alreadyRunning: "這個 Session 已有 Playbook 在執行。請先在 Session 待辦把它標記完成，再啟動下一個。",
  workflow: "Playbook", inProgress: "執行中", finished: "完成",
  running: "Playbook · 執行中", done: "Playbook · 完成", sent: "已送出", queued: "待送出",
  complete: "標記完成", reopen: "重新開啟", send: "送出",
  zeroReviewTitle: "歸零審查",
  zeroReviewBody: `對這個 Project 做一次歸零審查。建立 Epic 追蹤工作，並依需要分派獨立的角色審查：產品、UX、架構、程式碼品質、效率、文件與證據。
每項結論都要附上證據。對照成熟的人類專家做法，分清刻意取捨與歷史累積的複雜度。
找出容易誤導人的行為、多餘步驟或讀寫，以及缺少的能力。
彙整審查報告與目標流程，把改善項目拆成各角色負責的子項目；需要我決定的取捨另外列出。
共用事實只整理一次，放進每份審查 brief，避免各角色重複讀取。`,
}

const words = { en, "zh-Hant": zhHant }

export type WorkflowWord = keyof typeof words.en
export function workflowWordFor(lang: CatalogTag, key: WorkflowWord, holes: Record<string, string> = {}): string {
  const source = words[lang === "zh-Hant" ? "zh-Hant" : "en"][key]
  return source.replace(/\{(\w+)\}/g, (all, name) => holes[name] ?? all)
}
export function workflowWord(key: WorkflowWord, holes: Record<string, string> = {}): string {
  return catalogWord("workflow", key).replace(/\{(\w+)\}/g, (all, name) => holes[name] ?? all)
}

/** An unedited built-in uses the reader's language; edited rows keep their text. */
export function localizedWorkflow(row: Snippet): Snippet {
  if (row.builtin !== "zero_review") return row
  return { ...row, title: workflowWord("zeroReviewTitle"), body: workflowWord("zeroReviewBody") }
}
