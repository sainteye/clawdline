// Injected into the console before it loads, for a GPT-Live take
// (record.mjs --live, docs/copilot-demo.md). Unlike call-stand-in.js it does
// not stand in for OpenAI: the call is a real GPT-Live session the stand-in
// daemon opens with the machine's key. This stands in for the microphone
// only, playing the person's recorded lines into the stream getUserMedia
// returns, and it watches the call so the recorder knows when the copilot
// has finished a turn. The copilot's own voice is recorded from the remote
// track as it arrives.
(() => {
  const LINES = window.__DEMO_LINES // { id: [voice, text] }; only the person's lines are played
  const ac = new AudioContext()
  const mic = ac.createMediaStreamDestination()
  // GPT-Live needs the input audio to keep flowing, silence included; an idle
  // destination sends nothing and OpenAI closed the session after about 20 s.
  const hum = ac.createConstantSource(); hum.offset.value = 0; hum.connect(mic); hum.start()
  const buffers = {}
  const ready = Promise.all(Object.keys(LINES).filter((id) => id.startsWith('p')).map(async (id) => {
    const res = await fetch('/demo-voices/' + id + '.ogg')
    buffers[id] = await ac.decodeAudioData(await res.arrayBuffer())
  }))
  const wall = () => performance.timeOrigin + performance.now()
  const clips = []
  navigator.mediaDevices.getUserMedia = async () => { await ac.resume(); return mic.stream }

  const watch = {
    lastSaid: 0, // when the copilot's transcript last grew
    lastVoice: 0, // when the copilot's voice was last heard on the remote track
    personEnd: 0, // when the person's last line ended
    delegations: 0, // backend work started and not yet completed
    responses: new Set(), // backend responses created and not yet finished (one per tool round)
    latestNoteAt: 0, // when the call last handed the live model a Session's latest state
    reportNotes: 0, // how many Session report batches the call has handed the live model
    reportNoteAt: 0,
    awaiting: false, // backend work asked for (a delegation, or a continuation after a tool result) whose response has not started
    pending: new Set(), // backend function calls without a result yet
    said: [], // the copilot's words, as the transcript gave them
    backend: [], // the backend's own answers, which the live model speaks from
    errors: [],
    tools: [],
  }
  const recording = { chunks: [], start: 0, recorder: null }
  function record(stream) {
    if (recording.recorder) return
    const r = new MediaRecorder(stream, { mimeType: 'audio/webm;codecs=opus' })
    r.ondataavailable = (e) => { if (e.data.size) recording.chunks.push(e.data) }
    r.start(1000)
    recording.recorder = r
    recording.start = wall()
    // The transcript runs ahead of the voice, so when the copilot is speaking
    // is read from the sound itself: a level above the floor marks it.
    const analyser = ac.createAnalyser(); analyser.fftSize = 2048
    ac.createMediaStreamSource(stream).connect(analyser)
    const buf = new Float32Array(analyser.fftSize)
    setInterval(() => {
      analyser.getFloatTimeDomainData(buf)
      let sum = 0; for (const v of buf) sum += v * v
      if (Math.sqrt(sum / buf.length) > 0.01) watch.lastVoice = wall()
    }, 100)
  }
  function observe(m) {
    if (m.type === 'session.output_transcript.delta') { watch.lastSaid = wall(); const last = watch.said[watch.said.length - 1]; if (last && wall() - last.at < 2500) last.text += m.delta; else watch.said.push({ at: wall(), text: String(m.delta) }) }
    else if (m.type === 'session.delegation.created') { watch.delegations++; watch.awaiting = true }
    else if (m.type === 'response.event' && m.event?.type === 'response.output_text.delta') { const last = watch.backend[watch.backend.length - 1]; if (last && last.id === m.event.item_id) last.text += m.event.delta; else watch.backend.push({ id: m.event.item_id, at: wall(), text: String(m.event.delta) }) }
    else if (m.type === 'response.event' && m.event?.type === 'response.created') { watch.responses.add(m.event.response?.id); watch.awaiting = false }
    else if (m.type === 'response.event' && /^response\.(completed|failed|cancelled|incomplete)$/.test(m.event?.type ?? '')) { watch.responses.delete(m.event.response?.id); watch.delegations = Math.max(0, watch.delegations - 1) }
    else if (m.type === 'response.event' && m.event?.type === 'response.output_item.done' && m.event.item?.type === 'function_call') {
      watch.pending.add(m.event.item.call_id); watch.tools.push({ name: m.event.item.name, args: m.event.item.arguments, at: wall() })
    } else if (m.type === 'error') watch.errors.push(m.error?.code + ': ' + m.error?.message)
  }
  const Real = window.RTCPeerConnection
  window.RTCPeerConnection = class extends Real {
    constructor(...a) {
      super(...a)
      this.addEventListener('track', (e) => record(e.streams[0] || new MediaStream([e.track])))
    }
    createDataChannel(...a) {
      const dc = super.createDataChannel(...a)
      dc.addEventListener('message', (ev) => { try { observe(JSON.parse(ev.data)) } catch {} })
      const send = dc.send.bind(dc)
      dc.send = (text) => {
        try {
          const m = JSON.parse(text)
          if (m.type === 'response.item.create' && m.item?.type === 'function_call_output') watch.pending.delete(m.item.call_id)
          if (m.type === 'response.create') watch.awaiting = true
          if (m.type === 'session.commentary.append' && /latest state of Session/.test(String(m.content))) watch.latestNoteAt = wall()
          if (m.type === 'session.commentary.append' && /Report this Session batch/.test(String(m.content))) { watch.reportNotes++; watch.reportNoteAt = wall() }
        } catch {}
        return send(text)
      }
      return dc
    }
  }

  window.__demo = {
    clips, watch, stage: 'idle',
    /** Plays one of the person's lines into the microphone; resolves when it has been said. */
    async person(id) {
      await ready
      const src = ac.createBufferSource(); src.buffer = buffers[id]; src.connect(mic)
      src.start(ac.currentTime + 0.03)
      clips.push({ id, wallMs: wall() + 30 })
      await new Promise((r) => { src.onended = r })
      watch.personEnd = wall()
    },
    /** A click that should be answered (a Session opened from the list) is the person's turn. */
    mark() { watch.personEnd = wall() },
    /**
     * Whether the copilot has answered the person's last line and finished:
     * it spoke after the line ended, has been silent for `quietMs`, and has no
     * backend response or tool call outstanding.
     */
    answered(quietMs) {
      const now = wall()
      const spoke = Math.max(watch.lastSaid, watch.lastVoice)
      return spoke > watch.personEnd && now - spoke >= quietMs && watch.pending.size === 0 && watch.responses.size === 0 && !watch.awaiting
    },
    /** The copilot's recorded voice, as base64 webm, and when it started (page wall clock). */
    async voice() {
      const r = recording.recorder
      if (!r) return { start: 0, data: '' }
      // A call that ended by itself may already have stopped the recorder.
      if (r.state !== 'inactive') await new Promise((ok) => { r.onstop = ok; r.stop() })
      const blob = new Blob(recording.chunks, { type: 'audio/webm' })
      const data = await new Promise((ok) => { const f = new FileReader(); f.onload = () => ok(String(f.result).split(',')[1] ?? ''); f.readAsDataURL(blob) })
      return { start: recording.start, data }
    },
  }
})()
