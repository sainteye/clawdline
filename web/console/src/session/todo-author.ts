/**
 * Who wrote a Session's direct to-do.
 *
 * A row the Session added itself, on the person's request, names the
 * Session's own conversation id as its author (`created_by`). It was never
 * sent, so the sent/read marks would say something false about it; the row
 * carries the "added by Session" label in their place. A person's row names
 * the person's actor and keeps its receipts.
 */
export function addedBySession(todo: { created_by?: string }, conversation: string | undefined): boolean {
  return !!conversation && todo.created_by === conversation
}
