import { nextWord } from "../next-strings.js"
import type { CarrierReading } from "./carrier-state.js"

/**
 * One dot on a machine's row: filled when this page's reads of that machine
 * take the direct channel, hollow when they take the Cloud relay, faint when
 * this browser has not read that machine yet.
 *
 * No word beside it. The sentence, with its reason, is the tip; `spoken` also
 * puts it in the row's own name, for a row whose name a reader hears (a
 * heading whose button states its own label cannot, so that one is `title`
 * only and the section around it says it instead).
 */
export function CarrierDot({ reading, machine, spoken = false }: {
  reading: CarrierReading
  machine: string
  spoken?: boolean
}) {
  const word = nextWord(reading.word, { machine })
  return (
    <span className="cloud-carrier-dot" data-carrier={reading.show} title={word} aria-hidden={spoken ? undefined : true}>
      {spoken && <span className="sr-only">{word}</span>}
    </span>
  )
}
