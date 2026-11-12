/** Adapted from Codync's destination composer. Composition never admits a send. */
export function composerEnter(event: { key: string; shiftKey?: boolean; altKey?: boolean; repeat?: boolean; isComposing?: boolean; keyCode?: number }, composing = false): "send" | "newline" | "ignore" {
  if (event.key !== "Enter" || event.isComposing || composing || event.keyCode === 229) return "ignore"
  if (event.shiftKey || event.altKey) return "newline"
  return event.repeat ? "ignore" : "send"
}
