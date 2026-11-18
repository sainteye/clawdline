import test from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see order.test.ts.
import { availableTranscript, TRANSCRIPT_QUIET_FAILURES, TRANSCRIPT_QUIET_MS, transcriptShow, type TranscriptRead } from "./transcript-trouble.ts"
import type { TranscriptPage } from "@clawdline/contract"
// @ts-expect-error -- a `.ts` path, for node; see order.test.ts.
import { Poller, type PollState, type ReadFailureKind } from "../poll.ts"

function read(extra: Partial<TranscriptRead>): TranscriptRead {
  return { hasData: false, failureKind: null, failures: 0, failingForMs: 0, ...extra }
}

/**
 * The pane's reads, driven through the real poller on a hand-turned clock:
 * each step settles one read with a page or a throw, `stepMs` after the last,
 * and answers what the pane would show right after it.
 */
async function pane(steps: ("ok" | "no-record" | ReadFailureKind)[], stepMs = 4000): Promise<string[]> {
  let t = 0
  let next: (() => void) | null = null
  const script = [...steps]
  const shown: string[] = []
  let state: PollState<TranscriptPage> | null = null
  const poller = new Poller<TranscriptPage>({
    read: async () => {
      const step = script.shift()!
      if (step === "ok" || step === "no-record") return availableTranscript({ id: "s", signature: "fixture", entries: [],
        evidence: step === "ok" ? "transcript" : "none", ...(step === "no-record" ? { note: "record temporarily unavailable" } : {}) })
      const err = new Error(step)
      ;(err as { kind?: string }).kind = step
      throw err
    },
    intervalMs: stepMs,
    classify: (err) => (err as { kind?: ReadFailureKind }).kind ?? "unknown",
    onChange: (s) => (state = s),
    now: () => t,
    setTimer: (fn) => (next = fn),
    clearTimer: () => (next = null),
  })
  poller.start()
  for (let i = 0; i < steps.length; i++) {
    await new Promise((r) => setImmediate(r))
    const s = state!
    shown.push(
      transcriptShow({
        hasData: s.data !== null,
        failureKind: s.failureKind,
        failure: s.failure,
        failures: s.failures,
        failingForMs: s.failingSince === null ? 0 : t - s.failingSince,
      }),
    )
    if (i < steps.length - 1) {
      t += stepMs
      const fire = next as (() => void) | null
      fire?.()
    }
  }
  poller.stop()
  return shown
}

test("a first read nobody answered, then a page, never shows the failure", async () => {
  assert.deepEqual(await pane(["unanswered", "ok"]), ["loading", "entries"])
})

test("three unanswered reads inside the quiet stretch still show loading", async () => {
  // 0, 4 and 8 s: three failures, but only 8 s of them.
  assert.deepEqual(await pane(["unanswered", "unanswered", "unanswered"]), ["loading", "loading", "loading"])
})

test("unanswered reads past both bounds show the failure", async () => {
  assert.deepEqual(await pane(["unanswered", "unanswered", "unanswered", "unanswered"]), [
    "loading",
    "loading",
    "loading",
    "failure",
  ])
  // Many fast failures are not enough on their own: the stretch must be long too.
  assert.deepEqual(await pane(["unanswered", "unanswered", "unanswered", "unanswered", "unanswered"], 1000), [
    "loading",
    "loading",
    "loading",
    "loading",
    "loading",
  ])
})

test("a refusal on the first read is shown at once", async () => {
  assert.deepEqual(await pane(["refused"]), ["failure"])
})

test("temporary Cloud refusals wait through the same grace as unanswered reads", () => {
  for (const code of ["offline", "cloud_reconnecting", "cloud_read_busy", "cloud_read_timeout", "read_reply_mismatch"]) {
    const temporary = { failureKind: "refused" as const, failure: { code }, failures: 1 }
    assert.equal(transcriptShow(read(temporary)), "loading", code)
    assert.equal(transcriptShow(read({ ...temporary, waitingForMs: 4000, failingForMs: 4000 })), "recovering", code)
    assert.equal(transcriptShow(read({ ...temporary, failures: 3, failingForMs: 12000 })), "failure", code)
  }
})

test("a slow first response gets a polite status without declaring it failed", () => {
  assert.equal(transcriptShow(read({ waitingForMs: 3999 })), "loading")
  assert.equal(transcriptShow(read({ waitingForMs: 4000 })), "recovering")
  assert.equal(transcriptShow(read({ hasData: true, waitingForMs: 4000 })), "entries")
})

test("permission and execution refusals remain errors even during grace", () => {
  for (const code of ["forbidden", "machine_pairing_required", "execution_generation_changed"]) {
    assert.equal(transcriptShow(read({ failureKind: "refused", failure: { code, status: 403 }, failures: 1 })), "failure")
  }
})

test("manual retry replaces an established error with a neutral reading status", () => {
  assert.equal(transcriptShow(read({ failureKind: "unanswered", failures: 4, failingForMs: 20000, retrying: true })), "recovering")
})

test("unavailable pages use the bounded recovery and preserve the last readable page", async () => {
  assert.deepEqual(await pane(["no-record", "ok"]), ["loading", "entries"])
  assert.deepEqual(await pane(["ok", "no-record", "ok"]), ["entries", "entries", "entries"])
  assert.deepEqual(await pane(["no-record", "no-record", "no-record", "no-record"]), ["loading", "loading", "loading", "failure"])
  assert.throws(() => availableTranscript({ id: "s", signature: "", entries: [], evidence: "none", note: "record unavailable" }),
    { code: "transcript_record_unavailable", message: "record unavailable" })
})

test("entries on screen and one missed read stay on screen with no notice", async () => {
  assert.deepEqual(await pane(["ok", "unanswered", "unknown", "ok"]), ["entries", "entries", "entries", "entries"])
})

test("a page after a run of failures starts the next run from nothing", async () => {
  // Four failures (shown), a page, then three more: the new run is inside
  // the quiet stretch again, so the entries stay without the notice.
  assert.deepEqual(
    await pane(["unanswered", "unanswered", "unanswered", "unanswered", "ok", "unanswered", "unanswered", "unanswered"]),
    ["loading", "loading", "loading", "failure", "entries", "entries", "entries", "entries"],
  )
})

test("the bounds are both required, at their edges", () => {
  const edge = { failureKind: "unanswered" as const, failures: TRANSCRIPT_QUIET_FAILURES, failingForMs: TRANSCRIPT_QUIET_MS }
  assert.equal(transcriptShow(read(edge)), "failure")
  assert.equal(transcriptShow(read({ ...edge, failures: TRANSCRIPT_QUIET_FAILURES - 1 })), "loading")
  assert.equal(transcriptShow(read({ ...edge, failingForMs: TRANSCRIPT_QUIET_MS - 1 })), "loading")
  assert.equal(transcriptShow(read({ ...edge, failureKind: "unknown" })), "failure", "an unknown throw waits the same")
})
