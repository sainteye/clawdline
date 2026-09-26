// What this console carries to a machine, word by word, and what it does not.
//
// The seam used to decide that in two hand-written places: a `switch` over four
// GET paths in `relay-reader.ts` and a table of routes in `relay-writer.ts`.
// Both were right when they were written and neither could notice when they
// stopped being: the machine's vocabulary is `cloudops.Implemented()` on the Go
// daemon, it grows, and nothing here read it. That is how `info` came to be a
// word the machine answers and the browser never asks — the status line under
// every cloud session said "Loading…" for as long as that was true, and the
// machine's log held no refusal, because the refusal was this page's own.
//
// So the words live here, once, in three lists that together must be exactly
// the machine's vocabulary:
//
//   `CARRIED`           — asked of the machine, with the console route that asks.
//   `DEFERRED`          — the machine answers it; this console does not ask
//                         yet, and says why and where it can be done instead.
//   `NO_MACHINE_ROUTE`  — in the machine's vocabulary with nothing behind it
//                         there.
//
// **What makes this sourced rather than written down twice** is
// `TestTheConsoleCarryTableMatchesThisMachinesVocabulary`
// (internal/app/cloudops/carry_test.go). It reads this file and compares its
// three key sets with `Implemented()` and `Vocabulary()`. A word added to the
// catalog and to neither list here fails that test by name, and so does a word
// here that the machine has stopped knowing. The runtime half is
// `RelayReader.drift()`, which asks the machine's own descriptor the same
// question on the page, for a machine running ahead of the bundle it is being read
// from.
//
// Nothing is imported here, so both the console and `node --test` read it as it
// is.

/** One word of the machine's Cloud vocabulary. */
export type CloudWord = CarriedWord | keyof typeof DEFERRED | keyof typeof NO_MACHINE_ROUTE

/**
 * A word this console asks a machine for.
 *
 * `relay-writer.ts` declares each of its routes' words through this type, so a
 * route that names a word missing from `CARRIED` is a compile error rather than
 * a fourteenth place the vocabulary is written down.
 */
export type CarriedWord = keyof typeof CARRIED

/**
 * The words this console asks a machine for, and the console route that asks.
 *
 * The value is documentation and the key is the contract: `writeRoute` and the
 * reader's own cases name these keys, and TypeScript refuses a word that is not
 * one of them.
 */
