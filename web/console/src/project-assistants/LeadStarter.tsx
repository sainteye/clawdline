import type { Words } from "./words"

/** One suggested first message for a new lead conversation; choosing it fills the box, it does not send. */
export function LeadStarter({ w, onChoose }: { w: Words; onChoose: (text: string) => void }) {
  return <div className="pa-lead-starter">
    <p>{w.leadStarterHint}</p>
    <button type="button" onClick={() => onChoose(w.leadStarterMessage)}>{w.leadStarterMessage}</button>
  </div>
}
