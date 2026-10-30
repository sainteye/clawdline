import { catalogWord } from "../catalog.js"
import { escapeHTML } from "../legacy/bridge.js"

export const WORKFLOW_PROFILES = ["clawdline", "superpowers", "poteto"] as const
export type WorkflowStage = { id: string; title: string; output: string; acceptance: string }
export type WorkflowRecord = {
  state: string; profile_id?: string; version?: string; source?: string; digest?: string
  graphify_enabled?: boolean; stages?: WorkflowStage[]
}
export const workflowWord = (key: string) => catalogWord("workflowProfiles", key)
export function workflowName(id: string): string {
  return WORKFLOW_PROFILES.includes(id as typeof WORKFLOW_PROFILES[number]) ? workflowWord(id) : id
}
export function workflowLabel(record: WorkflowRecord | undefined): string {
  if (record?.state === "recorded" && record.profile_id) return workflowName(record.profile_id)
  return workflowWord(record?.state === "unavailable" ? "unavailable" : "legacy")
}

/** Escaped wire evidence only; machine defaults never stand in for a launch record. */
export function workflowInfoHTML(record: WorkflowRecord | undefined): string {
  const esc = escapeHTML
  const row = (name: string, value: string) => '<div><dt>' + esc(workflowWord(name)) + '</dt><dd>' + esc(value) + '</dd></div>'
  const recorded = record?.state === "recorded" && !!record.profile_id
  const caret = '<svg class="flow-caret" viewBox="0 0 16 16" aria-hidden="true" focusable="false"><path d="m4 6 4 4 4-4"></path></svg>'
  const state = !recorded ? workflowWord(record?.state === "unavailable" ? "unavailableDescription" : "legacyDescription")
    : workflowWord(record.profile_id === "clawdline" ? "standardState" : "loadedState")
  let body = '<p class="flow-fixed-note">' + esc(recorded ? workflowWord("fixed") : state) + '</p>'
  if (recorded) {
    body += '<dl class="flow-facts">' + row("source", record.source || workflowWord("unknown")) +
      row("version", record.version || workflowWord("unknown")) + row("digest", record.digest || workflowWord("unknown")) +
      row("launch", state) + row("use", workflowWord("noUseReceipt")) +
      row("graphify", workflowWord(typeof record.graphify_enabled === "boolean" ? record.graphify_enabled ? "on" : "off" : "unknown")) + '</dl>'
    if (record.stages?.length) {
      body += '<details class="flow-steps" data-workflow-detail="steps"><summary><span>' + esc(workflowWord("steps")) + '</span>' + caret + '</summary><ol>'
      for (const stage of record.stages) {
        const builtin = record.version === "1" && WORKFLOW_PROFILES.includes(record.profile_id as typeof WORKFLOW_PROFILES[number]) && ["brief", "plan", "execute", "verify", "deliver"].includes(stage.id)
        const text = (field: "title" | "output" | "acceptance") => builtin ? workflowWord("stage." + stage.id + "." + field) : stage[field]
        body += '<li><strong>' + esc(text("title")) + '</strong><p>' + esc(workflowWord("output")) + ': ' + esc(text("output")) + '</p><p>' +
          esc(workflowWord("acceptance")) + ': ' + esc(text("acceptance")) + '</p></li>'
      }
      body += '</ol></details>'
    }
  }
  return '<details class="flow-session" data-workflow-detail="profile"><summary><span><span class="flow-eyebrow">' + esc(workflowWord("title")) +
    '</span><strong>' + esc(workflowLabel(record)) + '</strong><small>' + esc(recorded ? workflowWord("fixedShort") : state) + '</small></span>' + caret +
    '</summary><div class="flow-session-body">' + body + '</div></details>'
}
