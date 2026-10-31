/** All native slash commands share the same continuation, including future names. */
export function nativeTerminalCommand(text:string,assistant:string|undefined,backend:string|undefined,pictures=0):boolean {
 return !pictures && (assistant==="claude"||assistant==="codex") && (backend==="tmux"||backend==="iterm") && !/^\/resume(?:[ \t]|$)/.test(text.trim()) && /^\/[^\s/]+(?:\s|$)/.test(text.trim())
}

/** Whether this Session has an existing CLI the Terminal panel can show and
 * operate. Gemini's standalone CLI qualifies; its slash commands do not route
 * here, because Clawdline gives them their own meaning (`antigravitySlashDraft`). */
export function nativeTerminalAvailable(assistant:string|undefined,backend:string|undefined):boolean {
 return (assistant==="claude"||assistant==="codex"||assistant==="antigravity") && (backend==="tmux"||backend==="iterm")
}
