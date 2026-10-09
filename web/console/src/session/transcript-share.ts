/*
 * The transcript page the pane last read, for the other panel that wants the
 * same entries.
 *
 * The "my messages" sheet (`UserMessages.tsx`) used to read the identical
 * `/v1/transcript?limit=200` route on a 4-second poll of its own while open —
 * a second copy of what the pane on the same screen had just read. It reads
 * this instead, and only reads for itself when the pane is not reading that
 * session (a provider subagent's page is open, or none is).
 *
 * Nothing is imported at run time, so `node --test` loads this file as it is.
 */
import type { TranscriptPage } from "@clawdline/contract"

type Listener = () => void

export class SharedTranscripts {
  private pages = new Map<string, TranscriptPage>()
  private listeners = new Set<Listener>()

  /** The page the pane holds for `session`, or null when it holds none. */
  get(session: string): TranscriptPage | null {
    return this.pages.get(session) ?? null
  }

  publish(session: string, page: TranscriptPage): void {
    if (this.pages.get(session) === page) return
    this.pages.set(session, page)
    this.changed()
  }

  /** The pane stopped reading `session`. */
  forget(session: string): void {
    if (!this.pages.delete(session)) return
    this.changed()
  }

  subscribe = (fn: Listener): (() => void) => {
    this.listeners.add(fn)
    return () => {
      this.listeners.delete(fn)
    }
  }

  private changed(): void {
    for (const fn of this.listeners) fn()
  }
}

export const sharedTranscripts = new SharedTranscripts()
