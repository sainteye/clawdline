// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogWord } from "../catalog.ts"

/**
 * The words dictated last and what the optional LLM cleanup did to them.
 * `polished` and `raw` are both present only when the cleanup replaced
 * Whisper's text; `showing` is which of the two is in the box now.
 */
export interface Dictated {
  status: string
  polished?: string
  raw?: string
  showing: "polished" | "raw"
}

const FALLBACK = new Set(["timeout", "unfaithful", "unavailable", "quota", "too_long", "failed"])

/** Nothing to say when cleanup was off or left the words as they were. */
export function dictatedFrom(said: string, polish?: { status: string; raw?: string }): Dictated | null {
  if (!polish) return null
  if (polish.status === "applied" && polish.raw) return { status: "applied", polished: said, raw: polish.raw, showing: "polished" }
  if (FALLBACK.has(polish.status)) return { status: polish.status, showing: "polished" }
  return null
}

export function polishWord(d: Dictated): string {
  if (d.status === "applied") return catalogWord("voicePolish", d.showing === "raw" ? "raw" : "applied")
  return catalogWord("voicePolish", FALLBACK.has(d.status) ? d.status : "failed")
}

/**
 * The box with the other version in place of the one showing. The last
 * occurrence is swapped, because dictation appends. A box where that text is no
 * longer present has been edited, and there is nothing safe to swap.
 */
export function swapDictation(box: string, d: Dictated): { text: string; dictated: Dictated } | null {
  if (!d.polished || !d.raw) return null
  const from = d.showing === "polished" ? d.polished : d.raw
  const to = d.showing === "polished" ? d.raw : d.polished
  const at = box.lastIndexOf(from)
  if (at < 0) return null
  return {
    text: box.slice(0, at) + to + box.slice(at + from.length),
    dictated: { ...d, showing: d.showing === "polished" ? "raw" : "polished" },
  }
}

/**
 * The pictures a dictation sent with the paper plane carries: the ones already
 * in the box, as the send button would send them. A picture still shrinking
 * has not arrived, so the message waits in the box (`hold`) rather than going
 * without it; a destination that takes no pictures leaves them where they are.
 */
export function dictationPictures(box: { here: boolean; allowed: boolean; busy: boolean; urls: readonly string[] }):
  { pictures: string[]; hold: boolean } {
  if (!box.here || !box.allowed) return { pictures: [], hold: false }
  if (box.busy) return { pictures: [], hold: true }
  return { pictures: box.urls.slice(), hold: false }
}
