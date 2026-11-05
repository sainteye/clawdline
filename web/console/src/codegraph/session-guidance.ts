import instructions from "../../../../internal/domain/codegraphguidance/instructions.json" with { type: "json" }
import type { CodegraphGuidance } from "@clawdline/contract"
import { sessionSelectionIdentity } from "../legacy/js/session/selection.js"

type Row = { id: string; sessionId?: unknown } | null
/** Read only the old conversation's opt-in; never enumerate browser history. */
export function legacyCodegraphKey(row: Row): string {
 if (typeof row?.sessionId !== "string" || !row.sessionId) return ""
 const target = sessionSelectionIdentity(row)
 return target ? "clawdline.codegraph-guidance:" + JSON.stringify([target.machine, target.session, row.sessionId]) : ""
}
export function legacyCodegraphEnabled(row: Row): boolean {
 try { const key=legacyCodegraphKey(row);return !!key && localStorage.getItem(key)==="on" } catch {return false}
}
export function codegraphInstructions(language: string, enabled=true): string {
 const words=language==="zh-Hant" ? instructions["zh-Hant"] : instructions.en
 return enabled ? words.enabled : words.disabled
}
export function codegraphGuidanceStatus(g:CodegraphGuidance):string {
 if(g.state==="pending")return g.enabled?"pendingOnce":"pendingOff"
 if(g.state==="provided")return g.enabled?"providedOnce":"providedOff"
 if(g.state==="launch")return "launchProvided"
 if(g.state==="unknown")return "deliveryUnknown"
 if(g.state==="unavailable")return "guidanceUnavailable"
 return "guidanceOff"
}
