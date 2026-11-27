// The charter's "what to ask you" section as an ask policy: two lists side by
// side (do without asking, always ask), a list of exceptions and one line on
// how to ask. The data stays ordinary charter Markdown, so Bots read the same
// text; a section is an ask policy only when its body is exactly four "### "
// sub-headings in one language, in order, each followed by a "- " list (the
// last by one line). Any other shape is not one, and renders as before.

// @ts-expect-error Node's strip-types runner loads source extensions.
import { sectionParts, type CharterSection } from "./charter-text.ts"

export type AskLanguage = "zh" | "en"
export interface AskPolicy { language: AskLanguage; free: string[]; ask: string[]; exceptions: string[]; how: string }
export type AskList = "free" | "ask" | "exceptions"

export const askHeadings: Record<AskLanguage, readonly [string, string, string, string]> = {
  zh: ["不用問，直接做", "一定要問", "例外狀況", "怎麼問"],
  en: ["No need to ask", "Always ask", "Exceptions", "How to ask"],
}

// The section headings the offer to convert free text appears under.
const askSectionHeadings = ["什麼要問你", "What to ask you", "What to ask you about"]
export const isAskSectionHeading = (heading: string) => askSectionHeadings.includes(heading.trim())
/** The sub-headings' language for converting a section: Chinese under a Chinese heading. */
export const askLanguageOf = (heading: string): AskLanguage => /[\u3400-\u9fff]/.test(heading) ? "zh" : "en"

/** The body of an ask-policy section, or null when the body has any other shape. */
export function parseAskPolicy(body: string): AskPolicy | null {
  const lines = body.split("\n").filter(line => line.trim() !== "")
  for (const language of ["zh", "en"] as const) {
    const parsed = parseIn(lines, askHeadings[language])
    if (parsed) return { language, ...parsed }
  }
  return null
}

function parseIn(lines: readonly string[], headings: readonly string[]) {
  const blocks: string[][] = []
  for (const line of lines) {
    if (line.startsWith("### ")) {
      if (line.slice(4).trim() !== headings[blocks.length]) return null
      blocks.push([])
    } else if (!blocks.length) return null
    else blocks[blocks.length - 1].push(line)
  }
  if (blocks.length !== headings.length) return null
  const lists = blocks.slice(0, 3).map(block => block.map(line => /^- (.*\S.*)$/.exec(line)?.[1].trim()))
  if (lists.some(list => list.some(item => item === undefined))) return null
  const how = blocks[3]
  if (how.length > 1 || how.some(line => /^[-*#>]|^\d+\.|^\s/.test(line))) return null
  const [free, ask, exceptions] = lists as string[][]
  return { free, ask, exceptions, how: how[0]?.trim() ?? "" }
}

/** An ask policy for a whole section, when the section holds one. */
export function sectionAskPolicy(section: CharterSection): AskPolicy | null {
  if (!section.heading) return null
  return parseAskPolicy(sectionParts(section).body)
}

/** The section body for a policy; empty items are dropped, an empty list keeps its heading. */
export function serializeAskPolicy(policy: AskPolicy): string {
  const [free, ask, exceptions, how] = askHeadings[policy.language]
  const list = (heading: string, items: readonly string[]) => {
    const kept = items.map(item => item.replace(/[\r\n]+/g, " ").trim()).filter(Boolean)
    return [`### ${heading}`, ...(kept.length ? ["", ...kept.map(item => `- ${item}`)] : [])].join("\n")
  }
  const line = policy.how.replace(/[\r\n]+/g, " ").trim()
  return [list(free, policy.free), list(ask, policy.ask), list(exceptions, policy.exceptions), line ? `### ${how}\n\n${line}` : `### ${how}`].join("\n\n")
}

const prefixes: Record<AskLanguage, { free: RegExp; ask: RegExp; exception: RegExp; how: RegExp }> = {
  zh: { free: /^不用問[^：:]*[：:]\s*/, ask: /^一定要問[：:]\s*/, exception: /^例外(?:狀況)?[：:]\s*/, how: /^要問時/ },
  en: { free: /^no need to ask[^:]*:\s*/i, ask: /^always ask[^:]*:\s*/i, exception: /^exceptions?:\s*/i, how: /^(?:when (?:you )?ask|to ask)/i },
}

/**
 * A best-effort policy from free text, for the person to check and save. A
 * line starting "不用問…：" or "一定要問：" becomes items, split at "；" and,
 * when that leaves one item, at "、"; "例外：" is one exception; a line starting
 * "要問時" is how to ask. Every other line is kept whole as an exception, so no
 * sentence is lost.
 */
export function askPolicyFromText(body: string, language: AskLanguage): AskPolicy {
  const words = prefixes[language]
  const policy: AskPolicy = { language, free: [], ask: [], exceptions: [], how: "" }
  for (const raw of body.split("\n")) {
    const line = raw.replace(/^\s*(?:[-*]|\d+\.)\s+/, "").trim()
    if (!line || /^#{1,6}\s/.test(line)) continue
    const free = words.free.exec(line), ask = words.ask.exec(line), exception = words.exception.exec(line)
    if (free) policy.free.push(...splitItems(line.slice(free[0].length), language))
    else if (ask) policy.ask.push(...splitItems(line.slice(ask[0].length), language))
    else if (exception) policy.exceptions.push(line.slice(exception[0].length))
    else if (words.how.test(line) && !policy.how) policy.how = line
    else policy.exceptions.push(line)
  }
  return policy
}

function splitItems(text: string, language: AskLanguage): string[] {
  // Full-width marks are escaped: \uFF1B is the semicolon, \u3001 the enumeration comma, \u3002 the full stop.
  const [strong, weak] = language === "zh" ? [/[\uFF1B;]/, /\u3001/] : [/;/, /,/]
  const parts = text.split(strong).map(item => item.trim()).filter(Boolean)
  const items = parts.length > 1 ? parts : text.split(weak).map(item => item.trim()).filter(Boolean)
  return items.map(item => item.replace(/[\u3002.]$/, "").trim()).filter(Boolean)
}

/** The rows after Enter in row `index`: a new empty row below it, which takes the focus. */
export function insertAskRow(items: readonly string[], index: number): { items: string[]; focus: number } {
  const at = Math.min(Math.max(index + 1, 0), items.length)
  return { items: [...items.slice(0, at), "", ...items.slice(at)], focus: at }
}

/** The rows after removing row `index`; the focus goes to the row before it, or the first. */
export function removeAskRow(items: readonly string[], index: number): { items: string[]; focus: number } {
  if (index < 0 || index >= items.length) return { items: [...items], focus: -1 }
  const next = [...items.slice(0, index), ...items.slice(index + 1)]
  return { items: next, focus: next.length ? Math.max(index - 1, 0) : -1 }
}

/** The rows an ask list shows: an empty list shows one empty row to type in. */
export const shownAskRows = (items: readonly string[]) => items.length ? [...items] : [""]
