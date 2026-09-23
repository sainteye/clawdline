/**
 * Completion reports are an append-only history; the latest written
 * conclusion leads without changing the item's document array.
 *
 * @param {import("./api.js").WorkV2Document[] | undefined} documents
 * @returns {import("./api.js").WorkV2Document[]}
 */
export function completionReportsNewestFirst(documents) {
  return [...(documents ?? [])]
    .filter((document) => document.role === "completion_report")
    .sort((left, right) => right.created_at - left.created_at || right.id.localeCompare(left.id))
}
