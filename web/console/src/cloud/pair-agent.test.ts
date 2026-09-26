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
import { PAIR_AGENT_HANDLER, agentOutcome, agentSaid, handPairToAgent, pairAgentAvailable, type PairAgentReply } from "./pair-agent.ts"

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
  const h = host(() => Promise.resolve({ ok: true, mode: "agent", task_id: "7ab00050-0000-4000-8000-000000000050" }))
  const reply = await handPairToAgent(INPUT, h.window)
  assert.deepEqual(h.sent, [{ offer: INPUT.offer, machine_id: "mac_build-box", machine_name: "build-box" }])
  assert.deepEqual(reply, { ok: true, mode: "agent", taskID: "7ab00050-0000-4000-8000-000000000050" })
})

test("every answer becomes a reply, and none of them throws", async () => {
  const cases: Array<[() => Promise<unknown>, PairAgentReply]> = [
    [() => Promise.resolve({ ok: true, mode: "direct" }), { ok: true, mode: "direct", taskID: "" }],
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
  assert.deepEqual(said({ ok: true, mode: "agent", taskID: "t" }), ["agent", 'cloudPairAgentSent{"machine":"build-box"}'])
  assert.deepEqual(said({ ok: true, mode: "direct", taskID: "" }), ["direct", "cloudPairAgentDirect{}"])
  assert.deepEqual(said({ ok: false, error: "cancelled" }), ["cancelled", "cloudPairAgentCancelled{}"])
  assert.deepEqual(said({ ok: false, error: "busy" }), ["refused", 'cloudPairAgentRefused{"reason":"busy"}'])
})

// The card itself is React and is read here as source, as unpaired-rows.test.ts
// reads it: the hand-off is offered only for a named machine inside the Mac
// app, above the copy box, which stays for every other browser.
test("the waiting card offers the hand-off above the copy box, only where the shell can take it", () => {
  const here = dirname(fileURLToPath(import.meta.url))
  const panel = readFileSync(resolve(here, "PairPanel.tsx"), "utf8")
  assert.match(panel, /const handOff = offer && asked && pairAgentAvailable\(\) \? asked : null/)
  const button = panel.indexOf('id="cloud-pair-agent"')
  const copyBox = panel.indexOf('id="cloud-pair-command"')
  assert.ok(button > 0 && copyBox > button, "the hand-off button is drawn before the copy box")
  assert.match(panel, /handPairToAgent\(\{ offer: state\.fragment, machineID: handOff\.id, machineName: handOff\.name \}\)/)
  assert.match(panel, /id="cloud-pair-copy"/)
})

test("both languages carry every hand-off sentence", () => {
  const here = dirname(fileURLToPath(import.meta.url))
  const words = readFileSync(resolve(here, "../next-strings.ts"), "utf8")
  for (const key of [
    "cloudPairAgentHand", "cloudPairAgentHandWhen", "cloudPairAgentSending", "cloudPairAgentSent",
    "cloudPairAgentDirect", "cloudPairAgentCancelled", "cloudPairAgentRefused",
  ]) {
    assert.equal(words.split(`    ${key}:`).length - 1, 2, `${key} is in both languages`)
  }
})
