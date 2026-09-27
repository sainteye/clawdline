/**
 * An item's documents are an append-only history; of the ones kept, the
 * latest written leads without changing the item's document array.
 *
 * @param {import("./api.js").WorkV2Document[] | undefined} documents
 * @param {(document: import("./api.js").WorkV2Document) => boolean} keep
 * @returns {import("./api.js").WorkV2Document[]}
 */
export function documentsNewestFirst(documents, keep) {
  return [...(documents ?? [])]
    .filter(keep)
    .sort((left, right) => right.created_at - left.created_at || right.id.localeCompare(left.id))
}

/**
 * Completion reports, the latest written conclusion first.
 *
 * @param {import("./api.js").WorkV2Document[] | undefined} documents
 * @returns {import("./api.js").WorkV2Document[]}
 */
export function completionReportsNewestFirst(documents) {
  return documentsNewestFirst(documents, (document) => document.role === "completion_report")
}
