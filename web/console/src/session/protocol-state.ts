import type { SessionProtocol } from "@clawdline/contract"

const phaseKeys: Readonly<Record<string,string>> = {
  control_pending: "controlPending", starting: "starting", idle: "idle", prompt_pending: "promptPending",
  permission_pending: "permissionPending", cancel_requested: "cancelRequested",
  request_failed: "requestFailedPhase", closed: "closed", not_connected: "notConnected",
}

/** Protocol receipt state; never derive completion or responsiveness from age. */
export function protocolStateHTML(
  state: SessionProtocol,
  word: (key:string)=>string,
  escape: (text:string)=>string,
  clock: (seconds:number)=>string,
): string {
  const note = (text:string) => '<p class="note">' + escape(text) + '</p>'
  let html = '<div class="acp-state" data-acp-phase="' + escape(state.phase) + '">'
  html += note(word(state.operation === "usage" && state.phase === "prompt_pending" ? "quotaPending" : phaseKeys[state.phase] || "notConnected"))
  if (state.last_received_at) html += note(word("lastReceived") + " · " + clock(state.last_received_at / 1e9))
  if (state.operation !== "usage" && state.completed_at && state.stop_reason) html += note(word("stopReason") + " · " + state.stop_reason)
  if (state.rpc_error !== undefined && state.rpc_error !== null) html += note(word("requestFailed") + " · " + state.rpc_error)
  html += note(word(state.trace_error ? "traceGap" : state.trace_enabled ? "traceEnabled" : "traceDisabled"))
  return html + '</div>'
}
