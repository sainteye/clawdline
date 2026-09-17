// "My messages" — its copied data module, and the press that opens it.
//
// `js/view/user-messages-data.js` is the Swift app's, byte for byte, and it is
// the whole of what this feature decides: which turns are the person's, which
// of them survive the search field, where one of them is in the transcript, and
// the words for the sheet before the catalog has arrived. `input/user-messages.js`
// is not copied — it builds its own DOM island on the body at import, through
// ids React has not drawn yet — so `session/UserMessages.tsx` owns the markup
// and the clicks, and takes every answer from here.
import {
  copyForUserMessages,
  filterUserMessages as filterUserMessagesOriginal,
  userMessageEntries as userMessageEntriesOriginal,
  userMessagePosition as userMessagePositionOriginal,
} from "./js/view/user-messages-data.js"
import type { TranscriptEntry } from "@clawdline/contract"

/** How the `⋯` menu asks for the sheet without owning it (`overlays/events.ts`'s idiom). */
export const OPEN_USER_MESSAGES = "clawdline:open-user-messages"

/** `#session-user-messages`: open "my messages" for the session on screen. */
export function requestUserMessages(): void {
  document.dispatchEvent(new CustomEvent(OPEN_USER_MESSAGES))
}

/** The words the sheet needs before the catalog lands; this module's own table. */
export const userMessagesCopy = copyForUserMessages as (
  language: string,
) => { title: string; empty: string; search: string; noMatches: string }

/** Turns authored by the person, newest first, with what is still on its way at the top. */
export const userMessageEntries = userMessageEntriesOriginal as (
  entries: TranscriptEntry[],
  pending: TranscriptEntry[],
) => TranscriptEntry[]

/** The visible slice while the person types, keeping the original entry objects. */
export const filterUserMessages = filterUserMessagesOriginal as (
  entries: TranscriptEntry[],
  query: string,
) => TranscriptEntry[]

/**
 * The row one of those exact entry objects occupies in the transcript. Text and
 * timestamps are deliberately not identities: a person can send the same
 * sentence twice, including twice in the same second.
 */
export const userMessagePosition = userMessagePositionOriginal as (
  entries: TranscriptEntry[],
  pending: TranscriptEntry[],
  selected: TranscriptEntry,
  newestFirst: boolean,
) => number