export const CARRIED = {
  agent: "GET /v1/sessions/{id}/agents/{agent}?limit=",
  answer: "POST /v1/sessions/{id}/key",
  board: "GET /v1/board?project=&item=",
  // The capacity block on Settings (`pages/settings/CapacityBlock.tsx`), which
  // every capacity push names: a machine read with no parameter.
  capacity: "GET /v1/capacity",
  // The dashboard behind the session counts (`machine/`): this machine's CPU
  // and memory and each session's share, a machine read with no parameter.
  "machine-usage": "GET /v1/machine/usage",
  "board.items": "GET /v1/board?project=&audience=&cursor=&limit=",
  end: "POST /v1/sessions/{id}/close",
  focus: "POST /v1/sessions/{id}/focus",
  git: "GET /v1/sessions/{id}/git",
  "git-diff": "GET /v1/sessions/{id}/git/diff?path=",
  image: "GET /v1/artifacts/images/{id}?session={id}",
  info: "GET /v1/sessions/{id}/info[?parts=summary]",
  interrupt: "POST /v1/sessions/{id}/interrupt",
  intents: "POST /v1/intents",
  "past-sessions": "GET /v1/places/{id}/sessions[/{assistant}]",
  places: "GET /v1/places",
  "project-worktree-lifecycle": "GET /v1/projects/{project}/worktrees",
  "project-icon-copy": "PUT /v1/projects/{id}/icon",
  // Project settings sync (docs/project-sync.md). The two source reads are
  // also asked of *another* paired machine by `cloud/project-sync.ts`, which
  // is how a mirror reads its source without either machine reaching the other.
  "project-manifest": "GET /v1/project-sync/manifest",
  "project-entry": "GET /v1/project-sync/entry?repo=",
  "project-mirror": "GET /v1/project-sync/mirror",
  "project-mirror-apply": "POST /v1/project-sync/mirror",
  "project-mirror-detach": "DELETE /v1/project-sync/mirror?repo=",
  "project-worktree-lifecycle-refresh": "POST /v1/projects/{project}/worktrees/refresh",
  projects: "GET /v1/projects",
  "push-key": "GET /v1/push/key",
  "push-subscribe": "POST /v1/push/subscribe",
  "push-test": "POST /v1/push/test",
  "push-unsubscribe": "POST /v1/push/unsubscribe",
  resume: "POST /v1/places/{id}/resume[/{assistant}]/{past}",
  // The live screen (`session/ScreenPanel.tsx`). It sat in `DEFERRED` and in
  // `DEFERRED_ASKED` while this machine answered it, so 「即時畫面」 on a phone
  // only ever said to go and look on the machine — including from the Waiting
  // card that sends a person there to read the question.
  screen: "GET /v1/sessions/{id}/screen",
  // The sessions a reboot took away (docs/session-restore.md), offered where
  // the empty session list stands (`session/Restore.tsx`). The read is the
  // machine's; the two commands carry the sheet's press key as their request.
  "restorable-sessions": "GET /v1/sessions/restorable",
  "restore-sessions": "POST /v1/sessions/restorable/restore",
  "dismiss-restorable": "POST /v1/sessions/restorable/dismiss",
  "schedule-create": "POST /v1/orchestrator/schedules",
  "schedule-delete": "DELETE /v1/orchestrator/schedules/{id}",
  "schedule-webhook-bind-v1": "POST /v1/orchestrator/schedule-webhooks/bind",
  "schedule-run": "POST /v1/orchestrator/schedules/{id}/run",
  "schedule-update": "PATCH /v1/orchestrator/schedules/{id}",
  schedule: "GET /v1/orchestrator/schedules/{id}",
  schedules: "GET /v1/orchestrator/schedules",
  send: "POST /v1/sessions/{id}/send",
  // The Shell panel (`session/ShellPanel.tsx`): the tail of one background
  // command's output, polled while the panel is open.
  shell: "GET /v1/sessions/{id}/shells/{shell}?bytes=",
  // No console route asks this: the copied client does, on every connection
  // that did not take over a live socket (`_recoverSessions`), so a list
  // opened after a relay eviction is sent every row rather than whichever
  // changed.
  "sessions.snapshot": "cloud-client.js _recoverSessions (on connect)",
  "smart-title": "POST /v1/sessions/{id}/smart-title",
  "snippet-create": "POST /v1/snippets",
  "snippet-delete": "DELETE /v1/snippets/{id}",
  "snippet-order": "POST /v1/snippets/order",
  "snippet-update": "PATCH /v1/snippets/{id}",
  snippets: "GET /v1/snippets",
  start: "POST /v1/places/{id}/start[/{assistant}[/{model}]]",
  timeline: "GET /v1/timeline?project=&entry=&cursor=&environment=&category=&upcoming=",
  transcript: "GET /v1/transcript?session={id}",
  // The token bill (`pages/work/TokenBill.tsx`). Machine reads, not session
  // reads: a conversation id is the ledger's key and not a row this page holds.
  // Whether compacting early paid (docs/token-ledger.md "Did compacting early
  // pay"): a machine read with its one query field.
  "usage.compare-compaction": "GET /v1/usage/compare-compaction[?since=]",
  "usage.item": "GET /v1/usage/items/{id}",
  "usage.session": "GET /v1/usage/sessions/{conversation}",
  "usage.task": "GET /v1/usage/tasks/{id}",
  // Things waiting to be verified (`pages/verify.tsx`, docs/verifications.md):
  // the phone is where the person reads them, so every route crosses. Two
  // machine reads and five commands, each with its body as the route reads it.
  "verification.close": "POST /v1/verifications/{id}/close",
  "verification.create": "POST /v1/verifications",
  "verification.criterion": "POST /v1/verifications/{id}/criteria/{index}",
  "verification.delete": "DELETE /v1/verifications/{id}[?force=1]",
  "verification.get": "GET /v1/verifications/{id}",
  "verification.list": "GET /v1/verifications",
  "verification.note": "POST /v1/verifications/{id}/notes",
  voice: "POST /v1/voice",
  "work.backlog": "GET /v1/work/backlog[?project=&cursor=]",
  "work.board": "GET /v1/work/board[?project=&cursor=]",
  "work.decisions": "GET /v1/work/decisions",
  "work.digests": "GET /v1/work/digests?kind=",
  "work.proposals": "GET /v1/work/proposals[?project=]",
  "work.v2.assign": "POST /v1/work/v2/items/{id}/assign",
  "work.v2.remind": "POST /v1/work/v2/items/{id}/remind",
  "work.v2.cancel": "POST /v1/work/v2/items/{id}/cancel",
  "work.v2.complete": "POST /v1/work/v2/items/{id}/complete",
  "work.v2.create": "POST /v1/work/v2/items",
  "work.v2.edit": "PATCH /v1/work/v2/items/{id}",
  "work.v2.image-create": "POST /v1/work/v2/items/{id}/images",
  "work.v2.image-delete": "DELETE /v1/work/v2/items/{id}/images/{image}",
  "work.v2.image": "GET /v1/work/v2/images/{id}[?size=thumb]",
  "work.v2.item": "GET /v1/work/v2/items/{id}",
  "work.v2.items": "GET /v1/work/v2/items[?project=]",
  "work.v2.search": "GET /v1/work/v2/items?project=&status=&q=",
  "work.v2.proposal-resolve": "POST /v1/work/v2/proposals/{id}/{accept|reject}",
  "work.v2.proposals": "GET /v1/work/v2/proposals?state=",
  "work.v2.session-todos": "GET /v1/work/v2/session-todos/{terminal}",
  "work.v2.todo-action": "POST /v1/work/v2/session-todos/{terminal}/{id}/{action}",
  "work.v2.todo-create": "POST /v1/work/v2/session-todos/{terminal}",
  "work.v2.todo-image-create": "POST /v1/work/v2/session-todos/{terminal}/{id}/images",
} as const

