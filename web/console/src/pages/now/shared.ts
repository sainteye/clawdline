import { RefusalError } from "@clawdline/core"
import * as L from "../../legacy/bridge.js"
import { nowWord } from "./words.js"

/**
 * A read that did not happen, as a sentence.
 *
 * The same shape as the board's `failureWords` and for the same reason: the
 * daemon refuses with a code, the catalog has a sentence per code, and
 * `failureSentence` puts `code · ref` after it — which is the pair somebody
 * reporting it will be asked for. A block on this page that replaced that
 * with one subjectless sentence would be doing, in a larger type size, the
 * thing this page exists to stop.
 */
export function failureWords(e: unknown): string {
  if (e instanceof RefusalError) {
    return L.failureSentence(e, nowWord("unreadable") + " " + e.code)
  }
  // Nothing answered. Not "there is none": nothing is known either way, which
  // is what the block's `—` already says and what this sentence explains.
  return L.failureSentence(e, nowWord("unreadable"))
}
