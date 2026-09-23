/**
 * Older Agent-written reports sometimes stored a Markdown blank line as the
 * two visible characters `\\n\\n`. Recover only that paragraph separator:
 * a single `\\n` may be intentional prose or code and must stay literal.
 *
 * @param {string} body
 */
export function completionReportText(body) {
  return body.replace(/\\r\\n\\r\\n|\\n\\n/g, "\n\n")
}
