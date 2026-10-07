// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogWord, currentCatalogTag } from "./catalog.ts"

/**
 * Punctuation that sits between catalog fragments and the values put into them.
 *
 * Much of the console's copy was cut out of Chinese sentences, so a fragment
 * often ends in an opening quote or a colon and the source supplied the rest:
 * `「`, `：`, `。`, `、`. Written into the source, those marks reached every
 * language, and English read `Answer received: “Yes」` and `Reason:Only…`.
 * Each mark is now chosen for the language being shown, and a closing quote or
 * bracket is the partner of the opening one the catalog fragment itself used,
 * because translations differ (`“`, `「`, `„`, `« `, `‘`).
 */

const PAIRS: Readonly<Record<string, string>> = {
  "「": "」", "『": "』", "“": "”", "‘": "’", "„": "“", "«": "»", "‹": "›",
  "（": "）", "(": ")", "〈": "〉", "《": "》", "【": "】",
}
const OPENERS: Readonly<Record<string, string>> = Object.fromEntries(
  Object.entries(PAIRS).filter(([open]) => open !== "„").map(([open, close]) => [close, open]))

/** Chinese and Japanese take full-width marks; every other shipped language takes Latin ones. */
export function fullWidthPunctuation(): boolean {
  return /^(zh|ja)/u.test(currentCatalogTag())
}

/**
 * The closing mark for the last opening quote or bracket in `text`, or "" when
 * none is open. A French `« ` gets its ` »` with the same space inside.
 */
export function closingMark(text: string): string {
  const trimmed = text.trimEnd()
  const open = trimmed.at(-1) ?? ""
  const close = PAIRS[open]
  if (!close) return ""
  return text.length > trimmed.length ? ` ${close}` : close
}

/**
 * The opening mark for a fragment that closes one it did not open, such as
 * `lines unchanged)` or `” is offline.`, or "" when it closes none. A French
 * ` »` gets its `« ` with the same space inside.
 */
export function openingMark(text: string): string {
  const open: string[] = []
  for (let index = 0; index < text.length; index++) {
    const mark = text[index]
    const opener = OPENERS[mark]
    if (opener && open.length && PAIRS[open[open.length - 1]] === mark) { open.pop(); continue }
    if (opener) return index > 0 && /\s/u.test(text[index - 1]) ? `${opener} ` : opener
    if (PAIRS[mark]) open.push(mark)
  }
  return ""
}

/** A catalog label that ends in a Latin colon or comma keeps a space before its value; a full-width one carries its own. */
export function spacedLabel(text: string): string {
  return /[:;,]$/u.test(text) ? `${text} ` : text
}

/** `catalogWord` for a label that a value follows directly. */
export function catalogLabel(domain: string, key: string): string {
  return spacedLabel(catalogWord(domain, key))
}

/** `label：value` in Chinese and Japanese, `label: value` elsewhere. */
export function labelled(label: string, value: string): string {
  return `${label}${colonMark()}${value}`
}

/** The colon that ends a label, with the space a Latin one needs. */
export function colonMark(): string {
  return fullWidthPunctuation() ? "：" : ": "
}

/** The space a Latin sentence keeps between two fragments; Chinese and Japanese run them together. */
export function wordGap(): string {
  return fullWidthPunctuation() ? "" : " "
}

/** The separator between the items of an inline list. */
export function listSeparator(): string {
  return fullWidthPunctuation() ? "、" : ", "
}

/** The separator between two clauses of one line. */
export function clauseSeparator(): string {
  return fullWidthPunctuation() ? "，" : ", "
}

/** The mark that ends a sentence. */
export function fullStop(): string {
  return fullWidthPunctuation() ? "。" : "."
}

/** `text` in parentheses, with the space a Latin parenthesis takes before it. */
export function parenthesized(text: string): string {
  return fullWidthPunctuation() ? `（${text}）` : ` (${text})`
}

/** A title quoted inside a sentence. */
export function quotedTitle(text: string): string {
  return fullWidthPunctuation() ? `〈${text}〉` : `“${text}”`
}
