import * as L from "../../legacy/bridge.js"
import type { WorkV2Document, WorkV2Item } from "./api.js"
import { completionReportsNewestFirst } from "./completion-report-order.js"
import { completionReportText } from "./completion-report-text.js"
import { epicPlanDocuments } from "./epic-gate.js"
import { when } from "./shared.js"
import { WorkIcon } from "./WorkIcon.js"

export function completionReports(item: WorkV2Item): WorkV2Document[] {
  return completionReportsNewestFirst(item.documents)
}

/** A durable, user-readable conclusion. It is narrative, not a substitute for lifecycle receipts. */
export function WorkCompletionReports({ item, expanded = false }: { item: WorkV2Item; expanded?: boolean }) {
  return <WorkDocuments documents={completionReports(item)} label="結案報告" expanded={expanded}
    authority={() => "Agent 調查結論 · 驗證、Merge 與部署證據另列於項目進度"} />
}

const PLAN_TITLE: Partial<Record<WorkV2Document["role"], string>> = { plan: "計劃書", plan_review: "Child Review" }

/** An Epic's plan and the Child Session reviews of it, each plan followed by its review. */
export function WorkEpicPlanDocuments({ item }: { item: WorkV2Item }) {
  return <WorkDocuments documents={epicPlanDocuments(item.documents)} label="計劃書與 Review"
    authority={(document) => document.role === "plan_review"
      ? `Child Session 對計劃書的 review${document.reference ? ` · Task ${document.reference.slice(0, 8)}` : ""}`
      : "負責 Session 寫的實作計劃 · 實作前須經 Child Session review"} />
}

/** Agent-written documents drawn as folded, readable sections; the role picks the tone. */
function WorkDocuments({ documents, label, authority, expanded = false }: {
  documents: WorkV2Document[]
  label: string
  authority: (document: WorkV2Document) => string
  expanded?: boolean
}) {
  if (!documents.length) return null
  return <section className="work-completion-reports" aria-label={label}>
    {documents.map((document, index) => <details className="work-completion-report" data-role={document.role} key={document.id} open={expanded}>
      <summary><WorkIcon name="check" /><span className="work-completion-report-title">
        <strong>{document.title || PLAN_TITLE[document.role] || label}</strong>
        <time dateTime={new Date(document.created_at * 1000).toISOString()}>寫於 {when(document.created_at)}</time>
      </span>
        {documents.length > 1 && <small>{index + 1} / {documents.length}</small>}</summary>
      <p className="work-completion-report-authority">{authority(document)}</p>
      <div className="work-completion-report-body"
        dangerouslySetInnerHTML={{ __html: L.richTextHTML(completionReportText(document.body)) }} />
      {document.reference && <p className="work-completion-report-reference">參考：{document.reference}</p>}
    </details>)}
  </section>
}