/**
 * Words this machine answers that this console does not ask for yet.
 *
 * Each sentence is what a person reading a refusal needs: what is missing and
 * where the thing can be done instead. They are here rather than in a document
 * because this is the list the drift guard reads, so a word cannot be quietly
 * left out of both.
 *
 * **A sentence here is a decision, and a decision expires.** `git` sat here
 * saying the working tree was not read over Cloud yet, months after this machine
 * began answering the word and after `legacy/git-bridge.ts` had been copied in
 * and was asking for the route on every press of 「Git 變更」; the page refused
 * its own request and the person was told "無法讀取 Git 變更". `schedules` sat
 * here the same way and for the same length of time. Neither was caught,
 * because the guard below only ever asked whether a word was *classified*,
 * never whether the sentence was still true. `DEFERRED_ASKED` is the half of
 * that question a machine can answer.
 */
export const DEFERRED = {
  document: "A document's text is not read over Clawdline Cloud yet: open it on the machine.",
  documents: "A session's documents are not listed over Clawdline Cloud yet: open them on the machine.",
  // `key` and `answer` are one command under two names on the wire. This
  // console sends a waiting card's press as `answer`, because only that
  // spelling carries `expect`, the fingerprint of the question the press was
  // chosen for (F1, `RelayWriter.press`). `key` is the older spelling and is
  // deliberately never sent.
  key: "A waiting card's press is sent as `answer`, which names the question it answers; `key` is the older spelling of the same command.",
} as const

/**
 * The deferred words a screen in this console already asks for.
 *
 * **This is what "deliberately not carried yet" costs, named.** A word is in
 * `DEFERRED` because somebody decided not to carry it; it is in this list
 * because some page here issues the request anyway, so the refusal a person
 * meets is this bundle refusing itself, in front of a screen that was built to
 * show the answer. Deferring is still legal — the list is not a failure — but
 * it is no longer silent, and this is the roster to re-read when choosing what
 * to carry next.
 *
 * It is held to both directions by `carry.test.ts`, which reads every `/v1/…`
 * path this console spells and asks `uncarriedWordOf` what each one stands
 * for: a deferred word some screen asks for and this list does not name fails,
 * and so does a name here that no screen asks for any more. That is the guard
 * `git` needed and did not have — the day `git-bridge.ts` landed, the entry
 * had to say so.
 */
