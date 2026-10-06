// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { catalogRefusalDetail, currentCatalogTag, type CatalogTag } from "../../catalog.ts"

type VisibleDetail = { text: string; lang: CatalogTag }

/** The copied status already says the code and ref; add a producer detail only for that failure. */
export function projectRefusalDetail(status: string, error: unknown): VisibleDetail | null {
  const code = error && typeof error === "object" && typeof (error as { code?: unknown }).code === "string"
    ? (error as { code: string }).code : ""
  if (!code || !status.includes(code)) return null
  const detail = catalogRefusalDetail(error)
  return detail && !status.includes(detail.text) ? detail : null
}

/** A separate span keeps raw English pronounceable without changing the copied click handlers. */
export function showProjectRefusalDetail(status: HTMLElement | null, error: unknown): void {
  if (!status) return
  const existing = status.querySelector<HTMLElement>(":scope > span[data-catalog-refusal-detail]")
  const base = [...status.childNodes].filter((node) => node !== existing)
    .map((node) => node.textContent ?? "").join("")
  const detail = projectRefusalDetail(base, error)
  if (!detail) { existing?.remove(); return }
  const desired = ` · ${detail.text}`
  const lang = detail.lang === "en" && currentCatalogTag() !== "en" ? "en" : ""
  if (existing?.textContent === desired && existing.lang === lang) return
  const span = existing ?? status.ownerDocument.createElement("span")
  span.dataset.catalogRefusalDetail = "true"
  span.textContent = desired
  if (lang) span.lang = lang
  else span.removeAttribute("lang")
  if (!existing) status.appendChild(span)
}
