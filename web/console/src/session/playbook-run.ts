/** Metadata follows the complete launch message in the durable Session to-do. */
const MARKER = "\n\n[Clawdline-Playbook "
const END = "]"

export function playbookTodoText(id: string, title: string, message: string): string {
  return message + MARKER + id + " " + encodeURIComponent(title) + END
}

export function playbookRunOf(text: string): { id: string; title: string; message: string } | null {
  const at = text.lastIndexOf(MARKER)
  if (at < 0 || !text.endsWith(END)) return null
  const data = text.slice(at + MARKER.length, -END.length)
  const split = data.indexOf(" ")
  if (split < 1) return null
  const id = data.slice(0, split)
  if (!/^[0-9a-f-]{36}$/.test(id)) return null
  try {
    const title = decodeURIComponent(data.slice(split + 1))
    return title ? { id, title, message: text.slice(0, at) } : null
  } catch { return null }
}
