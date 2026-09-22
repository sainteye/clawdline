/**
 * A terminal row can exist before Codex has written and exposed its rollout.
 * Once the row learns the provider conversation id, an `/info` answer taken
 * before that moment no longer answers the same amount of evidence.
 *
 * Empty remains empty: opening an untouched session must not start a refresh
 * loop while there is still no provider record to read.
 */
export function conversationBecameKnown(previous: string | undefined, current: string | undefined): boolean {
  return !!current && current !== previous
}

/** A held answer taken before (or for a different) provider conversation. */
export function factsMissConversation(
  current: string | undefined,
  held: { session?: { sessionId?: string } } | null | undefined,
): boolean {
  return !!current && !!held && held.session?.sessionId !== current
}
