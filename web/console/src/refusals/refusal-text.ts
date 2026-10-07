import { catalogRefusalDetail, catalogWord } from "../catalog.js"
import { describeFailure } from "../legacy/js/core/failure-text.js"

/** Keep the copied failure semantics, with the selected catalog owning the complete frame. */
export function failureSentence(
  error: unknown,
  options?: string | { sentence?: string; fallback?: string },
): string {
  const detail = catalogRefusalDetail(error)
  const said = (describeFailure as (
    error: unknown,
    options?: string | { sentence?: string; fallback?: string },
  ) => { text: string; tag: string })(error, detail ? {
    sentence: detail.text,
    fallback: typeof options === "string" ? options : options?.fallback,
  } : options)
  return catalogWord("legacy", "webFailWithTag")
    .replace(/\{text\}|\{tag\}/gu, (part) => part === "{text}" ? said.text : said.tag)
}
