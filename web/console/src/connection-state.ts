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
