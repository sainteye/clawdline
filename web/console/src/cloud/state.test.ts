import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see cloud/relay-reader.test.ts.
import { accessProblemAfterRead, machineAccessProblem, machineListAnswer, machineListRefusal, readProblemOffersPairing, retryReason } from "./state.ts"

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


test("timeout and a named pairing refusal retain their machine without guessing a cause", () => {
  assert.deepEqual(machineAccessProblem({ type: "read", identity: {machine: "m"}, error: {code: "cloud_read_timeout"} }), {machine: "m", code: "cloud_read_timeout"})
  assert.deepEqual(machineAccessProblem({ type: "error", machine: "m", error: {code: "machine_pairing_required"} }), {machine: "m", code: "machine_pairing_required"})
  assert.equal(machineAccessProblem({type: "read", machine: "m"}), null)
  assert.equal(machineAccessProblem({type: "read", machine: "m", realign: true,
    error: {code: "machine_pairing_required"}}), null, "a retained failure is not a new access problem")
  assert.equal(machineAccessProblem({type: "error", error: {code: "cloud_read_timeout"}}), null)
})


test("cached data and other machines do not clear a failed read", () => {
  const held = {machine: "m", code: "cloud_read_timeout"}
  for (const event of [
    {type:"sessions", machine:"m", realign:true}, {type:"read", machine:"other"}, {type:"read"},
    {type:"read", machine:"m", realign:true}, {type:"read", machine:"m", error:{code:"offline"}},
  ]) assert.equal(accessProblemAfterRead(held,event),held)
  assert.equal(accessProblemAfterRead(held,{type:"read", identity:{machine:"m"}}),null)
})

test("only a named pairing failure offers pairing again", () => {
  for (const code of ["cloud_read_timeout", "cloud_read_unavailable", "socket_error", "offline"]) {
    assert.equal(readProblemOffersPairing(code), false, code)
  }
  assert.equal(readProblemOffersPairing("unknown_sender"), true)
  assert.equal(readProblemOffersPairing("machine_pairing_required"), true)
})

test("live Session updates clear transient warnings while retained replays and other machines do not", () => {
  for (const code of ["cloud_read_timeout", "cloud_read_unavailable", "offline", "socket_error"]) {
    const held = {machine: "m", code}
    for (const type of ["sessions", "session_status"]) {
      const event = {type, identity: {machine: "m"}, realign: false}
      assert.equal(accessProblemAfterRead(held, event), null, `${type} recovered after ${code}`)
      assert.equal(accessProblemAfterRead(held, {...event, realign: true}), held)
      assert.equal(accessProblemAfterRead(held, {...event, identity: {machine: "other"}}), held)
      assert.equal(accessProblemAfterRead(held, {...event, error: {code: "offline"}}), held)
      const pairing = {machine: "m", code: "machine_pairing_required"}
      assert.equal(accessProblemAfterRead(pairing, event), pairing, "a publication cannot verify outbound permission")
    }
    assert.equal(accessProblemAfterRead(held, {type: "orchestrator", machine: "m"}), held,
      "presence alone cannot prove the Session view recovered")
  }
})
