import { useState } from "react"
import { readProblemIsTransient } from "./state.js"

/** A quiet status row never covers content or takes focus from a draft. */
export function CloudReadProblem(props: {
  message: string; code: string; trace?: string; retryLabel: string; dismissLabel: string;
  onRetry: () => void;
  fleetWakeAvailable?: boolean;
  /** Offered when pairing this browser again is the way back (`readProblemOffersPairing`). */
  pair?: { label: string; run: () => void };
}) {
  const [dismissed, setDismissed] = useState(false)
  if (dismissed || (props.fleetWakeAvailable && readProblemIsTransient(props.code))) return null
  return <section role="status" className="cloud-read-problem">
    <span title={props.code + (props.trace ? ` · ${props.trace}` : "")}>{props.message}</span>
    {props.pair ? <button type="button" data-pair="true" onClick={props.pair.run}>{props.pair.label}</button> : null}
    <button type="button" onClick={props.onRetry}>{props.retryLabel}</button>
    <button type="button" aria-label={props.dismissLabel} title={props.dismissLabel}
      onClick={() => setDismissed(true)}>×</button>
  </section>
}
