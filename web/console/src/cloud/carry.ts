// What this console carries to a Mac, word by word, and what it does not.
//
// The seam used to decide that in two hand-written places: a `switch` over four
// GET paths in `relay-reader.ts` and a table of routes in `relay-writer.ts`.
// Both were right when they were written and neither could notice when they
// stopped being: the Mac's vocabulary is `cloudops.Implemented()` on the Go
// daemon, it grows, and nothing here read it. That is how `info` came to be a
// word the Mac answers and the browser never asks — the status line under
// every cloud session said "Loading…" for as long as that was true, and the
// Mac's log held no refusal, because the refusal was this page's own.
//
// So the words live here, once, in three lists that together must be exactly
// the Mac's vocabulary:
//
//   `CARRIED`       — asked of the Mac, with the console route that asks.
//   `DEFERRED`      — the Mac answers it; this console does not ask yet, and
//                     says why and where it can be done instead.
//   `NO_MAC_ROUTE`  — in the Mac's vocabulary with nothing behind it there.
//
// **What makes this sourced rather than written down twice** is
// `TestTheConsoleCarryTableMatchesThisMachinesVocabulary`
// (internal/app/cloudops/carry_test.go). It reads this file and compares its
// three key sets with `Implemented()` and `Vocabulary()`. A word added to the
// catalog and to neither list here fails that test by name, and so does a word
// here that the Mac has stopped knowing. The runtime half is
// `RelayReader.drift()`, which asks the machine's own descriptor the same
// question on the page, for a Mac running ahead of the bundle it is being read
// from.
//
// Nothing is imported here, so both the console and `node --test` read it as it
// is.

/** One word of the Mac's Cloud vocabulary. */
export type CloudWord = CarriedWord | keyof typeof DEFERRED | keyof typeof NO_MAC_ROUTE

/**
 * A word this console asks a Mac for.
 *
 * `relay-writer.ts` declares each of its routes' words through this type, so a
 * route that names a word missing from `CARRIED` is a compile error rather than
 * a fourteenth place the vocabulary is written down.
 */
export type CarriedWord = keyof typeof CARRIED

/**
 * The words this console asks a Mac for, and the console route that asks.
 *
 * The value is documentation and the key is the contract: `writeRoute` and the
 * reader's own cases name these keys, and TypeScript refuses a word that is not
 * one of them.
 */
export const CARRIED = {
  answer: "POST /v1/sessions/{id}/key",
  end: "POST /v1/sessions/{id}/close",
  focus: "POST /v1/sessions/{id}/focus",
  image: "GET /v1/artifacts/images/{id}?session={id}",
  info: "GET /v1/sessions/{id}/info[?parts=summary]",
  "past-sessions": "GET /v1/places/{id}/sessions[/{assistant}]",
  places: "GET /v1/places",
  resume: "POST /v1/places/{id}/resume[/{assistant}]/{past}",
  send: "POST /v1/sessions/{id}/send",
  start: "POST /v1/places/{id}/start[/{assistant}[/{model}]]",
  transcript: "GET /v1/transcript?session={id}",
  voice: "POST /v1/voice",
} as const

/**
 * Words this Mac answers that this console does not ask for yet.
 *
 * Each sentence is what a person reading a refusal needs: what is missing and
 * where the thing can be done instead. They are here rather than in a document
 * because this is the list the drift guard reads, so a word cannot be quietly
 * left out of both.
 */
export const DEFERRED = {
  board: "The work board is not read over Clawdline Cloud yet: read it on the Mac.",
  "board.items": "The work board's items are not read over Clawdline Cloud yet: read them on the Mac.",
  document: "A document's text is not read over Clawdline Cloud yet: open it on the Mac.",
  documents: "A session's documents are not listed over Clawdline Cloud yet: open them on the Mac.",
  git: "The working tree is not read over Clawdline Cloud yet: read it on the Mac.",
  // `key` and `answer` are one command under two names on the wire. This
  // console sends a waiting card's press as `answer`, because only that
  // spelling carries `expect`, the fingerprint of the question the press was
  // chosen for (F1, `RelayWriter.press`). `key` is the older spelling and is
  // deliberately never sent.
  key: "A waiting card's press is sent as `answer`, which names the question it answers; `key` is the older spelling of the same command.",
  "schedule-create": "Schedules are not written over Clawdline Cloud yet: make one on the Mac.",
  "schedule-delete": "Schedules are not written over Clawdline Cloud yet: delete it on the Mac.",
  "schedule-run": "Schedules are not run over Clawdline Cloud yet: run it on the Mac.",
  "schedule-update": "Schedules are not written over Clawdline Cloud yet: change it on the Mac.",
  schedules: "The schedule list is not read over Clawdline Cloud yet: read it on the Mac.",
  screen: "What the terminal is showing is not read over Clawdline Cloud yet: look at it on the Mac.",
} as const

/**
 * Words the Mac knows and has nothing behind. Asking for one is answered
 * `unknown_command` by the bridge, so the console does not ask: the sentence
 * here is what it says instead.
 *
 * A word that gains a route on the Mac fails the drift guard until it is moved
 * into `CARRIED` or `DEFERRED`, which is the whole reason this list is a list
 * and not a comment.
 */
