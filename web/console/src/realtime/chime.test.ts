import { test, type TestContext } from "node:test"
import assert from "node:assert/strict"

// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { playChime, primeChime } from "./chime.ts"

class FakeContext {
  static made: FakeContext[] = []
  currentTime = 0
  tones: number[] = []
  closed = 0
  resumed = 0
  destination = {}
  constructor() { FakeContext.made.push(this) }
  resume() { this.resumed++; return Promise.resolve() }
  close() { this.closed++; return Promise.resolve() }
  createGain() {
    return { gain: { value: 0, setValueAtTime() {}, linearRampToValueAtTime() {} }, connect() {}, disconnect() {} }
  }
  createOscillator() {
    const context = this
    return { frequency: { value: 0 }, connect() {}, start() { context.tones.push(this.frequency.value) }, stop() {} }
  }
}

function withAudio(run: (contexts: FakeContext[], session: { type: string }) => void, t: TestContext) {
  const scope = globalThis as unknown as Record<string, unknown>
  const had = scope.AudioContext
  const hadNavigator = Object.getOwnPropertyDescriptor(globalThis, "navigator")
  const session = { type: "auto" }
  FakeContext.made = []
  scope.AudioContext = FakeContext
  Object.defineProperty(globalThis, "navigator", { value: { audioSession: session }, configurable: true })
  t.mock.timers.enable({ apis: ["setTimeout"] })
  try {
    run(FakeContext.made, session)
  } finally {
    t.mock.timers.reset()
    scope.AudioContext = had
    if (hadNavigator) Object.defineProperty(globalThis, "navigator", hadNavigator)
  }
}

test("the chime is the copilot's two rising notes", () => {
  const context = new FakeContext()
  playChime(context as unknown as AudioContext)
  assert.deepEqual(context.tones, [880, 1320])
})

test("a primed chime opens its context in the tap, sounds once on play and then closes", (t) => {
  withAudio((contexts, session) => {
    const chime = primeChime()
    assert.equal(contexts.length, 1)
    assert.equal(contexts[0].resumed, 1)
    assert.deepEqual(contexts[0].tones, [])
    chime.play()
    assert.deepEqual(contexts[0].tones, [880, 1320])
    assert.equal(session.type, "playback")
    chime.play()
    assert.deepEqual(contexts[0].tones, [880, 1320])
    t.mock.timers.tick(1000)
    assert.equal(contexts[0].closed, 1)
    assert.equal(session.type, "auto")
  }, t)
})

test("a dropped chime closes silently", (t) => {
  withAudio((contexts) => {
    const chime = primeChime()
    chime.drop()
    chime.play()
    assert.deepEqual(contexts[0].tones, [])
    assert.equal(contexts[0].closed, 1)
  }, t)
})

test("a chime nobody plays closes itself once no recording could still be waiting", (t) => {
  withAudio((contexts) => {
    primeChime()
    t.mock.timers.tick(179999)
    assert.equal(contexts[0].closed, 0)
    t.mock.timers.tick(1)
    assert.equal(contexts[0].closed, 1)
  }, t)
})
