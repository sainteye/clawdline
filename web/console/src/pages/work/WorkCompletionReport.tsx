import * as L from "../../legacy/bridge.js"
import type { WorkV2Document, WorkV2Item } from "./api.js"
import { completionReportText } from "./completion-report-text.js"

export function completionReports(item: WorkV2Item): WorkV2Document[] {
  return (item.documents ?? []).filter((document) => document.role === "completion_report")
}

/** A durable, user-readable conclusion. It is narrative, not a substitute for lifecycle receipts. */
export function WorkCompletionReports({ item, expanded = false }: { item: WorkV2Item; expanded?: boolean }) {
  const reports = completionReports(item)
  if (!reports.length) return null
  return <section className="work-completion-reports" aria-label="結案報告">
    {reports.map((document, index) => <details className="work-completion-report" key={document.id} open={expanded}>
      <summary><span aria-hidden="true">✓</span><strong>{document.title || "結案報告"}</strong>
        {reports.length > 1 && <small>{index + 1} / {reports.length}</small>}</summary>
      <p className="work-completion-report-authority">Agent 調查結論 · 驗證、Merge 與部署證據另列於項目進度</p>
      <div className="work-completion-report-body"
        dangerouslySetInnerHTML={{ __html: L.richTextHTML(completionReportText(document.body)) }} />
      {document.reference && <p className="work-completion-report-reference">參考：{document.reference}</p>}
    </details>)}
  </section>
}
