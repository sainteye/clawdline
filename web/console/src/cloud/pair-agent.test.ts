// Handing a pairing offer to the Mac app, against a fake reply handler:
// `node --test web/console/src/cloud/pair-agent.test.ts`.
//
// The fake stands where `window.webkit.messageHandlers.clawdlinePairAgent`
// stands in the Mac app's Cloud tab (`shell/darwin/CloudPairing.swift`,
// `CloudPairAgent`): `postMessage` answers a promise of what the shell replied.
// What is checked is what the page sends — three values and nothing else — and
// that every answer, including none, becomes one of the card's sentences.
import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
// @ts-expect-error -- a `.ts` path, for node; see cloud/forget.test.ts.
import { PAIR_AGENT_EVENT, PAIR_AGENT_HANDLER, agentOutcome, agentSaid, elapsedSaid, handPairToAgent, listenPairAgent, pairAgentAvailable, progressSaid, readPairAgentEvent, type PairAgentReply } from "./pair-agent.ts"

function host(answer: (body: unknown) => Promise<unknown>) {
  const sent: unknown[] = []
  return {
    sent,
    window: {
      webkit: {
        messageHandlers: {
          [PAIR_AGENT_HANDLER]: {
            postMessage(body: unknown) {
              sent.push(body)
              return answer(body)
            },
          },
        },
      },
    },
  }
}

const INPUT = { offer: "eyJhY2NvdW50X2lkIjoiYWNjdF8xIn0", machineID: "mac_build-box", machineName: "build-box" }

test("only a page with the shell's handler offers the hand-off", () => {
  assert.equal(pairAgentAvailable({}), false)
  assert.equal(pairAgentAvailable({ webkit: { messageHandlers: {} } }), false)
  assert.equal(pairAgentAvailable({ webkit: { messageHandlers: { [PAIR_AGENT_HANDLER]: {} } } }), false)
  assert.equal(pairAgentAvailable(host(() => Promise.resolve(null)).window), true)
})

test("the page sends the offer, the id and the name, and no text of its own", async () => {
  const h = host(() => Promise.resolve({ ok: true, mode: "agent", task_id: "7ab00050-0000-4000-8000-000000000050", trust: "recorded" }))
  const reply = await handPairToAgent(INPUT, h.window)
  assert.deepEqual(h.sent, [{ offer: INPUT.offer, machine_id: "mac_build-box", machine_name: "build-box" }])
  assert.deepEqual(reply, { ok: true, mode: "agent", taskID: "7ab00050-0000-4000-8000-000000000050", trust: "recorded" })
})

test("every answer becomes a reply, and none of them throws", async () => {
  const cases: Array<[() => Promise<unknown>, PairAgentReply]> = [
    [() => Promise.resolve({ ok: true, mode: "direct" }), { ok: true, mode: "direct", taskID: "", trust: "" }],
    [() => Promise.resolve({ ok: false, error: "cancelled" }), { ok: false, error: "cancelled" }],
    [() => Promise.resolve({ ok: false, error: "machine_id is not a Clawdline Cloud machine id." }), { ok: false, error: "machine_id is not a Clawdline Cloud machine id." }],
    [() => Promise.resolve({ ok: true, mode: "something else" }), { ok: false, error: "refused" }],
    [() => Promise.resolve(undefined), { ok: false, error: "refused" }],
    [() => Promise.reject(new Error("gone")), { ok: false, error: "unreachable" }],
  ]
  for (const [answer, want] of cases) {
    assert.deepEqual(await handPairToAgent(INPUT, host(answer).window), want)
  }
  assert.deepEqual(await handPairToAgent(INPUT, {}), { ok: false, error: "unavailable" })
})

test("the status line says sent, finished here, cancelled, or refused with the reason", () => {
  const word = (key: string, holes: Record<string, string | number> = {}) => `${key}${JSON.stringify(holes)}`
  const said = (agent: "sending" | PairAgentReply) => [agentOutcome(agent), agentSaid(agent, "build-box", word as never)]
  assert.deepEqual(said("sending"), ["sending", "cloudPairAgentSending{}"])
  assert.deepEqual(said({ ok: true, mode: "agent", taskID: "t", trust: "" }), ["agent", 'cloudPairAgentSent{"machine":"build-box"}'])
  assert.deepEqual(said({ ok: true, mode: "direct", taskID: "", trust: "" }), ["direct", "cloudPairAgentDirect{}"])
  assert.deepEqual(said({ ok: false, error: "cancelled" }), ["cancelled", "cloudPairAgentCancelled{}"])
  assert.deepEqual(said({ ok: false, error: "busy" }), ["refused", 'cloudPairAgentRefused{"reason":"busy"}'])
})

