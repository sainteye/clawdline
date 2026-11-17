import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { test } from 'node:test'
import { runInNewContext } from 'node:vm'

// Exercise the same event observer injected into recording Chrome, without OpenAI.
function fixture() {
  let now = 1000, receive
  const window = { __DEMO_LINES: {}, RTCPeerConnection: class {
    addEventListener() {}
    createDataChannel() { return { addEventListener: (_, fn) => { receive = fn }, send() {} } }
  } }
  const context = {
    window, performance: { timeOrigin: 0, now: () => now }, navigator: { mediaDevices: {} },
    AudioContext: class {
      createMediaStreamDestination() { return {} }
      createConstantSource() { return { offset: {}, connect() {}, start() {} } }
    },
  }
  runInNewContext(readFileSync(new URL('../../../tools/copilot-demo/live-stand-in.js', import.meta.url), 'utf8'), context)
  const channel = new window.RTCPeerConnection().createDataChannel()
  return { demo: window.__demo, at: value => { now = value },
    send: content => channel.send(JSON.stringify({ type: 'session.commentary.append', content })),
    say: delta => receive({ data: JSON.stringify({ type: 'session.output_transcript.delta', delta }) }),
  }
}

test('the demo waits for both named summaries, a sentence end, and remote audio quiet', () => {
  const f = fixture()
  f.send('Report this Session batch now: "Work A": waiting / "工作 B": running')
  f.at(2000); f.say('Work A is waiting.')
  f.demo.watch.lastVoice = 2000
  f.at(2800)
  assert.equal(f.demo.firstBatchFinished(800), false, 'one summary is not the batch')
  f.say(' 工作 B 正在')
  f.demo.watch.lastVoice = 2800
  f.at(4000)
  assert.equal(f.demo.firstBatchFinished(800), false, 'a pause inside a sentence is insufficient')
  f.say('執行。')
  f.demo.watch.lastVoice = 4500
  f.at(4900)
  assert.equal(f.demo.firstBatchFinished(800), false, '400 ms of audio quiet is insufficient')
  f.at(5300)
  assert.equal(f.demo.firstBatchFinished(800), true)
})

test('a latest-state note is not answered by eight seconds of silence or old speech', () => {
  const f = fixture()
  f.at(2000); f.say('The kept summary.')
  f.demo.watch.lastVoice = 3000
  f.at(4000); f.send('The latest state of Session "Work A" just arrived.')
  f.demo.watch.personEnd = f.demo.watch.latestNoteAt
  f.at(13000)
  assert.equal(f.demo.answered(3500), false)
  f.say('The latest work is complete.')
  f.demo.watch.lastVoice = 14000
  f.at(17500)
  assert.equal(f.demo.answered(3500), true)
})
