import { catalogRefusalDetail, catalogWord } from "../catalog.js"
import { describeFailure } from "../legacy/js/core/failure-text.js"

// The copied formatter is pinned to the retired app. New Cloud refusal codes
// enter through this editable adapter, with their code and ref still attached.
const SESSION_RECEIPT_SENTENCES: Readonly<Record<string, string>> = {
  idempotency_key_required: "webFailReceiptKeyRequired",
  idempotency_key_reused: "webFailReceiptKeyReused",
  execution_generation_required: "webFailExecutionGeneration",
  execution_generation_changed: "webFailExecutionGeneration",
  execution_target_missing: "webFailExecutionGeneration",
  execution_target_required: "webFailExecutionGeneration",
  execution_machine_mismatch: "webFailExecutionGeneration",
  execution_source_unknown: "webFailExecutionGeneration",
  execution_check_unavailable: "webFailExecutionGeneration",
  execution_records_full: "webFailExecutionGeneration",
  receipt_unavailable: "webFailReceiptUnavailable",
  receipt_outcome_unknown: "webFailReceiptUnknown",
  receipt_pending: "webFailReceiptPending",
  receipt_capacity: "webFailReceiptCapacity",
}

/** Keep the copied failure semantics, with the selected catalog owning the complete frame. */
export function failureSentence(
  error: unknown,
  options?: string | { sentence?: string; fallback?: string },
): string {
  const detail = catalogRefusalDetail(error)
  const code = error && typeof error === "object" && "code" in error ? error.code : undefined
  const key = typeof code === "string" ? SESSION_RECEIPT_SENTENCES[code] : undefined
  const sentence = detail?.text ?? (typeof options === "object" ? options?.sentence : undefined) ??
    (key ? catalogWord("legacy", key) : undefined)
  const said = (describeFailure as (
    error: unknown,
    options?: string | { sentence?: string; fallback?: string },
  ) => { text: string; tag: string })(error, sentence ? {
    sentence,
    fallback: typeof options === "string" ? options : options?.fallback,
  } : options)
  return catalogWord("legacy", "webFailWithTag")
    .replace(/\{text\}|\{tag\}/gu, (part) => part === "{text}" ? said.text : said.tag)
}
