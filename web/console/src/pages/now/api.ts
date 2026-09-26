import { RefusalError, TransportError, isRefusal } from "@clawdline/core/refusal"
import type { BrokerLandingList, TaskList } from "@clawdline/contract"
import type { Source } from "./freshness.js"

/**
 * The three reads behind the "now" page, and nothing else.
 *
 * **Four routes this daemon already answers, and no new storage** (review
 * §5.2). Every field is the wire's; nothing here is derived from another
 * block, so one block being wrong cannot make a second block wrong.
 *
 * They are four calls and not one, and that is the design rather than an
 * accident of having three lists: the page is read on a phone over a relay,
 * and a single answer covering all three would make one unreachable source a
 * blank page. Each call fails on its own and its block says so on its own.
 *
 * Every path here is carried over Clawdline Cloud, which is not automatic:
 * `/v1/orchestrator/tasks` rides on the machine descriptor (`ANSWERED_HERE`),
 * `work.v2.proposals` and `work.decisions` were already words, and
 * `/v1/orchestrator/landings` became one for this page (`cloud/carry.ts`).
 * A path that is not carried is a blank block on a phone and nothing at all
 * on this machine, which is the worst way to find out.
 */

/**
 * One Agent proposal in the Board's queue (work system v2 §10), as much of it
 * as this page draws; `WorkV2Proposal` in `pages/work/api.ts` has the whole row.
 */
export interface WaitingProposal {
  id: string
  title: string
  project_id: string
  created_at: number
}

/** One decision, as much of it as this page draws. */
export interface WaitingDecision {
  id: string
  question: string
  blocking: boolean
  created_at: number
}

/**
 * The Board's pending proposals. The v2 route carries no `source`: it is one
 * read of the work store, whole unless `truncated` says the page was cut.
 */
export interface ProposalPage {
  rows: WaitingProposal[]
  truncated: boolean
}

export interface DecisionPage {
  counts: Record<string, number>
  rows: WaitingDecision[]
  source: Source
}

async function call<T>(path: string): Promise<T> {
  const controller = new AbortController()
  const timer = setTimeout(() => controller.abort(), 15_000)
  let res: Response
  try {
    res = await fetch(path, { credentials: "same-origin", signal: controller.signal })
  } catch (cause) {
    throw new TransportError(`GET ${path} did not complete`, cause)
  } finally {
    clearTimeout(timer)
  }
  const text = await res.text()
  let parsed: unknown = null
  try {
    parsed = text ? JSON.parse(text) : null
  } catch (cause) {
    throw new TransportError(`${path} answered with something that is not JSON`, cause)
  }
  if (!res.ok) {
    if (isRefusal(parsed)) throw new RefusalError(res.status, parsed, path)
    throw new TransportError(`${path} answered ${res.status} with no refusal in it`)
  }
  return parsed as T
}

/**
 * What is running. The list carries finished tasks too — a child's tab stays
 * under its root after it ends — so the page keeps the rows that have not
 * reached a terminal state, which is what "in progress" means here.
 */
export const readTasks = () => call<TaskList>("/v1/orchestrator/tasks")

/** What has been delivered and not recorded, machine-wide, oldest first. */
export const readLandings = () => call<BrokerLandingList>("/v1/orchestrator/landings")

/**
 * The queue the person answers on the Board. Not `/v1/work/proposals`: that is
 * work system v1's list, still mounted after the cutover and empty, so reading
 * it drew "nothing is waiting" while the Board held proposals.
 */
export const readProposals = () => call<ProposalPage>("/v1/work/v2/proposals?state=pending")

/**
 * Questions a session asked (`POST /v1/orchestrator/decisions`); a person
 * reads and answers them here, not on the session route.
 */
export const readDecisions = () => call<DecisionPage>("/v1/work/decisions")

/** The states a task has not come back from. Everything else has finished. */
const LIVE = new Set(["queued", "spawning", "briefed"])

/** The rows of a task list that are still going. `unreadable` is not one: it never started. */
export function liveTasks(list: TaskList): TaskList["tasks"] {
  return list.tasks.filter((row) => LIVE.has(String(row.state)))
}
