/** A run of text to draw as a mention chip; offsets are UTF-16 units of text. */
export type MarkdownMention = { start: number; end: number; id: string; title?: string }

const escape = (s: string) => s.replace(/[&<>"']/g, c => `&#${c.charCodeAt(0)};`)

// Each run is swapped for a private-use placeholder before Markdown and for
// its chip after, so the chip covers exactly the source's run. A run that
// Markdown put inside a tag's attributes, code or a link goes back as the
// text it was: a chip there would be copied with the code or cut the address.
export function markdownWithMentions(text: string, mentions: readonly MarkdownMention[], render: (source: string) => string): string {
  if (!mentions.length) return render(text)
  let source = "", at = 0
  mentions.forEach((m, i) => { source += text.slice(at, m.start) + `\uE000${i}\uE001`; at = m.end })
  source += text.slice(at)
  const written = (i: string) => { const m = mentions[Number(i)]; return m ? escape(text.slice(m.start, m.end)) : "" }
  const literal: string[] = []
  // The renderer escapes every < and > of the message, so a tag is its own.
  return render(source).replace(/<(\/?)([a-z0-9]+)[^>]*>|\uE000(\d+)\uE001/gi, (all: string, closing: string, tag: string | undefined, i: string | undefined) => {
    if (i !== undefined) {
      const m = mentions[Number(i)]
      if (!m || literal.length) return written(i)
      return `<span class="pa-mention-chip" data-mention="${escape(m.id)}"${m.title ? ` title="${escape(m.title)}"` : ""}>${written(i)}</span>`
    }
    const name = tag!.toLowerCase()
    if (name === "pre" || name === "code" || name === "a") {
      if (!closing) literal.push(name)
      else if (literal.lastIndexOf(name) >= 0) literal.length = literal.lastIndexOf(name)
    }
    return all.replace(/\uE000(\d+)\uE001/g, (_, n: string) => written(n))
  })
}
