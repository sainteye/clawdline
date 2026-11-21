import assert from "node:assert/strict"
import { test } from "node:test"
import { readFileSync } from "node:fs"
import { runInNewContext } from "node:vm"
// @ts-expect-error -- Node reads the TypeScript source in its strip-types test runner.
import { connectLiveDictation, readDictationSettings, writeDictationSetting } from "./live-dictation.ts"

test("read the selected engine from the active machine and recognize older daemons", async () => {
  const signal = new AbortController().signal
  const connection = { current: () => true, fetch: async () => Response.json({}) }
  assert.equal(await readDictationSettings(connection, signal), null)
  connection.fetch = async () => Response.json({ voice_transcription: "whisper" })
  assert.deepEqual(await readDictationSettings(connection, signal), { engine: "whisper", polish: false })
  connection.fetch = async () => Response.json({ voice_transcription: "openai_realtime", voice_polish: true })
  assert.deepEqual(await readDictationSettings(connection, signal), { engine: "openai_realtime", polish: true })
})

test("the recording panel saves the same machine preference with a verified response", async () => {
  const signal = new AbortController().signal
  let current = true, calls = 0
  const connection = {
    current: () => current,
    fetch: async (path: RequestInfo | URL, init?: RequestInit) => {
      calls += 1
      assert.equal(path, "/v1/settings/voice")
      assert.equal(init?.method, "POST")
      assert.equal(new Headers(init?.headers).get("idempotency-key"), "recording-choice")
      assert.deepEqual(JSON.parse(String(init?.body)), { voice_transcription: "whisper" })
      return Response.json({ voice_transcription: "whisper" })
    },
  }
  assert.equal(await writeDictationSetting(connection, { voice_transcription: "whisper" }, "recording-choice", signal), true)
  current = false
  assert.equal(await writeDictationSetting(connection, { voice_transcription: "whisper" }, "recording-choice", signal), false)
  assert.equal(calls, 1)
})

test("automatic correction saves only the existing correction preference", async () => {
  const calls: unknown[] = []
  const connection = {
    current: () => true,
    fetch: async (_: RequestInfo | URL, init?: RequestInit) => {
      calls.push(JSON.parse(String(init?.body)))
      return Response.json({ voice_polish: true })
    },
  }
  const signal = new AbortController().signal
  assert.equal(await writeDictationSetting(connection, { voice_polish: true }, "correction", signal), true)
  assert.deepEqual(calls, [{ voice_polish: true }])
})

function fakeRealtime(t: import("node:test").TestContext, direction: RTCRtpTransceiverDirection = "sendrecv") {
  const oldPeer = globalThis.RTCPeerConnection
  const oldFetch = globalThis.fetch
  t.after(() => {
    globalThis.RTCPeerConnection = oldPeer
    globalThis.fetch = oldFetch
  })
  const sent: unknown[] = []
  const previews: string[] = []
  const track = { readyState: "live", enabled: false } as MediaStreamTrack
  const sender = {} as RTCRtpSender
  const stream = { getAudioTracks: () => [track] } as MediaStream
  const channel = new class extends EventTarget {
    readyState = "open"
    send(data: string) { sent.push(JSON.parse(data)) }
    close() { this.readyState = "closed" }
    receive(event: unknown) {
      this.dispatchEvent(new MessageEvent("message", { data: JSON.stringify(event) }))
    }
  }()
  let peerClosed = false
  globalThis.RTCPeerConnection = class extends EventTarget {
    createDataChannel(label: string) {
      assert.equal(label, "oai-events")
      return channel
    }
    addTrack(sentTrack: MediaStreamTrack) {
      assert.equal(sentTrack, track)
      assert.equal(track.enabled, false)
      return sender
    }
    getTransceivers() { return [{ sender, currentDirection: direction }] }
    async createOffer() { return { type: "offer", sdp: "fixture-offer" } }
    async setLocalDescription() {}
    async setRemoteDescription(answer: RTCSessionDescriptionInit) {
      assert.deepEqual(answer, { type: "answer", sdp: "fixture-answer" })
      channel.receive({ type: "session.created", session: { type: "transcription" } })
    }
    close() { peerClosed = true }
  } as unknown as typeof RTCPeerConnection

  globalThis.fetch = fakeFetch
  const connect = () => connectLiveDictation({
    current: () => true,
    fetch: async (path, init) => {
      assert.equal(path, "/v1/voice/live-transcription")
      assert.equal(init?.method, "POST")
      assert.equal(new Headers(init?.headers).get("idempotency-key"), "one-dictation")
      return Response.json({ client_secret: "fixture-transcription-grant" })
    },
  }, stream, "one-dictation", new AbortController().signal, (text) => previews.push(text))
  return { connect, receive, sent, previews, channel, get peerClosed() { return peerClosed } }

  function receive(type: string, item_id: string, extra = {}) {
    channel.receive({ type, item_id, ...extra })
  }
  async function fakeFetch(path: RequestInfo | URL, init?: RequestInit) {
    assert.equal(path, "https://api.openai.com/v1/realtime/calls")
    assert.equal(new Headers(init?.headers).get("authorization"), "Bearer fixture-transcription-grant")
    assert.equal(init?.body, "fixture-offer")
    return new Response("fixture-answer")
  }
}

