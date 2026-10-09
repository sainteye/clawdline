import type { ReactNode } from "react"

/** The same input row for a local Session and a pinned remote Session. */
export function ComposerBox({ children }: { children: ReactNode }) {
  return <div className="box">{children}</div>
}
