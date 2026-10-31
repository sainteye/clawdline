/** All native slash commands share the same continuation, including future names. */
export function nativeTerminalCommand(text:string,assistant:string|undefined,backend:string|undefined,pictures=0):boolean {
 return !pictures && (assistant==="claude"||assistant==="codex") && (backend==="tmux"||backend==="iterm") && /^\/[^\s/]+(?:\s|$)/.test(text.trim())
}