test("WebRTC sends no assistant request and accepts only the final text for its commit", async (t) => {
  const realtime = fakeRealtime(t)
  const { receive, sent, previews, channel } = realtime
  const session = await realtime.connect()
  receive("conversation.item.input_audio_transcription.delta", "utterance", { delta: "初稿" })
  assert.deepEqual(previews, ["初稿"])
  const finished = session.finish()
  assert.deepEqual(sent, [{ type: "input_audio_buffer.commit" }])
  receive("input_audio_buffer.committed", "utterance")
  receive("conversation.item.input_audio_transcription.completed", "unrelated", { transcript: "Foreign result" })
  receive("conversation.item.input_audio_transcription.completed", "utterance", { transcript: "定稿" })
  assert.equal(await finished, "定稿")
  session.close()
  assert.equal(realtime.peerClosed, true)
  assert.equal(channel.readyState, "closed")
})

test("an open Realtime data channel cannot start dictation without a negotiated audio sender", async (t) => {
  const realtime = fakeRealtime(t, "inactive")
  await assert.rejects(realtime.connect(), { code: "voice_realtime_audio_unavailable" })
  assert.equal(realtime.peerClosed, true)
  assert.deepEqual(realtime.sent, [])
})

test("an OpenAI empty audio commit returns a typed failure and closes WebRTC", async (t) => {
  const realtime = fakeRealtime(t)
  const session = await realtime.connect()
  const finished = session.finish()
  realtime.channel.receive({ type: "error", error: { code: "input_audio_buffer_commit_empty" } })
  await assert.rejects(finished, { code: "voice_realtime_audio_empty" })
  assert.equal(realtime.peerClosed, true)
})

test("the Whisper worklet turns 48 kHz input into a bounded 16 kHz window and skips digital silence", () => {
  const audio: ArrayBuffer[] = []
  let create: () => { process(input: Float32Array[][]): boolean } = () => { throw new Error("missing processor") }
  runInNewContext(readFileSync(new URL("./whisper-preview.worklet.js", import.meta.url), "utf8"), {
    sampleRate: 48000,
    AudioWorkletProcessor: class { port = { postMessage(pcm: ArrayBuffer) { audio.push(pcm) } } },
    registerProcessor(_: string, ctor: new () => { process(input: Float32Array[][]): boolean }) {
      create = () => new ctor()
    },
  })
  const processor = create()
  processor.process([[new Float32Array(3 * 48000)]])
  assert.equal(audio.length, 0)
  processor.process([[new Float32Array(3 * 48000).fill(0.25)]])
  assert.equal(audio.length, 1)
  assert.equal(audio[0].byteLength, 96000)
  assert.equal(new DataView(audio[0]).getInt16(0, true), 8191)
})
