import type { ReactNode } from "react"
import { SidebarIcon } from "../../SidebarIcon.js"
import { nextWord } from "../../next-strings.js"
import { attentionCount, type Attention } from "./health/attention.js"

/** Discovery only: every operations read still rechecks superuser authority in Cloud. The
 *  dashboard comes first and the reach page last; nobody else sees the section at all. The mark
 *  is the Cloud's actionable items only: a failed or pending read draws no mark and no zero. */
export function AdministratorSection({ allowed, attention, children }: { allowed: boolean; attention: Attention | null; children?: ReactNode }) {
  if (!allowed) return null
  const count = attentionCount(attention)
  const owed = count !== null && count > 0
  return <section className="sidebar-group" aria-label={nextWord("sidebarAdministrator")}>
    <h2>{nextWord("sidebarAdministrator")}</h2>
    <a className="sidebar-item" id="nav-admin-dashboard" href="/admin/health">
      <SidebarIcon name="usage" />{nextWord("adminDashboardNav")}
      {owed ? <span className="sidebar-attention" role="img" aria-label={nextWord("adminAttentionMark", { n: String(count) })} /> : null}
    </a>
    {children}
  </section>
}
