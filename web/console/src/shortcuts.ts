/** Shared by the keyboard handler, discoverable hints and the cheat sheet. */
export const SHORTCUTS = {
  sessions: { key: "k", modifier: true },
  board: { key: "b", modifier: true },
  sidebar: { key: "\\", modifier: true },
  help: { key: "/", modifier: true },
  start: { key: "n", modifier: false },
  task: { key: "t", modifier: false },
  voice: { key: "v", modifier: false },
  compose: { key: "c", modifier: false },
  attention: { key: "a", modifier: false },
} as const

export type Shortcut = keyof typeof SHORTCUTS

export function macKeyboard(platform = navigator.platform): boolean {
  return /Mac|iPhone|iPad|iPod/i.test(platform)
}

export function shortcutLabel(id: Shortcut, mac = macKeyboard()): string {
  const { key, modifier } = SHORTCUTS[id]
  return (modifier ? mac ? "⌘" : "Ctrl+" : "") + key.toUpperCase()
}

export function shortcutAria(id: Shortcut, mac = macKeyboard()): string {
  const { key, modifier } = SHORTCUTS[id]
  return (modifier ? mac ? "Meta+" : "Control+" : "") + key
}

type Key = Pick<KeyboardEvent, "key" | "code" | "metaKey" | "ctrlKey" | "altKey" | "shiftKey" | "isComposing" | "defaultPrevented">

/** Editor formatting, IME composition and controls keep their own keys. */
export function matchShortcut(event: Key, mac: boolean, editing: boolean, control: boolean): Shortcut | null {
  if (event.defaultPrevented || event.isComposing || event.altKey) return null
  const modified = mac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey
  if (!event.metaKey && !event.ctrlKey && event.key === "?" && !editing) return "help"
  if (event.shiftKey) return null
  for (const [id, binding] of Object.entries(SHORTCUTS)) {
    if (binding.modifier ? !modified : event.metaKey || event.ctrlKey || editing || control) continue
    if (editing && id === "board") continue
    // Punctuation keeps working with a non-Latin input source selected.
    const punctuation = binding.key === "/" ? "Slash" : binding.key === "\\" ? "Backslash" : null
    if (event.key.toLowerCase() === binding.key || (punctuation && event.code === punctuation)) return id as Shortcut
  }
  return null
}
