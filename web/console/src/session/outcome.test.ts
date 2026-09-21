// What a failed write may have done: `node --test web/console/src/session/*.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `order.test.ts`.
import { outcomeOf, writeIsOff } from "./outcome.ts"

// F3: "did not happen" has to be proved; everything else is "not known". The
// default is the uncertain answer, because the certain one sends a person to
// "try again", and a try again of something that ran is the same words twice.
test("only a refusal decided before the terminal was touched says it did not happen", () => {
  for (const code of [
    "bad_request", "empty_text", "too_large", "session_not_found", "session_unknown", "busy",
    "pictures_unavailable", "backend_unsupported", "write_disabled", "forbidden", "unauthorized",
    "menu_moved", "menu_unreadable", "menu_unverified", "idempotency_key_reused", "receipts_full",
    "store_busy", "store_unavailable", "close_blocked", "closeability_unknown",
  ]) {
    assert.equal(outcomeOf({ status: 409, code }), "not_done", code)
  }
  for (const code of [
    "terminal_io_failed", "send_failed", "close_failed", "internal", "request_outcome_unknown",
    "request_in_progress", "receipt_expired", "upstream_unreachable", "something_new", "",
  ]) {
    assert.equal(outcomeOf({ status: 502, code }), "unknown", code)
  }
})

test("nothing answered is not known; what the far side says wins", () => {
  assert.equal(outcomeOf({ status: null, code: "offline" }), "unknown", "a fetch that threw")
  // The Cloud seam says what it knows about its own layers (`relay-writer.ts`).
  assert.equal(outcomeOf({ status: 503, code: "offline", said: "not_done" }), "not_done")
  assert.equal(outcomeOf({ status: 409, code: "busy", said: "unknown" }), "unknown")
  assert.equal(outcomeOf({ status: 409, code: "busy", said: "maybe" }), "not_done", "a word it does not know is ignored")
})

test("a failed card retries only while the composer would still accept it", () => {
  assert.equal(writeIsOff("busy"), false)
  for (const code of ["write_disabled", "cloud_commands_disabled", "cloud_read_only", "cloud_read_needs_send_prompt", "unknown_sender"]) {
    assert.equal(writeIsOff(code), true, code)
  }
})