export const DEFERRED_ASKED: readonly (keyof typeof DEFERRED)[] = ["document", "documents"]

/**
 * Words the machine knows and has nothing behind. Asking for one is answered
 * `unknown_command` by the bridge, so the console does not ask: the sentence
 * here is what it says instead.
 *
 * A word that gains a route on the machine fails the drift guard until it is moved
 * into `CARRIED` or `DEFERRED`, which is the whole reason this list is a list
 * and not a comment.
 */
export const NO_MACHINE_ROUTE = {
  "diagnostics.events": "This machine does not take a page's diagnostic events over Clawdline Cloud.",
  "diagnostics.report": "This machine does not take a diagnostic report over Clawdline Cloud.",
  dispatch: "Dispatching a task over Clawdline Cloud has no pinned wire shape on this machine: dispatch it on the machine.",
  skills: "A session's skills are not listed over Clawdline Cloud: read them on the machine.",
} as const

/**
 * Own-origin routes this seam answers itself, with no Cloud word behind them.
 *
 * They are not part of the machine's vocabulary and so not part of the Go drift
 * guard; they are here so that the one place that says what a hosted page can
 * reach says all of it, and `carry.test.ts` holds the reader to this list so
 * that being here is a fact and not a note. `/v1/sessions` is the relay's own
 * snapshot of this machine's rows, `/v1/orchestrator/tasks` is the dispatched
 * work that rides on the same machine's descriptor, `/v1/health` is whether
 * the line is up, and `/v1/strings` is the bundle's own catalog
 * (`cloud/strings.ts`).
 *
 * **The task list is here and not in a word list**, and that is the whole
 * answer to "which class does it belong to". A word is something a browser
 * asks a machine to do; this is something the machine already said. It publishes its
 * dispatched work on the `orch/` snapshot beside `machine.commands`
 * (`internal/transport/cloud/tasklist.go`), the copied client keeps every
 * descriptor it opens, and reading it back costs the relay nothing — exactly
 * as the session rows do. `dispatch` stays in `NO_MACHINE_ROUTE` and is a
 * different thing: it is *starting* a task from a browser, which this machine
 * refuses by name (`cloudDispatchUnpinned`) because no pinned wire shape says
 * where the task file would be written. Reading the list was never that.
 */
export const ANSWERED_HERE: readonly string[] = [
  "/v1/sessions",
  "/v1/orchestrator/tasks",
  "/v1/health",
  "/v1/strings",
]

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

/** Every word in the three lists, sorted — what the machine's vocabulary must be. */
export function words(): CloudWord[] {
  return [...Object.keys(CARRIED), ...Object.keys(DEFERRED), ...Object.keys(NO_MACHINE_ROUTE)].sort() as CloudWord[]
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
  // `/v1/snippets*`, `board`, `board.items` and `timeline` were all here until
  // this console began asking for them. What is carried is parsed once — by
  // the reader's own case and by `writeRoute` — and a second spelling of it
  // here would be a second thing to keep right. A POST to `/v1/board` is an
  // item write, which this daemon refuses by name at the route, so it is not a
  // word this table has to name either.
  if (head === "diagnostics" && a === "report") return "diagnostics.report"
  // The schedule list, single read and writes are carried, so they are parsed
  // once by the reader's own cases and `writeRoute`, and are deliberately not
  // spelled a second time here.
  if (head === "sessions" && a && b) {
    switch (b) {
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
  return (NO_MACHINE_ROUTE as Record<string, string>)[word] ?? ""
}

/**
 * What a refusal says about a route this console does not carry.
 *
 * A route that stands for one of the machine's words says that word's sentence, so
 * the reader learns what is missing rather than that "something" is. A route
 * with no word at all — most of this daemon's API — says so plainly. Neither
 * is read for its wording by anything: the code is `cloud_not_carried` and the
 * screens choose their own sentence by it (`legacy/js/core/failure-text.js`).
 */
export function notCarriedDetail(method: string, path: string, word?: string): string {
  const sentence = uncarried(word || uncarriedWordOf(method, path))
  if (sentence) return sentence
  return `${method} ${path} is not carried over Clawdline Cloud: do it on the machine itself.`
}