export const NO_MAC_ROUTE = {
  agent: "This Mac does not answer an agent's own record over Clawdline Cloud: read it on the Mac.",
  "diagnostics.events": "This Mac does not take a page's diagnostic events over Clawdline Cloud.",
  "diagnostics.report": "This Mac does not take a diagnostic report over Clawdline Cloud.",
  dispatch: "Dispatching a task over Clawdline Cloud has no pinned wire shape on this Mac: dispatch it on the Mac.",
  schedule: "One schedule is not read over Clawdline Cloud yet: read it on the Mac.",
  shell: "A session's shell is not read over Clawdline Cloud: read it on the Mac.",
  skills: "A session's skills are not listed over Clawdline Cloud: read them on the Mac.",
  snippets: "Snippets are not carried over Clawdline Cloud yet: open them on the Mac.",
  timeline: "A project's timeline is not read over Clawdline Cloud yet: read it on the Mac.",
} as const

/**
 * Own-origin routes this seam answers itself, with no Cloud word behind them.
 *
 * They are not part of the vocabulary and so not part of the drift guard; they
 * are here so that the one place that says what a hosted page can reach says
 * all of it. `/v1/sessions` is the relay's own snapshot of this machine's rows,
 * `/v1/health` is whether the line is up, and `/v1/strings` is the bundle's own
 * catalog (`cloud/strings.ts`).
 */
export const ANSWERED_HERE: readonly string[] = ["/v1/sessions", "/v1/health", "/v1/strings"]

/**
 * What the seam is handed: the words this bundle asks for, and what it says
 * about a route it does not carry.
 *
 * `relay-reader.ts` imports nothing at run time — that is what lets it be
 * loaded as it is by `node --test` — so the table reaches it as a value rather
 * than as an import (`RelayReaderOptions.carry`), from the one place that
 * assembles the seam.
 */
export interface CarryTable {
  readonly carried: readonly string[]
  detail(method: string, path: string, word?: string): string
}

/** This build's table, as the seam takes it. */
export const CARRY_TABLE: CarryTable = { carried: Object.keys(CARRIED), detail: notCarriedDetail }

/** Every word in the three lists, sorted — what the Mac's vocabulary must be. */
export function words(): CloudWord[] {
  return [...Object.keys(CARRIED), ...Object.keys(DEFERRED), ...Object.keys(NO_MAC_ROUTE)].sort() as CloudWord[]
}

/**
 * The Cloud word a console route this seam does not carry stands for, or "".
 *
 * Only the uncarried ones: what is carried is parsed by `writeRoute` and by
 * the reader's own cases, and a second spelling of those would be a second
 * thing to keep right. This is what turns "this is not carried" into "snippets
 * are not carried", which is the difference between a refusal somebody can act
 * on and one they can only report.
 */
export function uncarriedWordOf(method: string, path: string): string {
  const parts = path.split("/").slice(1)
  if (parts[0] !== "v1") return ""
  let segments: string[]
  try {
    segments = parts.slice(1).map((p) => decodeURIComponent(p))
  } catch {
    return ""
  }
  const [head, a, b] = segments
  if (head === "snippets") return "snippets"
  if (head === "board") return method === "GET" ? "board" : "board.items"
  if (head === "timeline") return "timeline"
  if (head === "diagnostics" && a === "report") return "diagnostics.report"
  if (head === "orchestrator" && a === "schedules") {
    if (segments.length === 2) return method === "POST" ? "schedule-create" : "schedules"
    if (method === "DELETE") return "schedule-delete"
    if (method === "PATCH" || method === "PUT") return "schedule-update"
    if (method === "POST") return "schedule-run"
    return "schedule"
  }
  if (head === "sessions" && a && b) {
    switch (b) {
      case "git":
        return "git"
      case "screen":
        return "screen"
      case "skills":
        return "skills"
      case "documents":
        return segments.length > 3 ? "document" : "documents"
    }
  }
  return ""
}

/** The sentence for a word this console will not ask for, or "" for one it carries. */
export function uncarried(word: string): string {
  const deferred = (DEFERRED as Record<string, string>)[word]
  if (deferred) return deferred
  return (NO_MAC_ROUTE as Record<string, string>)[word] ?? ""
}

/**
 * What a refusal says about a route this console does not carry.
 *
 * A route that stands for one of the Mac's words says that word's sentence, so
 * the reader learns what is missing rather than that "something" is. A route
 * with no word at all — most of this daemon's API — says so plainly. Neither
 * is read for its wording by anything: the code is `cloud_not_carried` and the
 * screens choose their own sentence by it (`legacy/js/core/failure-text.js`).
 */
export function notCarriedDetail(method: string, path: string, word?: string): string {
  const sentence = uncarried(word || uncarriedWordOf(method, path))
  if (sentence) return sentence
  return `${method} ${path} is not carried over Clawdline Cloud: do it on the Mac itself.`
}
