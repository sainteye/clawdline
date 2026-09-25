/**
 * What the Send button on a direct to-do says, and whether it can be pressed.
 *
 * The daemon refuses a second delivery of an unread to-do for
 * `app.DirectTodoResendAfter` (two minutes), so a double tap or the same press
 * from a second screen does not type the words twice. Past it — or once the
 * Session has read its queue — the person can always send again: a Session
 * that never reads its queue must not leave the row unsendable for good.
 */
export const DIRECT_TODO_RESEND_AFTER_SECONDS = 120

export type TodoSend =
  | { kind: "send"; label: "Send" }
  | { kind: "again"; label: "再次 Send" }
  | { kind: "wait"; label: "再次 Send"; until: number }
  | { kind: "none" }

export function todoSend(todo: { completed_at?: number | null; sent_at: number | null; read_at: number | null }, now: number): TodoSend {
  if (todo.completed_at) return { kind: "none" }
  if (!todo.sent_at && !todo.read_at) return { kind: "send", label: "Send" }
  if (todo.read_at) return { kind: "again", label: "再次 Send" }
  const until = (todo.sent_at ?? 0) + DIRECT_TODO_RESEND_AFTER_SECONDS
  return now < until ? { kind: "wait", label: "再次 Send", until } : { kind: "again", label: "再次 Send" }
}
