// Plain operations behind the charter page's Playbook section: what a card
// says about its readers, the first sentence it previews, the reason a write
// records, and what one charter version changed in the playbooks. No words
// are looked up here; the screen turns the results into catalog text.
import type * as W from "@clawdline/contract"
// @ts-expect-error Node's strip-types runner loads source extensions.
import { charterLineDiff, type DiffLine } from "./charter-text.ts"

type Playbook = W.ProjectAssistantCharterPlaybook
type Reader = W.ProjectAssistantCharterReader

/** A card's reading state: every assigned Bot has the latest, someone has not, or nobody is assigned. */
export type PlaybookState = "current" | "unread" | "unassigned"

export function playbookState(readers: { read: readonly Reader[]; unread: readonly Reader[] }): PlaybookState {
  if (readers.unread.length) return "unread"
  return readers.read.length ? "current" : "unassigned"
}

const markup = /^(?:#{1,6}\s+|[-*+]\s+|\d+[.)]\s+|>\s*)+/

/**
 * The playbook's first sentence for its card: the first line with words,
 * without list or heading marks and emphasis, up to its first full stop,
 * shortened to `max` characters.
 */
export function firstSentence(text: string, max = 90): string {
  const line = text.split("\n").map(item => item.replace(markup, "").replace(/\*\*|__|`/g, "").trim()).find(Boolean) ?? ""
  // Full-width stops are written as escapes: 。！？
  const stop = /[\u3002\uff01\uff1f]|[.!?](?=\s|$)/u.exec(line)
  const sentence = stop ? line.slice(0, stop.index + stop[0].length) : line
  const chars = [...sentence]
  return chars.length > max ? chars.slice(0, max - 1).join("").trimEnd() + "…" : sentence
}

/** Who has not read the change yet, split by whether a turn is under way now. */
export function unreadByActivity(unread: readonly Reader[]): { working: Reader[]; idle: Reader[] } {
  return { working: unread.filter(reader => reader.running || reader.queued), idle: unread.filter(reader => !reader.running && !reader.queued) }
}

/** What changing a playbook's assignment does: who starts hearing it and who is told it no longer applies. */
export function assignmentChange(before: readonly string[], after: readonly string[]): { added: string[]; removed: string[] } {
  return { added: after.filter(id => !before.includes(id)), removed: before.filter(id => !after.includes(id)) }
}

/** The catalog key of the reason a playbook write records. */
export type PlaybookWrite = "add" | "edit" | "assign" | "remove"
export const playbookReasonKey = (kind: PlaybookWrite) => ({ add: "reasonPlaybookAdd", edit: "reasonPlaybookEdit", assign: "reasonPlaybookAssign", remove: "reasonPlaybookRemove" })[kind]

/** One playbook a version changed: its diff lines, and which of its title and assignment moved. */
export interface ChangedPlaybook {
  id: string
  title: string
  kind: "added" | "removed" | "edited"
  lines: DiffLine[]
  /** The earlier title when it was renamed. */
  renamedFrom?: string
  /** The assigned Bots before and after, when they changed. */
  assigned?: { before: string[]; after: string[] }
}

const sameSet = (a: readonly string[], b: readonly string[]) => a.length === b.length && a.every(id => b.includes(id))
const body = (text: string) => text.replace(/\n+$/, "")

/** The playbooks that differ between two versions, matched by id, in the newer version's order and then the removed ones. */
export function changedPlaybooks(before: readonly Playbook[], after: readonly Playbook[]): ChangedPlaybook[] {
  const out: ChangedPlaybook[] = []
  for (const next of after) {
    const old = before.find(item => item.id === next.id)
    if (!old) { out.push({ id: next.id, title: next.title, kind: "added", lines: charterLineDiff("", body(next.text)).filter(line => line.kind !== "removed"), assigned: { before: [], after: [...next.bot_ids] } }); continue }
    const textChanged = body(old.text) !== body(next.text), renamed = old.title !== next.title, reassigned = !sameSet(old.bot_ids, next.bot_ids)
    if (!textChanged && !renamed && !reassigned) continue
    out.push({ id: next.id, title: next.title, kind: "edited", lines: textChanged ? charterLineDiff(body(old.text), body(next.text)) : [],
      ...(renamed ? { renamedFrom: old.title } : {}), ...(reassigned ? { assigned: { before: [...old.bot_ids], after: [...next.bot_ids] } } : {}) })
  }
  for (const old of before) if (!after.some(item => item.id === old.id))
    out.push({ id: old.id, title: old.title, kind: "removed", lines: charterLineDiff(body(old.text), "").filter(line => line.kind !== "added"), assigned: { before: [...old.bot_ids], after: [] } })
  return out
}
