// Plain-text operations on a team charter. The charter is free Markdown; the
// only structure the screens rely on is a line starting with "## ", which
// begins a section. Text before the first such line is its own section.

export interface CharterSection { heading: string; text: string }

const headingLine = /^## /

/** Splits at "## " lines; joining the parts' text reproduces the input exactly. */
export function charterSections(text: string): CharterSection[] {
  const lines = text.split("\n"), sections: { heading: string; lines: string[] }[] = []
  for (const line of lines) {
    if (headingLine.test(line) || !sections.length) sections.push({ heading: headingLine.test(line) ? line.slice(3).trim() : "", lines: [line] })
    else sections[sections.length - 1].lines.push(line)
  }
  return sections.map(section => ({ heading: section.heading, text: section.lines.join("\n") }))
}

/** A blank preamble before the first heading is kept for joining but never shown. */
export const shownCharterSection = (section: CharterSection) => !!section.heading || !!section.text.trim()

/** The whole document with one section's text replaced. */
export function replaceCharterSection(text: string, index: number, replacement: string): string {
  const sections = charterSections(text)
  const parts = sections.map((section, i) => i === index ? replacement.replace(/\n+$/, "") + trailing(section.text) : section.text)
  return parts.join("\n")
}

const trailing = (text: string) => text.match(/\n*$/)?.[0] ?? ""

export type DiffLine = { kind: "same" | "removed" | "added"; text: string }

/** A line diff by longest common subsequence. */
export function charterLineDiff(before: string, after: string): DiffLine[] {
  const a = before.split("\n"), b = after.split("\n")
  const n = a.length, m = b.length
  const table: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0))
  for (let i = n - 1; i >= 0; i--) for (let j = m - 1; j >= 0; j--)
    table[i][j] = a[i] === b[j] ? table[i + 1][j + 1] + 1 : Math.max(table[i + 1][j], table[i][j + 1])
  const out: DiffLine[] = []
  let i = 0, j = 0
  while (i < n && j < m) {
    if (a[i] === b[j]) { out.push({ kind: "same", text: a[i] }); i++; j++ }
    else if (table[i + 1][j] >= table[i][j + 1]) out.push({ kind: "removed", text: a[i++] })
    else out.push({ kind: "added", text: b[j++] })
  }
  while (i < n) out.push({ kind: "removed", text: a[i++] })
  while (j < m) out.push({ kind: "added", text: b[j++] })
  return out
}

export interface ChangedSection { heading: string; lines: DiffLine[] }

/**
 * The sections a change touches, each with its line diff. Sections are paired
 * by heading where one exists in both, so a reordering or an added section
 * shows as that section rather than as a shift of every later one.
 */
export function changedCharterSections(before: string, after: string): ChangedSection[] {
  const old = charterSections(before), next = charterSections(after)
  const used = new Set<number>(), out: ChangedSection[] = []
  next.forEach((section, index) => {
    let match = old.findIndex((candidate, i) => !used.has(i) && candidate.heading === section.heading)
    if (match < 0 && !section.heading && index === 0 && old[0] && !old[0].heading && !used.has(0)) match = 0
    if (match >= 0) used.add(match)
    const previous = match >= 0 ? old[match].text : ""
    if (previous.replace(/\n+$/, "") !== section.text.replace(/\n+$/, ""))
      out.push({ heading: section.heading, lines: trimDiff(charterLineDiff(previous.replace(/\n+$/, ""), section.text.replace(/\n+$/, ""))) })
  })
  old.forEach((section, index) => {
    if (!used.has(index)) out.push({ heading: section.heading, lines: charterLineDiff(section.text.replace(/\n+$/, ""), "").filter(line => line.kind !== "added" || line.text) })
  })
  return out
}

// An empty added line from diffing against nothing is noise.
const trimDiff = (lines: DiffLine[]) => lines.filter(line => !(line.kind !== "same" && !line.text.trim() && lines.length > 1))

/** A first version from the five starter headings, with the Project's Bots listed under the first. */
export function charterTemplate(headings: readonly string[], starters: readonly string[], botNames: readonly string[], separator: string): string {
  return headings.map((heading, index) => {
    const body = index === 0 && botNames.length ? botNames.map(name => `- ${name}${separator}`).join("\n") : starters[index] ?? ""
    return `## ${heading}\n\n${body}`.replace(/\n+$/, "")
  }).join("\n\n") + "\n"
}
