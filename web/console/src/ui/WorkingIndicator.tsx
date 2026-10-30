import type { ReactNode } from "react"
import { BRAND_MARK } from "../brand-mark.js"
import { catalogWord } from "../catalog.js"
import "./working-indicator.css"

/** Mount only while the caller has confirmed active work; this does not estimate progress. */
export function WorkingIndicator({ detail, children }: { detail: ReactNode; children?: ReactNode }) {
  return <div className="working-indicator">
    <svg className="working-indicator-art" viewBox="0 0 64 44" aria-hidden="true" focusable="false" shapeRendering="crispEdges">
      <g className="working-indicator-body">
        {BRAND_MARK.cells.slice(0, 8).flatMap((row, y) => row.map((color, x) => color &&
          <rect key={`${x}-${y}`} x={x * 3 + 8} y={y * 3 + 4} width="3" height="3" fill={color} />))}
      </g>
      <g fill="#d97757">
        <rect className="working-indicator-foot is-left" x="20" y="28" width="6" height="4" />
        <rect className="working-indicator-foot is-right" x="38" y="28" width="6" height="4" />
        <rect className="working-indicator-dot" x="23" y="39" width="3" height="3" />
        <rect className="working-indicator-dot" x="31" y="39" width="3" height="3" />
        <rect className="working-indicator-dot" x="39" y="39" width="3" height="3" />
      </g>
    </svg>
    <div className="working-indicator-copy">
      <div role="status" aria-live="polite" aria-atomic="true">
        <p className="working-indicator-title">{catalogWord("workingIndicator", "title")}</p>
        <p className="working-indicator-detail">{detail}</p>
      </div>
      {children && <div className="working-indicator-extra">{children}</div>}
    </div>
  </div>
}
