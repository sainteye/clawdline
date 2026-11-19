import assert from "node:assert/strict"
import test from "node:test"
import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import { BotCall, botCallSendID, isBotCallSend, handoffInputRefused, type CallAudioSource, type CallInput, type CallObservation, type CallPorts } from "./bot-call.ts"

const fence: W.ProjectAssistantFence = {
  key: { machine_id: "source", bot_id: "bot" }, conversation_id: "conversation", binding_id: "", generation: 0,
  project: { machine_id: "source", project_id: "project", canonical_id: "project", kind: "repository" },
}
const bound = { ...fence, binding_id: "native", generation: 1 }
const turn = (id: string, state: W.ProjectAssistantTurnState, owner = bound) => ({
  id, state, fence: owner, message_id: id + "-input", started_at: "", failure_code: "",
})
const message = (turnID: string, text: string, owner = bound): W.ProjectAssistantMessage => ({
  id: turnID + "-reply", turn_id: turnID, text, fence: owner, at: "2026-01-01T00:00:00Z", seq: 1, quote_message_id: "", kind: "assistant",
})
const flush = async () => { await Promise.resolve(); await Promise.resolve() }

function fixture() {
  let current: CallObservation | null = { owner: "account/source", fence, activeTurn: "", turns: [], messages: [], caughtUp: true, frontier: 0 }
  let heard: (text: string) => void = () => {}
  let finishPlayback: () => void = () => {}
  const sent: CallInput[] = []
  const played: string[] = []
  const listening: boolean[] = []
  const playing: AbortSignal[] = []
  let closed = 0
  const audio = {
    listen(value: boolean) { listening.push(value) },
    play(text: string, signal: AbortSignal) {
      played.push(text)
      playing.push(signal)
      return new Promise<void>(resolve => { finishPlayback = resolve })
    },
    close() { closed++ },
  }
  const source: CallAudioSource = { async open(_signal, sink) { heard = sink; return audio } }
  const submit = async (input: CallInput) => {
    sent.push(input)
    return { state: "admitted" as const, turn: "turn-" + sent.length, eventSeq: sent.length }
  }
  const ports: CallPorts = { current: () => current, submit, changed() {} }
  const call = new BotCall(ports)
  return {
    audio, call, sent, played, playing, listening, ports, source,
    get closed() { return closed },
    hear(text: string) { heard(text) },
    finishPlayback() { finishPlayback() },
    show(update: Partial<CallObservation> | null) {
      current = update ? { ...current!, ...update } : null
      call.observe()
    },
  }
}

test("one call sends successive utterances, accepts its first binding and plays only its admitted final replies", async () => {
  const f = fixture()
  await f.call.start(f.source)
  f.hear("  First spoken request  ")
  f.hear("discard duplicate callback")
  await flush()
  assert.equal(f.call.state.phase, "waiting")
  assert.equal(f.sent.length, 1)
  assert.equal(f.sent[0].text, "First spoken request")
  assert.deepEqual(f.sent[0].fence, fence)
  f.show({ fence: bound, turns: [turn("turn-1", "waiting_permission")], activeTurn: "turn-1", frontier: 1 })
  assert.equal(f.call.state.phase, "permission")
  f.hear("do not answer approval by voice")
  f.show({
    turns: [turn("turn-1", "succeeded")], activeTurn: "", caughtUp: false,
    messages: [message("text-turn", "Unrelated"), message("turn-1", "Older generation", fence)],
  })
  assert.deepEqual(f.played, [])
  f.show({ caughtUp: true, messages: [message("turn-1", "First reply"), message("text-turn", "Unrelated")] })
  f.call.observe()
  assert.deepEqual(f.played, ["First reply"])
  assert.equal(f.call.state.phase, "speaking")
  f.hear("discard playback echo")
  f.finishPlayback()
  await flush()

  f.hear("Second spoken request")
  await flush()
  assert.equal(f.sent.length, 2)
  assert.equal(f.sent[0].callID, f.sent[1].callID)
  assert.deepEqual(f.sent[1].fence, bound)
  f.show({ turns: [turn("turn-2", "succeeded")], frontier: 2, messages: [message("turn-2", "Second reply")] })
  assert.deepEqual(f.played, ["First reply", "Second reply"])
  assert.notEqual(botCallSendID(f.sent[0].callID), botCallSendID(f.sent[0].callID))
  assert.ok(isBotCallSend(botCallSendID(f.sent[0].callID)))
})

