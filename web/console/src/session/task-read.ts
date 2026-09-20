/*
 * Saying when the dispatched-work list could not be read.
 *
 * That list is what the session list's indent, the child chip and the detail
 * header are all drawn from, and the read that fetches it ended in
 * `.catch(() => {})`. One half of that is right and stays: a failed read must
 * not put an error in front of anybody — one missing indent is not worth a
 * banner on a phone — and the last list standing is better than chips that
 * vanish and say every task is over.
 *
 * The other half is what cost the person their hierarchy. The hosted console
 * refused this read to itself, 501 `cloud_not_carried`, on every poll for as
 * long as it was hosted; the page said nothing, the Mac's log held nothing
 * because nothing had been asked of it, and the only symptom was a flat list
 * on a phone. A fault that produces no sentence anywhere is found by reading
 * the source, and only by somebody who already suspects it.
 *
 * So it is said, and said once. A ten-second lane that keeps failing would
 * otherwise write six lines a minute, which is a log nobody reads either — so
 * the same reason is said once, a different reason is a different fact and is
 * said again, and coming back is said too, because a trail that records only
 * breaking leaves a reader unable to tell a fault that is over from one that
 * is not.
 *
 * On the Cloud path this is the second of two records: the seam already notes
 * its own refusal in the page's log (`cloud/relay-reader.ts`, `SeamRow`). This
 * one is the half that also covers a console served by the daemon itself,
 * where there is no seam and so no log at all.
 *
 * Nothing is imported at run time, so `node --test` loads this file as it is.
 */

/**
 * The name for why a read failed: the daemon's own refusal code where there is
 * one, and otherwise what kind of failure it was.
 *
 * A `RefusalError` carries `code`, which is the only part of a refusal
 * anything may branch on (`core/src/refusal.ts`), and a `TransportError` means
 * nobody answered at all. Read by shape rather than by `instanceof`, because
 * nothing here is imported at run time.
 */
export function taskReadReason(error: unknown): string {
  const failure = error as { code?: unknown; name?: unknown } | null | undefined
  if (failure && typeof failure.code === "string" && failure.code) return failure.code
  if (failure && failure.name === "TransportError") return "unanswered"
  if (failure && typeof failure.name === "string" && failure.name) return failure.name
  return "unknown"
}

/** Where a line goes. The default is the browser's console; a test hands in its own. */
export type Say = (line: string) => void

/**
 * One page's account of whether the dispatched-work list is being read.
 *
 * It holds one thing: the reason the last line stated, or "" while the list is
 * arriving. Nothing else on the page reads it and nothing is drawn from it —
 * it exists so that the failure leaves a trace, which is the one thing it did
 * not do.
 */
export class TaskReadTrail {
  private said = ""
  private readonly say: Say

  constructor(say?: Say) {
    this.say = say ?? ((line) => console.warn(line))
  }

  /** The reason currently stated, or "" while the list is arriving. */
  get reason(): string {
    return this.said
  }

  /** A read failed. The list on screen stands; this is the only thing that happens. */
  failed(error: unknown): void {
    const reason = taskReadReason(error)
    if (reason === this.said) return
    this.said = reason
    this.say(
      "clawdline: the dispatched-work list could not be read (" +
        reason +
        "); the sessions are drawn without it, so a child may stand where its root put it rather than under it",
    )
  }

  /** A read arrived. Said only when it is the end of something that was said. */
  arrived(): void {
    if (!this.said) return
    const was = this.said
    this.said = ""
    this.say("clawdline: the dispatched-work list is being read again (was " + was + ")")
  }
}

/** This page's own trail. One per page, so a reason is said once however often the list is read. */
export const taskReads = new TaskReadTrail()
