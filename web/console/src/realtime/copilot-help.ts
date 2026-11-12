/**
 * The voice copilot's answer to "how do I use Clawdline": the sections of the
 * user guide (`docs/user/*.md`) that a question is about. The guide itself is
 * loaded only when asked (`copilot-help-docs.ts`); this file only picks, so
 * `node --test` loads it as it is.
 */

/** How many guide sections one answer carries. */
export const HELP_SECTIONS = 3
/** How much of each section is carried; a longer one is cut at a line. */
export const HELP_SECTION_CHARS = 2500

export interface HelpDoc { name: string; text: string }
export interface HelpSection { doc: string; heading: string; text: string }

/** The guide's sections, one per heading, in the person's language where the guide has it. */
export function helpSections(docs: readonly HelpDoc[], language: string): HelpSection[] {
  const chinese = /^zh/i.test(language)
  const variants = new Map<string, { en?: HelpDoc; zh?: HelpDoc }>()
  for (const d of docs) {
    const zh = d.name.endsWith('.zh-TW')
    const base = d.name.replace(/\.zh-TW$/, '')
    const v = variants.get(base) ?? {}
    if (zh) v.zh = d; else v.en = d
    variants.set(base, v)
  }
  const bases = new Map<string, HelpDoc>()
  for (const [base, v] of variants) {
    const pick = chinese ? (v.zh ?? v.en) : (v.en ?? v.zh)
    if (pick) bases.set(base, pick)
  }
  const out: HelpSection[] = []
  for (const d of bases.values()) {
    let heading = d.name
    let lines: string[] = []
    const flush = () => {
      const text = lines.join('\n').trim()
      if (text) out.push({ doc: d.name, heading, text })
      lines = []
    }
    for (const line of d.text.split('\n')) {
      const h = /^#{1,3}\s+(.+)$/.exec(line)
      if (h) { flush(); heading = h[1].trim(); continue }
      lines.push(line)
    }
    flush()
  }
  return out
}

/** The words of a question: Latin words, and every two adjacent Han characters. */
function terms(text: string): string[] {
  const lower = text.normalize('NFKC').toLowerCase()
  const out = new Set<string>()
  for (const w of lower.match(/[a-z0-9][a-z0-9_.-]{1,}/g) ?? []) out.add(w)
  for (const run of lower.match(/\p{Script=Han}+/gu) ?? []) {
    for (let i = 0; i + 1 < run.length; i++) out.add(run.slice(i, i + 2))
  }
  return [...out]
}

/**
 * The sections a question is about, best first: each word of the question
 * found in a section counts once, three times in its heading. Nothing found
 * is an empty list, so the copilot says the guide does not cover it.
 */
export function searchHelp(sections: readonly HelpSection[], question: string): HelpSection[] {
  const asked = terms(question)
  if (!asked.length) return []
  const scored = sections.map((s) => {
    const head = s.heading.toLowerCase()
    const body = s.text.toLowerCase()
    let score = 0
    for (const t of asked) score += (head.includes(t) ? 3 : 0) + (body.includes(t) ? 1 : 0)
    return { s, score }
  }).filter((x) => x.score > 0)
  scored.sort((a, b) => b.score - a.score)
  return scored.slice(0, HELP_SECTIONS).map(({ s }) => ({ ...s, text: clipAtLine(s.text, HELP_SECTION_CHARS) }))
}

function clipAtLine(text: string, max: number): string {
  if (text.length <= max) return text
  const cut = text.lastIndexOf('\n', max)
  return text.slice(0, cut > max / 2 ? cut : max) + '\n…'
}

/** The guide's table of contents: one id per section, and its page and heading. */
export function helpContents(sections: readonly HelpSection[]): string[] {
  return sections.map((s, i) => `s${i} ${s.doc} › ${s.heading}`)
}

/** The sections the copilot chose from the contents, by id, cut to their bound. */
export function helpRead(sections: readonly HelpSection[], ids: readonly string[]): HelpSection[] {
  const out: HelpSection[] = []
  for (const id of ids) {
    const m = /^s(\d+)$/.exec(id.trim())
    const s = m ? sections[Number(m[1])] : undefined
    if (s && !out.includes(s) && out.length < HELP_SECTIONS) out.push(s)
  }
  return out.map((s) => ({ ...s, text: clipAtLine(s.text, HELP_SECTION_CHARS) }))
}
