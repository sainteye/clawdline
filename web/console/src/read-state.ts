/**
 * One read, without using a placeholder as its answer.
 *
 * `refused` means an answer came back and said no. `unanswered` means the
 * transport never produced a usable answer. Those two states need different
 * next steps, and neither is an authoritative empty collection.
 */
export type ReadState<T> =
  | { phase: "loading" }
  | { phase: "ready"; value: T }
  | { phase: "empty_authoritative"; value: T }
  | { phase: "refused"; error: unknown }
  | { phase: "unanswered"; error: unknown }

/** A successful answer is the only route to authoritative empty. */
export function readAnswer<T>(value: T, empty: boolean): ReadState<T> {
  return empty ? { phase: "empty_authoritative", value } : { phase: "ready", value }
}

/**
 * Keep a named refusal apart from a request that did not answer.
 *
 * Most console clients use `RefusalError` and `TransportError`. The schedules
 * bridge predates them but preserves the refusal code; its `offline` code is
 * the one transport failure it creates. Reading the small public shape keeps
 * this state helper usable in the dependency-free Node tests too.
 */
export function readFailure(error: unknown): ReadState<never> {
  const failure = error as { name?: unknown; code?: unknown } | null | undefined
  const namedRefusal = failure?.name === "RefusalError"
  const code = typeof failure?.code === "string" ? failure.code : ""
  if (namedRefusal || (code !== "" && code !== "offline")) return { phase: "refused", error }
  return { phase: "unanswered", error }
}

/** The value from a successful non-empty answer, and nothing from any other state. */
export function readValue<T>(state: ReadState<T>): T | null {
  return state.phase === "ready" || state.phase === "empty_authoritative" ? state.value : null
}

/** True only when a successful answer carries at least one usable value. */
export function readReady<T>(state: ReadState<T>): state is Extract<ReadState<T>, { phase: "ready" }> {
  return state.phase === "ready"
}

/** True for both successful non-empty and authoritative-empty answers. */
export function readAnswered<T>(
  state: ReadState<T>,
): state is Extract<ReadState<T>, { phase: "ready" | "empty_authoritative" }> {
  return state.phase === "ready" || state.phase === "empty_authoritative"
}
