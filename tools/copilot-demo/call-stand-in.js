// Injected into the console before it loads (docs/copilot-demo.md). Stands in
// for the two things a recording cannot have: a microphone and OpenAI's
// Realtime peer. The console's own client, sheet, orb, meters and tools run
// unchanged; this plays the recorded voices into the "microphone" and the
// "remote track" and speaks the Realtime data channel's events. Each clip's
// start is logged so the recorder can lay the same voices under the video.
(() => {
  const LINES = window.__DEMO_LINES // { id: [voice, text] }
  const ac = new AudioContext()
  const mic = ac.createMediaStreamDestination()
  const remote = ac.createMediaStreamDestination()
  const buffers = {}
  const ready = Promise.all(Object.keys(LINES).map(async (id) => {
    const res = await fetch('/demo-voices/' + id + '.ogg')
    buffers[id] = await ac.decodeAudioData(await res.arrayBuffer())
  }))
  const clips = []
  const wait = (ms) => new Promise((r) => setTimeout(r, ms))
  function play(id, dest) {
    const src = ac.createBufferSource(); src.buffer = buffers[id]; src.connect(dest)
    const at = ac.currentTime + 0.03
    src.start(at)
    clips.push({ id, wallMs: performance.timeOrigin + performance.now() + 30 })
    return new Promise((r) => { src.onended = r })
  }

  navigator.mediaDevices.getUserMedia = async () => { await ac.resume(); return mic.stream }

  let dc = null, onTrack = null
  const fromClient = []
  const waiters = []
  function heard(ev) {
    fromClient.push(ev)
    for (const w of [...waiters]) if (w.test(ev)) { waiters.splice(waiters.indexOf(w), 1); w.ok(ev) }
  }
  const awaitClient = (test) => {
    const hit = fromClient.find((e) => !e.__used && test(e))
    if (hit) { hit.__used = true; return Promise.resolve(hit) }
    return new Promise((ok) => waiters.push({ test: (e) => { if (test(e)) { e.__used = true; return true } return false }, ok }))
  }
  class FakeChannel {
    constructor() { this.readyState = 'connecting'; this.onopen = null; this.onmessage = null; this.onclose = null; this.onerror = null }
    send(text) { heard(JSON.parse(text)) }
    close() { this.readyState = 'closed' }
  }
  class FakePeer {
    constructor() { this.ontrack = null }
    createDataChannel() { dc = new FakeChannel(); return dc }
    addTrack() {}
    async createOffer() { return { type: 'offer', sdp: 'v=0\r\n' } }
    async setLocalDescription() {}
    async setRemoteDescription() {
      setTimeout(() => { this.ontrack?.({ streams: [remote.stream] }); dc.readyState = 'open'; dc.onopen?.() }, 250)
    }
    close() {}
  }
  window.RTCPeerConnection = FakePeer
  const realFetch = window.fetch.bind(window)
  window.fetch = (input, init) => {
    const url = typeof input === 'string' ? input : input.url
    if (url.startsWith('https://api.openai.com/v1/realtime/calls')) return Promise.resolve(new Response('v=0\r\n', { status: 201, headers: { 'Content-Type': 'application/sdp' } }))
    return realFetch(input, init)
  }

  const emit = (ev) => dc?.onmessage?.({ data: JSON.stringify(ev) })
  let resp = 0
  const usage = (seconds, words) => ({
    input_token_details: { text_tokens: 3600 + resp * 400, audio_tokens: 40 + resp * 30, cached_tokens_details: { text_tokens: resp ? 3200 + resp * 300 : 0, audio_tokens: 0 } },
    output_token_details: { text_tokens: Math.round(words * 1.3), audio_tokens: Math.round(seconds * 20) },
  })

  async function copilot(id) {
    const text = LINES[id][1]
    emit({ type: 'response.created' })
    await wait(120)
    emit({ type: 'response.output_audio.delta' })
    const dur = buffers[id].duration * 1000
    // Captions arrive a word at a time, and Chinese two characters at a time.
    const words = text.match(/[\p{L}\p{N}'’.,!?:;%-]*[A-Za-z0-9][\p{L}\p{N}'’.,!?:;%-]*\s*|[^\sA-Za-z0-9]{1,2}\s*|\s+/gu)
    const done = play(id, remote)
    const t0 = performance.now()
    for (let i = 0; i < words.length; i++) {
      const due = t0 + dur * 0.95 * (i / words.length)
      const left = due - performance.now(); if (left > 0) await wait(left)
      emit({ type: 'response.output_audio_transcript.delta', delta: words[i] })
    }
    await done
    emit({ type: 'response.output_audio.done' })
    emit({ type: 'response.done', response: { usage: usage(dur / 1000, words.length) } }); resp++
  }
  async function person(id) {
    emit({ type: 'input_audio_buffer.speech_started' })
    await play(id, mic)
    await wait(500) // semantic VAD waits a little past the last word
    emit({ type: 'input_audio_buffer.speech_stopped' })
    await wait(350)
    emit({ type: 'conversation.item.input_audio_transcription.completed', transcript: LINES[id][1] })
  }
  async function tool(name, args) {
    emit({ type: 'response.created' })
    const call_id = 'call_' + name
    emit({ type: 'response.function_call_arguments.done', call_id, name, arguments: JSON.stringify(args) })
    const out = await awaitClient((e) => e.type === 'conversation.item.create' && e.item?.call_id === call_id)
    await awaitClient((e) => e.type === 'response.create')
    window.__demo.tools.push({ name, output: out.item.output })
  }

  window.__demo = {
    clips, tools: [], stage: 'idle',
    async run(steps) {
      await ready
      await awaitClient((e) => e.type === 'session.update')
      emit({ type: 'session.updated', session: { tools: new Array(12).fill({}) } })
      await awaitClient((e) => e.type === 'response.create') // the greeting the client asks for
      for (const step of steps) {
        window.__demo.stage = step.at || window.__demo.stage
        if (step.pause) await wait(step.pause)
        if (step.copilot) await copilot(step.copilot)
        if (step.person) await person(step.person)
        if (step.tool) await tool(step.tool, step.args || {})
        if (step.hold) { window.__demo.stage = 'hold:' + step.hold; while (window.__demo.release !== step.hold) await wait(50); window.__demo.stage = step.hold }
      }
      window.__demo.stage = 'done'
    },
  }
})()