test("mute drops input and interrupt cancels only the current playback", async () => {
  const f = fixture()
  await f.call.start(f.source)
  f.call.mute()
  f.hear("muted audio")
  assert.equal(f.listening.at(-1), false)
  assert.equal(f.sent.length, 0)
  f.call.mute()
  f.hear("question")
  await flush()
  f.show({ fence: bound, turns: [turn("turn-1", "succeeded")], messages: [message("turn-1", "Stop reading")] })
  f.call.mute()
  f.call.interrupt()
  assert.ok(f.playing[0].aborted)
  assert.deepEqual(f.call.state, { phase: "listening", muted: true })
  f.finishPlayback()
  await flush()
  f.hear("still muted")
  assert.equal(f.sent.length, 1)
  f.call.end()
  assert.equal(f.closed, 1)
})

test("hangup during microphone permission disposes its late stream and ignores stale callbacks", async () => {
  const f = fixture()
  let grant = () => {}
  let heard: (text: string) => void = () => {}
  const opening = f.call.start({ open(_signal, sink) {
    heard = sink
    return new Promise(resolve => { grant = () => resolve(f.audio) })
  } })
  assert.equal(f.call.state.phase, "opening")
  f.call.end()
  await f.call.start(f.source)
  heard("from the first microphone")
  grant()
  await opening
  assert.equal(f.closed, 1)
  assert.equal(f.sent.length, 0)
  assert.equal(f.call.state.phase, "listening")
})

for (const [reason, update] of [
  ["account or source epoch", { owner: "different account/source" }],
  ["Bot", { fence: { ...fence, key: { ...fence.key, bot_id: "other" } } }],
  ["conversation", { fence: { ...fence, conversation_id: "other" } }],
  ["source disconnect", null],
] as const) {
  test(reason + " ends the call before late admission or playback", async () => {
    const f = fixture()
    let admit = () => {}
    f.ports.submit = input => {
      f.sent.push(input)
      return new Promise(resolve => { admit = () => resolve({state: "admitted", turn: "turn-1", eventSeq: 1}) })
    }
    await f.call.start(f.source)
    f.hear("one request")
    f.show(update)
    admit()
    await flush()
    f.hear("late recognition")
    assert.equal(f.call.state.phase, "disconnected")
    assert.equal(f.closed, 1)
    assert.ok(f.sent[0].signal.aborted)
    assert.equal(f.sent.length, 1)
    assert.deepEqual(f.played, [])
  })
}

test("an ambiguous write stops the call without retrying or reading back its later reply", async () => {
  const f = fixture()
  f.ports.submit = async input => { f.sent.push(input); return { state: "sendUnknown" } }
  await f.call.start(f.source)
  f.hear("original request")
  await flush()
  f.show({ fence: bound, turns: [turn("turn-1", "succeeded")], messages: [message("turn-1", "Late reply")] })
  assert.equal(f.call.state.phase, "sendUnknown")
  assert.equal(f.sent.length, 1)
  assert.deepEqual(f.played, [])
})

test("only the source's explicit handoff admission refusal releases a call input", () => {
  const refusal = {contract_version: "project-assistants.v1", error: {code: "pa_stale_version", field: "handoff_revision", retry_after_seconds: 0}}
  assert.equal(handoffInputRefused(refusal), true)
  for (const cause of [null, new Error("lost response"), {...refusal, contract_version: "unknown"},
    {...refusal, error: {...refusal.error, code: "pa_outcome_unknown"}},
    {...refusal, error: {...refusal.error, field: "handoff_admission"}},
    {...refusal, error: {...refusal.error, retry_after_seconds: 1}},
    {contract_version: "project-assistants.v1", error: {code: "pa_outcome_unknown"}, response: refusal}]) {
    assert.equal(handoffInputRefused(cause), false)
  }
})
