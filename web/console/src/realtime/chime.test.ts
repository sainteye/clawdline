import { test, type TestContext } from "node:test"
import assert from "node:assert/strict"

// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { borrowAudioSession, playChime, primeChime } from "./chime.ts"

class FakeContext extends EventTarget {
  static made: FakeContext[] = []
  currentTime = 0
  tones: number[] = []
  closed = 0
  resumed = 0
  state = "running"
  destination = {}
  constructor() { super(); FakeContext.made.push(this) }
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

test("overlapping chimes release playback only after the last sound and restore the original category", t => {
  withAudio((contexts, session) => {
    const first = primeChime(), second = primeChime()
    first.play()
    t.mock.timers.tick(200)
    second.play()
    t.mock.timers.tick(800)
    assert.equal(session.type, "playback", "the second chime still owns playback")
    t.mock.timers.tick(200)
    assert.equal(session.type, "auto", "a later page visit must not inherit playback")
    assert.deepEqual(contexts.map(context => context.closed), [1, 1])
  }, t)
})

test("a finishing chime does not overwrite another audio owner's explicit category", t => {
  withAudio((_contexts, session) => {
    primeChime().play()
    session.type = "play-and-record"
    t.mock.timers.tick(1000)
    assert.equal(session.type, "play-and-record")
  }, t)
})

test("an interrupted chime releases playback and closes without waiting for its timeout", t => {
  withAudio((contexts, session) => {
    primeChime().play()
    contexts[0].state = "suspended"
    contexts[0].dispatchEvent(new Event("statechange"))
    assert.equal(session.type, "auto")
    assert.equal(contexts[0].closed, 1)
    t.mock.timers.tick(1000)
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

test("the voice call keeps duplex audio across a short cue and restores the prior route", t => {
  withAudio((_contexts, session) => {
    const call = borrowAudioSession("play-and-record")
    assert.equal(session.type, "play-and-record")
    const cue = primeChime()
    cue.play()
    assert.equal(session.type, "play-and-record")
    const replacement = borrowAudioSession("play-and-record")
    call()
    t.mock.timers.tick(1000)
    assert.equal(session.type, "play-and-record")
    replacement()
    assert.equal(session.type, "auto")
  }, t)
})
