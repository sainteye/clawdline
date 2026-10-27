import { useEffect, useRef, useState, type ReactNode } from "react"
import { LoadingSkeleton } from "../../LoadingSkeleton.js"
import { catalogWord } from "../../catalog.js"
import type { Decision, WorkV2Card } from "./api.js"
import { desktopBoardItems } from "./desktop-board.js"
import { WorkIcon } from "./WorkIcon.js"
import "./desktop-board.css"

/** Match the console's existing desktop boundary; never mount both card trees. */
export function useDesktopBoard(): boolean {
  const [desktop, setDesktop] = useState(() => typeof window !== "undefined" && window.matchMedia("(min-width: 900px)").matches)
  useEffect(() => {
    const media = window.matchMedia("(min-width: 900px)")
    const update = () => setDesktop(media.matches)
    update()
    media.addEventListener("change", update)
    return () => media.removeEventListener("change", update)
  }, [])
  return desktop
}

export interface DesktopBoardVisibility {
  proposals: boolean
  plans: boolean
  done: boolean
}

export function DesktopBoardSwitches({ value, onChange }: {
  value: DesktopBoardVisibility
  onChange: (value: DesktopBoardVisibility) => void
}) {
  return <div className="work-desktop-switches">
    {([
      ["proposals", catalogWord("work", "desktopShowProposals")],
      ["plans", catalogWord("work", "desktopShowPlans")],
      ["done", catalogWord("work", "desktopShowDone")],
    ] as const).map(([key, label]) => <label key={key} className="work-desktop-switch">
      <input type="checkbox" checked={value[key]} onChange={(event) => onChange({ ...value, [key]: event.currentTarget.checked })} />
      <span>{label}</span>
    </label>)}
  </div>
}

export function DesktopBoard({ items, decisions, visibility, proposalCount, proposals, renderCard, loading, loadingLabel }: {
  items: WorkV2Card[]
  decisions: Decision[]
  visibility: DesktopBoardVisibility
  proposalCount: number
  proposals: ReactNode
  renderCard: (item: WorkV2Card) => ReactNode
  loading: boolean
  loadingLabel: string
}) {
  const scroller = useRef<HTMLDivElement>(null)
  const groups = desktopBoardItems(items, decisions)
  const move = (direction: number) => {
    const node = scroller.current
    if (node) node.scrollBy({ left: direction * node.clientWidth, behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "instant" : "smooth" })
  }
  const lane = (key: keyof typeof groups, title: string) => <section key={key} className="work-desktop-lane" data-lane={key}>
    <header className="work-desktop-lane-head"><h2>{title}</h2><span className="work-count">{loading ? "" : groups[key].length}</span></header>
    <div className="work-desktop-lane-body">{loading ? <LoadingSkeleton label={loadingLabel} id={`work-desktop-${key}`} /> : groups[key].length ? groups[key].map(renderCard) : <p className="work-desktop-empty">{catalogWord("work", "desktopEmptyLane")}</p>}</div>
  </section>
  return <div className="work-desktop-board">
    <div ref={scroller} className="work-desktop-lanes" tabIndex={0} role="region" aria-label={catalogWord("work", "desktopColumns")} aria-busy={loading}>
      {visibility.proposals && <section className="work-desktop-lane" data-lane="proposals">
        <header className="work-desktop-lane-head"><h2>{catalogWord("inline", "90b290a48f82")}</h2><span className="work-count">{proposalCount}</span></header>
        <div className="work-desktop-lane-body">{proposals}</div>
      </section>}
      {visibility.plans && lane("planning", catalogWord("inline", "944fa4126f45"))}
      {lane("unassigned", catalogWord("inline", "109be039b4c0"))}
      {lane("active", catalogWord("work", "sectionActive"))}
      {lane("attention", catalogWord("work", "desktopAttention"))}
      {visibility.done && lane("done", catalogWord("inline", "f28461bb49c8"))}
    </div>
    <div className="work-desktop-scroll-tools"><span>{catalogWord("work", "desktopScrollHint")}</span>
      <button type="button" aria-label={catalogWord("work", "desktopScrollLeft")} onClick={() => move(-1)}><WorkIcon name="open" /></button>
      <button type="button" aria-label={catalogWord("work", "desktopScrollRight")} onClick={() => move(1)}><WorkIcon name="open" /></button>
    </div>
  </div>
}
