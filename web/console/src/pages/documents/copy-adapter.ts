// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogFormat, catalogRefusalDetail, catalogWord, catalogWordLanguage, currentCatalogTag } from "../../catalog.ts"
import { describeFailure } from "../../legacy/js/core/failure-text.js"

type DocumentRow = { scope: "project" | "task"; path: string; title?: string; task?: string; bytes: number }
type DocumentAnswer = { scope: "project" | "task"; path: string; byte_count: number }

const text = (key: string) => catalogWord("documents", key)
const format = (key: string, args: readonly unknown[]) => catalogFormat("documents", key, args)

/** Only the copied Documents view owns these elements; user document bodies and titles are untouched. */
export function adaptDocumentsCopy(page: HTMLElement, source: {
  rows(): readonly DocumentRow[]
  answer(): DocumentAnswer | null
  error(): unknown
}): () => void {
  const byId = (id: string) => page.querySelector<HTMLElement>(`#${id}`)
  const set = (element: HTMLElement | null, value: string, key?: string) => {
    if (!element) return
    if (element.textContent !== value) element.textContent = value
    if (key && catalogWordLanguage("documents", key) === "en") {
      if (element.lang !== "en") element.lang = "en"
    } else if (element.hasAttribute("lang")) element.removeAttribute("lang")
  }
  let paintedStatus = ""
  const renderError = (element: HTMLElement, error: object, sentence?: string, lang?: string) => {
    // The copied typedError already acknowledged this refusal. Reformatting
    // its sentence must not acknowledge it a second time.
    const source = error as { code?: unknown; ref?: unknown; detail?: unknown; reason?: unknown }
    const said = describeFailure({ ...source, code: source.code, ref: source.ref,
      detail: source.detail, reason: source.reason, acknowledge: undefined },
    { sentence, fallback: text("readFailed") })
    const template = catalogWord("legacy", "webFailWithTag")
    const parts = template.split(/(\{text\}|\{tag\})/u)
    const doc = element.ownerDocument
    const nodes = parts.map((part) => {
      if (part !== "{text}" && part !== "{tag}") return doc.createTextNode(part)
      const span = doc.createElement("span")
      span.textContent = part === "{text}" ? said.text : said.tag
      if (part === "{text}" && lang === "en") span.lang = "en"
      return span
    })
    element.replaceChildren(...nodes)
    if (element.hasAttribute("lang")) element.removeAttribute("lang")
  }
  const paint = () => {
    const fixed: [string, string][] = [
      ["documents-title", "title"], ["documents-back", "back"],
      ["document-list-back", "listBack"], ["document-share", "share"],
      ["document-copy", "copy"],
    ]
    for (const [id, key] of fixed) set(byId(id), text(key), key)

    const status = byId("documents-status")
    if (status && status.textContent !== paintedStatus) {
      const original = status.textContent || ""
      const ordinary: Record<string, string> = {
        "Loading documents…": "loading",
        "Waiting for the encrypted connection…": "connecting",
        "This session has no readable Markdown or text documents.": "empty",
        "Link copied.": "copied",
        "Share sheet opened.": "shared",
      }
      const key = ordinary[original]
      if (key) set(status, text(key), key)
      else if (status.dataset.state === "error" && original) {
        const code = /^([^:]+): /u.exec(original)?.[1] || "document_read_failed"
        const error = source.error() as { code?: string } | null
        const sourceError = error?.code === code ? error : { code }
        const explicit = error?.code === code ? catalogRefusalDetail(error) : null
        const special = code === "malformed_document_locator" ? "invalidLinkError"
          : code === "document_copy_unavailable" ? "copyUnavailableError"
            : code === "document_share_unavailable" ? "shareUnavailableError" : null
        if (special) renderError(status, sourceError, text(special), catalogWordLanguage("documents", special))
        else if (explicit) renderError(status, sourceError, explicit.text, explicit.lang)
        else renderError(status, sourceError, undefined, catalogWordLanguage("documents", "readFailed"))
      } else if (!original) set(status, "")
      paintedStatus = status.textContent || ""
    }

    const rows = source.rows()
    const buttons = page.querySelectorAll<HTMLButtonElement>("#documents-rows button.document-row")
    if (buttons.length === rows.length) buttons.forEach((button, index) => {
      const row = rows[index]
      if (button.querySelector("strong")?.textContent !== row.path) return
      const meta = button.querySelector<HTMLElement>("span")
      if (!meta) return
      const count = new Intl.NumberFormat(currentCatalogTag()).format(row.bytes)
      const key = row.scope === "task" ? row.bytes === 1 ? "listTaskMetaOne" : "listTaskMeta"
        : row.bytes === 1 ? "listProjectMetaOne" : "listProjectMeta"
      set(meta, row.scope === "task" ? format(key, [row.title || row.task || "", count]) : format(key, [count]), key)
    })

    // The copied controller clears metadata synchronously, then calls read in
    // the next microtask. Its previous answer must not repaint that gap.
    const answer = byId("document-viewer")?.hidden || status?.textContent === text("loading")
      ? null : source.answer()
    const meta = byId("document-meta")
    if (answer && meta && byId("document-title")?.textContent) {
      const key = answer.scope === "task" ? answer.byte_count === 1 ? "taskMetaOne" : "taskMeta"
        : answer.byte_count === 1 ? "projectMetaOne" : "projectMeta"
      const count = new Intl.NumberFormat(currentCatalogTag()).format(answer.byte_count)
      set(meta, format(key, [answer.path, count]), key)
    }

    for (const id of ["document-share", "document-copy"]) {
      const button = byId(id)
      if (!button) continue
      // documentShareURL always emits this typed code for a local or invalid
      // canonical URL. Do not erase any other producer reason or its ref.
      if (button.title.startsWith("document_share_unavailable:")) {
        const title = catalogWord("legacy", "webFailWithTag")
          .replace("{text}", text("shareUnavailable"))
          .replace("{tag}", "document_share_unavailable")
        if (button.title !== title) button.title = title
      }
    }
  }
  paint()
  const observer = new MutationObserver(paint)
  observer.observe(page, { childList: true, characterData: true, subtree: true, attributes: true, attributeFilter: ["title"] })
  return () => observer.disconnect()
}
