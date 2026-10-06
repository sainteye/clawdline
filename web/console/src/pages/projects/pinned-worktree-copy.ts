// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogWord, catalogWordLanguage } from "../../catalog.ts"
import type { WorktreeSourceRow } from "../../legacy/projects-bridge.js"
import sources from "./pinned-worktree-copy.json" with { type: "json" }

type Pair = readonly string[]
const pairs = sources as Record<string, Pair>
const source = (key: string, value: string): boolean => pairs[key]?.includes(value) ?? false
const word = (key: string): string => catalogWord("worktree", key)
const filled = (value: unknown): boolean => typeof value === "string" && !!value.trim()

const tagKeys = ["inUse", "landedIdentical", "unlanded", "mixed", "taskTemporary", "staleMetadata", "incompleteEvidence"]
const factKeys = ["staged", "modified", "untracked", "diskUsed", "created", "started", "ended", "taskState", "originSession", "localObservation", "canonicalTarget", "target", "storageObservation"]
const statusKeys = ["showingPrevious", "refreshed", "previousObservation", "noWorktrees", "lastObserved", "lifecycleUnavailable", "refreshing", "reading", "refreshBusy", "inventoryNotObserved", "statusUnreadable"]
const countKeys = ["countWorktrees", "countActive", "countStaged", "countModified", "countUntracked", "countUnknown"]

function write(node: HTMLElement | null, next: string, key?: string): void {
  if (!node || node.textContent === next) return
  node.textContent = next
  if (key && catalogWordLanguage("worktree", key) === "en") node.lang = "en"
}

/** Translate only an exact fixed label from the pinned renderer. */
export function fixedWorktreeWord(value: string, keys: readonly string[]): { text: string; key: string } | null {
  for (const key of keys) if (source(key, value)) return { text: word(key), key }
  return null
}

function fixed(node: HTMLElement | null, keys: readonly string[]): void {
  if (!node) return
  const result = fixedWorktreeWord(node.textContent ?? "", keys)
  if (result) write(node, result.text, result.key)
}

function prefix(node: HTMLElement | null, key: string): void {
  if (!node) return
  const value = node.textContent ?? ""
  for (const old of pairs[key] ?? []) {
    if (value.startsWith(old)) {
      write(node, word(key) + value.slice(old.length))
      return
    }
  }
}

export function localizeWorktreeStatus(value: string): string {
  let result = value
  for (const key of statusKeys) {
    for (const old of pairs[key] ?? []) result = result.replace(old, word(key))
  }
  return result
}

function localizeSummary(value: string): string {
  return value.split(" · ").map((part, index) => {
    const key = countKeys[index]
    if (!key) return part
    for (const old of pairs[key] ?? []) {
      if (part.endsWith(` ${old}`)) return part.slice(0, -old.length) + word(key)
    }
    return part
  }).join(" · ")
}

/** Runs in the mutation microtask before paint. It leaves the original DOM and listeners intact. */
export function decoratePinnedWorktree(root: HTMLElement, rowFor: (id: string) => WorktreeSourceRow | undefined): void {
  const summary = root.querySelector<HTMLElement>("#project-worktree-summary")
  if (summary) write(summary, localizeSummary(summary.textContent ?? ""))
  const status = root.querySelector<HTMLElement>("#project-worktree-status")
  if (status) write(status, localizeWorktreeStatus(status.textContent ?? ""))
  for (const heading of root.querySelectorAll<HTMLElement>(".worktree-group-title")) {
    fixed(heading, ["needsAttention", "afterDelivery", "incompleteEvidence"])
  }
  for (const card of root.querySelectorAll<HTMLElement>(".worktree-card[data-worktree-id]")) {
    const row = rowFor(card.dataset.worktreeId ?? "")
    if (!row) continue
    if (!filled(row.branch)) fixed(card.querySelector(".worktree-branch"), ["noBranch"])
    if (!filled(row.owner?.title)) fixed(card.querySelector(".worktree-owner, .worktree-owner-link"), ["ownerUnknown"])
    if (!filled(row.context?.purpose)) fixed(card.querySelector(".worktree-purpose"), ["purposeUnknown"])
    if (!filled(row.context?.originSession?.title)) {
      const origin = card.querySelectorAll<HTMLElement>(".worktree-lifecycle-facts .worktree-fact")
      fixed(origin[4]?.querySelector(".worktree-fact-value, .worktree-origin-link") ?? null, ["originUnknown"])
    }
    if (!filled(row.context?.state)) {
      const lifecycle = card.querySelectorAll<HTMLElement>(".worktree-lifecycle-facts .worktree-fact")
      fixed(lifecycle[3]?.querySelector(".worktree-fact-value") ?? null, ["unknown"])
    }
    for (const label of card.querySelectorAll<HTMLElement>(".worktree-tag")) fixed(label, tagKeys)
    for (const label of card.querySelectorAll<HTMLElement>(".worktree-fact-key")) fixed(label, factKeys)
    for (const value of card.querySelectorAll<HTMLElement>(".worktree-fact-value")) {
      if (value.closest(".worktree-lifecycle-facts") || value.closest(".worktree-observations") || value.closest(".worktree-counts")) {
        fixed(value, ["notObserved", "invalidTime"])
      }
    }
    for (const value of card.querySelectorAll<HTMLElement>(".worktree-observations .worktree-fact-value")) {
      const fact = value.closest<HTMLElement>(".worktree-fact")
      const key = fact?.querySelector<HTMLElement>(".worktree-fact-key")?.textContent
      const observations = [word("localObservation"), word("canonicalTarget"), ...pairs.localObservation, ...pairs.canonicalTarget]
      if (!key || !observations.includes(key)) continue
      for (const state of ["current", "stale", "unknown", "failed"]) {
        for (const old of pairs[state] ?? []) {
          if ((value.textContent ?? "").startsWith(old + " · ")) prefix(value, state)
        }
      }
      for (const old of pairs.notObserved) {
        const valueNow = value.textContent ?? ""
        if (valueNow.endsWith(" · " + old)) write(value, valueNow.slice(0, -old.length) + word("notObserved"))
      }
    }
    if (!filled(row.target)) {
      const facts = card.querySelectorAll<HTMLElement>(".worktree-observations .worktree-fact")
      fixed(facts[2]?.querySelector(".worktree-fact-value") ?? null, ["targetUnknown"])
    }
    fixed(card.querySelector(".worktree-cleanup-assessment"), ["cleanupEligible", "cleanupUnavailable"])
    prefix(card.querySelector(".worktree-current-status"), "now")
    prefix(card.querySelector(".worktree-next-owner"), "nextOwner")
  }
}