// The hosted browser's helper acts only on a machine it has already paired
// with. The target stays inaccessible until the offer completes there.
test("the pairing card sends its offer through a paired helper and keeps the manual route", () => {
  const here = dirname(fileURLToPath(import.meta.url))
  const panel = readFileSync(resolve(here, "PairPanel.tsx"), "utf8")
  const gate = readFileSync(resolve(here, "CloudGate.tsx"), "utf8")
  assert.match(gate, /machine\.id !== \(pairRequest\.mode === "offer" \? pairRequest\.machine\?\.id : ""\)/)
  assert.match(gate, /machineDescriptor\?\.\(machine\.id\)\?\.machine\?\.commands\?\.includes\("pair-agent-start"\)/)
  assert.match(gate, /_machineRequestAs[\s\S]*"pair-agent-start"/)
  assert.match(gate, /_machineRequestAs[\s\S]*"pair-agent-status"/)
  assert.match(panel, /autoHelper\.current = asked \? selectedHelper\?\.id/)
  assert.match(panel, /props\.onAgentStart\(helper\.id/)
  assert.match(panel, /cloudPairCloudHand/)
  const button = panel.indexOf('id="cloud-pair-agent"')
  assert.match(panel, /const manual = \(\s*<>[\s\S]*id="cloud-pair-command"/)
  const copyBox = panel.indexOf("{manual}")
  assert.ok(button > 0 && copyBox > button, "the hand-off button is drawn before the copy box")
  assert.match(panel, /handPairToAgent\(\{ offer:/)
  assert.match(panel, /id="cloud-pair-copy"/)
  const agentButton = panel.slice(panel.lastIndexOf("<button", button), button)
  assert.match(agentButton, /className="go"/)
  const manual = panel.indexOf('id="cloud-pair-manual"')
  assert.ok(manual > button, "the disclosure is under the hand-off")
  assert.match(panel, /\{handOff \? \(\s*<details/)
  assert.match(panel, /setManualOpen\(true\)/)
})

const TASK = "7ab00050-0000-4000-8000-000000000050"

test("an event is read only for the task this card handed off, and only in words the card knows", () => {
  assert.deepEqual(readPairAgentEvent({ task_id: TASK, state: "working" }, TASK), { stage: "working", summary: "", failedReason: "" })
  assert.deepEqual(readPairAgentEvent({ task_id: TASK, state: "failed", failed_reason: " ssh: no route " }, TASK), {
    stage: "failed",
    summary: "",
    failedReason: "ssh: no route",
  })
  assert.equal(readPairAgentEvent({ task_id: "another", state: "done" }, TASK), null)
  assert.equal(readPairAgentEvent({ task_id: TASK, state: "paired" }, TASK), null)
  assert.equal(readPairAgentEvent({ task_id: TASK }, TASK), null)
  assert.equal(readPairAgentEvent(null, TASK), null)
  assert.equal(readPairAgentEvent({ task_id: "", state: "done" }, ""), null)
  assert.equal(readPairAgentEvent({ task_id: TASK, state: "done", summary: "x".repeat(900) }, TASK)?.summary.length, 600)
})

test("the card listens only where the shell's handler exists, and stops when asked", () => {
  const listeners = new Map<string, (event: unknown) => void>()
  const window = {
    ...host(() => Promise.resolve(null)).window,
    addEventListener(type: string, fn: (event: unknown) => void) {
      listeners.set(type, fn)
    },
    removeEventListener(type: string, fn: (event: unknown) => void) {
      if (listeners.get(type) === fn) listeners.delete(type)
    },
  }
  const seen: string[] = []
  const stop = listenPairAgent(TASK, (p) => seen.push(p.stage), window)
  const fire = listeners.get(PAIR_AGENT_EVENT)
  assert.ok(fire, "listening for the shell's event")
  fire({ detail: { task_id: TASK, state: "starting" } })
  fire({ detail: { task_id: "someone else", state: "done" } })
  fire({ detail: { task_id: TASK, state: "done" } })
  assert.deepEqual(seen, ["starting", "done"])
  stop()
  assert.equal(listeners.size, 0)

  const browser = { addEventListener() { throw new Error("a browser without the shell was listened on") } }
  listenPairAgent(TASK, () => {}, browser)()
})

test("every stage has its sentence, and the time reads in seconds then minutes", () => {
  const word = (key: string, holes: Record<string, string | number> = {}) => `${key}${JSON.stringify(holes)}`
  const said = (p: Parameters<typeof progressSaid>[0]) => progressSaid(p, "build-box", word as never)
  const at = (stage: "starting" | "dialog" | "working" | "done" | "failed", failedReason = "") => ({ stage, summary: "", failedReason })
  assert.equal(said(null), "cloudPairAgentStarting{}")
  assert.equal(said(at("starting")), "cloudPairAgentStarting{}")
  assert.equal(said(at("dialog")), "cloudPairAgentDialog{}")
  assert.equal(said(at("working")), 'cloudPairAgentWorking{"machine":"build-box"}')
  assert.equal(said(at("done")), 'cloudPairAgentDone{"machine":"build-box"}')
  assert.equal(said(at("failed", "ssh: no route")), 'cloudPairAgentFailed{"reason":"ssh: no route"}')
  assert.equal(said(at("failed")), 'cloudPairAgentFailed{"reason":"cloudPairAgentFailedUnknown{}"}')
  assert.equal(elapsedSaid(42.7, word as never), 'cloudPairAgentElapsedSeconds{"seconds":42}')
  assert.equal(elapsedSaid(125, word as never), 'cloudPairAgentElapsedMinutes{"minutes":2,"seconds":5}')
  assert.equal(elapsedSaid(-3, word as never), 'cloudPairAgentElapsedSeconds{"seconds":0}')
})

test("both languages carry every hand-off sentence", () => {
  const here = dirname(fileURLToPath(import.meta.url))
  const words = readFileSync(resolve(here, "../next-strings.ts"), "utf8")
  for (const key of [
    "cloudPairAgentHand", "cloudPairAgentHandWhen", "cloudPairAgentSending", "cloudPairAgentSent",
    "cloudPairAgentDirect", "cloudPairAgentCancelled", "cloudPairAgentRefused",
    "cloudPairAgentProgressTitle", "cloudPairAgentStarting", "cloudPairAgentDialog", "cloudPairAgentWorking",
    "cloudPairAgentDone", "cloudPairAgentFailed", "cloudPairAgentFailedUnknown", "cloudPairAgentRetry",
    "cloudPairAgentElapsedSeconds", "cloudPairAgentElapsedMinutes", "cloudPairAgentTrustAsk", "cloudPairManual",
  ]) {
    assert.equal(words.split(`    ${key}:`).length - 1, 2, `${key} is in both languages`)
  }
})
