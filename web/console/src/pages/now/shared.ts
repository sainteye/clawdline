import * as L from "../../legacy/bridge.js"
import { nowWord } from "./words.js"

/**
 * A read that did not happen, as a sentence.
 *
 * The same shape as the board's `failureWords` and for the same reason: the
 * daemon refuses with a code, the catalog has a sentence per code, and
 * `failureSentence` chooses that sentence and puts `code · ref` after it —
 * which is the pair somebody reporting it will be asked for. A block on this
 * page that replaced that with one subjectless sentence would be doing, in a
 * larger type size, the thing this page exists to stop.
 *
 * The fallback does not name the code. The formatter appends it already, and
 * a fallback that spells it out too reads "這一塊讀不到。 store_unavailable
 * (store_unavailable)" — twice, the second time in brackets. That was on
 * screen before it was noticed in the source, which is the argument for
 * looking at the page and not only at the test.
 *
 * It takes a refusal and a connection that never answered the same way on
 * purpose: both mean this block has no count, and the sentence that follows
 * is the formatter's, which already tells the two apart.
 */
export function failureWords(e: unknown): string {
  return L.failureSentence(e, nowWord("unreadable"))
}
