import { RefusalError } from "@clawdline/core"
import * as L from "../../legacy/bridge.js"
import type { Item } from "./api.js"
import { workWord } from "./words.js"

/** A time on these routes (Unix seconds) as a person reads it: month, day, hour, minute. */
export function when(seconds: number | null | undefined): string {
  if (!seconds) return ""
  const zh = (document.documentElement.lang || navigator.language || "").toLowerCase().startsWith("zh")
  return new Date(seconds * 1000).toLocaleString(zh ? "zh-TW" : "en", {
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  })
}

/** Who holds an item: "you" for the person, the session otherwise. */
export function ownerWords(owner: string | null): string {
  if (!owner) return ""
  return workWord("owner", { owner: owner === "user" ? workWord("ownerYou") : owner })
}

/**
 * What the facts say an item is now — the derived reason, never a stored
 * state (D04). The catalog's own words where it has them.
 */
export function reasonWords(it: Item): string {
  const T = L.strings
  switch (it.derived.reason) {
    case "task_running":
      return T.webTaskRunning
    case "delivered_unlanded":
      return T.webProjectDelivered
    case "landed":
      return T.webProjectLanded
    case "no_dispatch":
      return workWord("reasonNoDispatch")
    case "attempts_failed":
      return workWord("reasonAttemptsFailed")
    case "redispatched":
      return workWord("reasonRedispatched")
    case "accepted":
      return workWord("reasonAccepted")
    case "unconfirmed":
      return workWord("reasonUnconfirmed")
    case "dropped":
      return workWord("reasonDropped")
    case "done_elsewhere":
      return workWord("reasonDoneElsewhere")
  }
  return it.derived.reason
}

/** An item's tasks in one line, naming only the counts that are not zero. */
export function taskWords(it: Item): string {
  const t = it.derived.tasks
  if (!t.total && !it.unknown_tasks) return workWord("noTasks")
  const parts = [workWord("tasks", { n: t.total })]
  if (t.live) parts.push(workWord("tasksLive", { n: t.live }))
  if (t.delivered) parts.push(workWord("tasksDelivered", { n: t.delivered }))
  if (t.landed) parts.push(workWord("tasksLanded", { n: t.landed }))
  if (t.failed) parts.push(workWord("tasksFailed", { n: t.failed }))
  if (it.unknown_tasks) parts.push(workWord("tasksUnknown", { n: it.unknown_tasks }))
  return parts.join(" · ")
}

/**
 * A failed command as a sentence. A refusal names what the daemon said; a
 * connection that never answered says that nothing is known to have been done,
 * which is not the same as "it failed" (refusal.ts).
 */
export function failureWords(e: unknown): string {
  if (e instanceof RefusalError) {
    if (e.code === "version_conflict") return workWord("failedConflict")
    // `detail` is the daemon's English. The catalog has a sentence per code
    // and puts `code · ref` after it; the fallback is deliberately generic so
    // an unknown code is not repeated as both prose and tag.
    return L.failureSentence(e, workWord("failed"))
  }
  return workWord("failedNetwork")
}

/** Today in the Mac's calendar, as a start date is written. */
export function today(): string {
  const d = new Date()
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}
