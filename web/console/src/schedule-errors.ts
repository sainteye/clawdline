import type { NextWord, nextWord } from "./next-strings.js"

export interface InvalidScheduleError {
  error?: string
  error_kind?: string
}

export interface ScheduleErrorCopy {
  sentence: string
  detail: string
  detailsLabel: string
}

const sentenceKeys: Record<string, NextWord> = {
  unreadable_json: "scheduleUnreadableJSON",
  schema: "scheduleInvalidSchema",
  project_unavailable: "scheduleProjectUnavailable",
}

/** Producer prose is evidence, not the screen's claim. The kind chooses that claim. */
export function scheduleErrorCopy(row: InvalidScheduleError, word: typeof nextWord): ScheduleErrorCopy {
  return {
    sentence: word(sentenceKeys[row.error_kind ?? ""] ?? "scheduleUnreadable"),
    detail: row.error?.trim() ?? "",
    detailsLabel: word("transcriptTechnicalDetails"),
  }
}

function html(value: string): string {
  return value.replace(/[&<>"']/g, (character) => {
    switch (character) {
      case "&": return "&amp;"
      case "<": return "&lt;"
      case ">": return "&gt;"
      case '"': return "&quot;"
      default: return "&#39;"
    }
  })
}

/** The string-rendered schedule list's equivalent of Transcript.technicalDetails(). */
export function invalidScheduleErrorHTML(row: InvalidScheduleError, word: typeof nextWord): string {
  const copy = scheduleErrorCopy(row, word)
  const details = copy.detail
    ? `<details class="schedule-error-details"><summary>${html(copy.detailsLabel)}</summary><code>${html(copy.detail)}</code></details>`
    : ""
  return `<p class="schedule-error">${html(copy.sentence)}</p>${details}`
}
