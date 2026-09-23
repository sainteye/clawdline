import * as L from "../../legacy/bridge.js"
import type { WorkV2Document, WorkV2Item } from "./api.js"
import { completionReportsNewestFirst } from "./completion-report-order.js"
import { completionReportText } from "./completion-report-text.js"
import { when } from "./shared.js"
import { WorkIcon } from "./WorkIcon.js"

export function completionReports(item: WorkV2Item): WorkV2Document[] {
  return completionReportsNewestFirst(item.documents)
}

/** A durable, user-readable conclusion. It is narrative, not a substitute for lifecycle receipts. */
export function WorkCompletionReports({ item, expanded = false }: { item: WorkV2Item; expanded?: boolean }) {
  const reports = completionReports(item)
  if (!reports.length) return null
  return <section className="work-completion-reports" aria-label="結案報告">
    {reports.map((document, index) => <details className="work-completion-report" key={document.id} open={expanded}>
      <summary><WorkIcon name="check" /><span className="work-completion-report-title">
        <strong>{document.title || "結案報告"}</strong>
        <time dateTime={new Date(document.created_at * 1000).toISOString()}>寫於 {when(document.created_at)}</time>
      </span>
        {reports.length > 1 && <small>{index + 1} / {reports.length}</small>}</summary>
      <p className="work-completion-report-authority">Agent 調查結論 · 驗證、Merge 與部署證據另列於項目進度</p>
      <div className="work-completion-report-body"
        dangerouslySetInnerHTML={{ __html: L.richTextHTML(completionReportText(document.body)) }} />
      {document.reference && <p className="work-completion-report-reference">參考：{document.reference}</p>}
    </details>)}
  </section>
}
