import { sessionSelectionIdentity } from "../legacy/js/session/selection.js"

type Row = { id: string; sessionId?: unknown } | null
const prefix = "clawdline.codegraph-guidance:"

/** Browser preference, scoped like a composer draft to one machine and conversation. */
export function codegraphGuidanceKey(row: Row): string {
 if (typeof row?.sessionId !== "string" || !row.sessionId) return ""
 const target = sessionSelectionIdentity(row)
 return target ? prefix + JSON.stringify([target.machine, target.session, row.sessionId]) : ""
}
export function codegraphGuidanceEnabled(row: Row): boolean {
 const key = codegraphGuidanceKey(row)
 try { return !!key && localStorage.getItem(key) === "on" } catch { return false }
}
export function setCodegraphGuidance(row: Row, enabled: boolean): void {
 const key = codegraphGuidanceKey(row)
 if (!key) throw new Error("unknown_conversation")
 if (enabled) localStorage.setItem(key, "on")
 else localStorage.removeItem(key)
}

/** Capture the choice in the accepted message; queued sends and retries keep those exact bytes. */
export function messageWithCodegraphGuidance(text: string, instructions: string, enabled: boolean): string {
 if (!enabled || text.trimStart().startsWith("/")) return text
 return text ? text + "\n\n" + instructions : instructions
}
