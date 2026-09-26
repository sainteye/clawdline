export type ConnectionLightState = "offline" | "live" | "retrying" | "connecting"

/**
 * The stream and health are two witnesses. A stream alone cannot prove the
 * selected host answered: under Cloud it is only the browser's relay socket.
 */
export function connectionLightState(
  streamLive: boolean,
  healthAnswered: boolean,
  healthFailed: boolean,
): ConnectionLightState {
  if (healthFailed) return "offline"
  if (healthAnswered && streamLive) return "live"
  if (healthAnswered) return "retrying"
  return "connecting"
}

/** The light as the header draws it: its state, its word, and its tip. */
export interface ConnectionLight {
  state: ConnectionLightState
  label: string
  tip: string
  /** Asks the host again, what pressing the light did (`detail-actions.js`). */
  onRetry: () => void
}

/**
 * The word and tip of `renderConn` (`view/list.js`): "streaming from the app ·
 * version" when live and "not connected — press to retry" otherwise. Both the
 * daemon's own light and the Cloud machine switcher read them here, so a
 * machine's state is said in one set of words wherever it is drawn.
 */
export function connectionLightWords(
  state: ConnectionLightState,
  words: Record<string, string>,
  version: unknown,
): { label: string; tip: string } {
  const label =
    state === "offline" ? words.webConnOffline : state === "live" ? words.webConnLive : words.webConnConnecting
  const tip =
    state === "live"
      ? words.webConnTipLive + (typeof version === "string" && version ? " · " + version : "")
      : words.webConnTipDown
  return { label: label || state, tip }
}
