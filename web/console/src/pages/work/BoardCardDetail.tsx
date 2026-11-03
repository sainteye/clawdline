import type { ReactNode } from "react"
import { LoadingSkeleton } from "../../LoadingSkeleton.js"
import { workItemIsFull, type WorkV2Card, type WorkV2Item } from "./api.js"

/** Mount mutation controls only after the complete item has arrived. */
export function BoardCardDetail({ card, error, retry, loadingLabel, retryLabel, children }: {
  card: WorkV2Card
  error: string
  retry: () => void
  loadingLabel: string
  retryLabel: string
  children: (item: WorkV2Item) => ReactNode
}) {
  if (workItemIsFull(card)) return children(card)
  return <div className="work-v2-card">
    <h3 id={`work-card-title-${card.id}`}>{card.title}</h3>
    {error ? <p className="work-note" role="alert">{error} <button className="chip" type="button" onClick={retry}>{retryLabel}</button></p>
      : <LoadingSkeleton label={loadingLabel} id="work-item-detail" />}
  </div>
}
