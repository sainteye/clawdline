import { Component, lazy, Suspense, type ReactNode } from "react"
import type { BotTowerProps } from "./BotTower"
import type { Words } from "../words"

// Only this file is in the Bots chunk. The scene, its picture, styles and
// drawing load when the person first opens the tower.
const BotTower = lazy(() => import("./BotTower"))

class TowerBoundary extends Component<{ fallback: ReactNode, children: ReactNode }, { failed: boolean }> {
  state = { failed: false }
  static getDerivedStateFromError() { return { failed: true } }
  render() { return this.state.failed ? this.props.fallback : this.props.children }
}

export function TowerSlot(props: BotTowerProps & { onList: () => void }) {
  const { onList, ...tower } = props, w: Words = props.w
  const fallback = <div className="pa-tower-fallback" role="alert"><p>{w.towerLoadFailed}</p><button type="button" className="pa-pill" onClick={onList}>{w.towerBackToList}</button></div>
  return <TowerBoundary fallback={fallback}>
    <Suspense fallback={<p className="pa-tower-fallback" role="status">{w.towerLoading}</p>}>
      <BotTower {...tower}/>
    </Suspense>
  </TowerBoundary>
}

const preference = "clawdline.bots.view"
export type BotsView = "list" | "tower"
/** The person's own choice, or null before they have made one. */
export function savedBotsView(): BotsView | null {
  try { const v = localStorage.getItem(preference); return v === "tower" || v === "list" ? v : null } catch { return null }
}
export function saveBotsView(view: BotsView) {
  try { localStorage.setItem(preference, view) } catch { /* A storage refusal keeps this page's choice only. */ }
}
