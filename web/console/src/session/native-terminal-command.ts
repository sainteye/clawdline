/** All native slash commands share the same continuation, including future names. */
export function nativeTerminalCommand(text:string,assistant:string|undefined,backend:string|undefined,pictures=0):boolean {
 return !pictures && (assistant==="claude"||assistant==="codex") && (backend==="tmux"||backend==="iterm") && !/^\/resume(?:[ \t]|$)/.test(text.trim()) && /^\/[^\s/]+(?:\s|$)/.test(text.trim())
}
