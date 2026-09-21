import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see cloud/relay-reader.test.ts.
import { machineAccessProblem, machineListAnswer, machineListRefusal, retryReason } from "./state.ts"

test("the machine list keeps loading, authoritative empty, and refusal apart", () => {
  assert.deepEqual(machineListAnswer({ machines: [], syncing: true }), { phase: "loading" })
  assert.deepEqual(machineListAnswer({ machines: [], syncing: false }), { phase: "empty_authoritative" })
  assert.deepEqual(machineListRefusal({ code: "unauthorized" }), {
    phase: "refused",
    code: "unauthorized",
    next: "sign_in",
  })
  assert.deepEqual(machineListRefusal({ code: "unreadable_envelope" }), {
    phase: "refused",
    code: "unreadable_envelope",
    next: "pair",
  })
  assert.deepEqual(machineListRefusal(new TypeError("Failed to fetch")), {
    phase: "refused",
    code: "machine_list_unanswered",
    next: "retry",
  })
})

test("an untyped reconnect distinguishes this browser being offline from an unknown interruption", () => {
  assert.deepEqual(retryReason({ code: "rate_limited" }, true), { kind: "named", code: "rate_limited" })
  assert.deepEqual(retryReason(new TypeError("Failed to fetch"), false), { kind: "browser_offline" })
  assert.deepEqual(retryReason(new TypeError("Failed to fetch"), true), { kind: "unknown" })
})

test("an access problem follows its receipt back to the machine id", () => {
  const error = Object.assign(new Error("cannot open"), {
    code: "unknown_sender",
    viewerEvent: { n: 17 },
  })
  const viewerEvents = {
    snapshot: () => ({
      rows: [
        { n: 16, data: { routed_machine: "machine-before" } },
        { n: 17, data: { routed_machine: "machine-five" } },
      ],
    }),
  }
  assert.deepEqual(machineAccessProblem({ type: "error", error }, viewerEvents), {
    machine: "machine-five",
    code: "unknown_sender",
  })
})

test("an access problem with no machine is withheld instead of accusing one of five", () => {
  assert.equal(machineAccessProblem({ type: "error", error: { code: "unknown_sender" } }), null)
  assert.deepEqual(
    machineAccessProblem({ type: "error", error: { code: "unknown_sender", detail: { machine: "machine-two" } } }),
    { machine: "machine-two", code: "unknown_sender" },
  )
})
