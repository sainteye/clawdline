/*
 * Which road this page's reads of one machine are taking, said as one dot.
 *
 * The direct carrier is **one data channel per page and machine**
 * (`direct-carrier.ts`), shared by that machine's terminal and by every Session
 * read of it. So it is a fact about a machine, never about a Session: two
 * Sessions on the same machine cannot take different roads, and two machines
 * routinely do. That is the whole reason this is drawn on the machine's own row
 * and nowhere near a Session's.
 *
 * What a filled dot does not promise: sends, dispatches and the content-free
 * status rows stay on the relay on purpose (`docs/cloud-terminal-wire.md`,
 * Session reads on the carrier). The words name reads, and only reads.
 */

/** What a machine's dot says. `unknown` is drawn as neither, because it is neither. */
export type CarrierShow = "direct" | "relay" | "unknown"

export interface CarrierFacts {
  /** Whether this browser has read that machine's own descriptor at all. */
  known: boolean
  /** Whether that descriptor says the machine opens carriers (`session_carrier_v1`). */
  supported: boolean
  /** Whether a carrier this page already holds to it is open now. */
  open: boolean
}

export interface CarrierReading {
  show: CarrierShow
  /** The word key the dot's tip uses, so the reason is said and not guessed. */
  word: "cloudCarrierDirect" | "cloudCarrierRelayMachineOff" | "cloudCarrierRelayNotOpen" | "cloudCarrierUnknown"
}

/**
 * An open carrier is the only thing that makes reads direct, so it is read
 * first: a machine whose descriptor has not arrived yet can still have the
 * channel this page opened before the descriptor did.
 */
export function carrierReading(facts: CarrierFacts): CarrierReading {
  if (facts.open) return { show: "direct", word: "cloudCarrierDirect" }
  if (!facts.known) return { show: "unknown", word: "cloudCarrierUnknown" }
  if (!facts.supported) return { show: "relay", word: "cloudCarrierRelayMachineOff" }
  return { show: "relay", word: "cloudCarrierRelayNotOpen" }
}
