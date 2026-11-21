import type { captureMachineFetch } from "../cloud/install.js"

export type DictationConnection = ReturnType<typeof captureMachineFetch>

export type DictationEngine = "whisper" | "openai_realtime"

function engineFrom(value: unknown): DictationEngine | null {
  return value === "whisper" || value === "openai_realtime" ? value : null
}

export async function readDictationEngine(connection: DictationConnection, signal: AbortSignal): Promise<DictationEngine | null> {
  try {
    const response = await connection.fetch("/v1/settings/voice", { cache: "no-store", signal })
    return response.ok ? engineFrom((await response.json()).voice_transcription) : null
  } catch {
    return null
  }
}

export async function writeDictationEngine(
  connection: DictationConnection, engine: DictationEngine, request: string, signal: AbortSignal,
): Promise<boolean> {
  if (!connection.current()) return false
  try {
    const response = await connection.fetch("/v1/settings/voice", {
      method: "POST", headers: { "Content-Type": "application/json", "Idempotency-Key": request },
      body: JSON.stringify({ voice_transcription: engine }), signal,
    })
    return response.ok && engineFrom((await response.json()).voice_transcription) === engine
  } catch {
    return false
  }
}

export class LiveDictationError extends Error {
  readonly code: string

  constructor(code = "voice_realtime_failed") {
    super(code)
    this.code = code
  }
}

interface LiveDictation {
  finish(): Promise<string>
  close(): void
}

type LiveEvent = {
  type?: string
  session?: { type?: string }
  item_id?: string
  delta?: string
  transcript?: string
  error?: { code?: string }
}

export async function connectLiveDictation(
  connection: DictationConnection,
  stream: MediaStream,
  request: string,
  signal: AbortSignal,
  preview: (text: string) => void,
): Promise<LiveDictation> {
  signal.throwIfAborted()
  if (!connection.current()) throw new LiveDictationError("voice_realtime_failed")
  if (typeof RTCPeerConnection !== "function") throw new LiveDictationError("voice_realtime_unsupported")
  const [track] = stream.getAudioTracks()
  if (!track || track.readyState === "ended") throw new LiveDictationError("voice_realtime_audio_unavailable")

  const peer = new RTCPeerConnection()
  const channel = peer.createDataChannel("oai-events")
  let closed = false
  let committed = ""
  let early: LiveEvent | undefined
  let partial = ""
  let completed: { resolve(text: string): void; reject(error: Error): void } | undefined
  let connected: { resolve(): void; reject(error: Error): void }
  const ready = new Promise<void>((resolve, reject) => { connected = { resolve, reject } })
  // A failed channel can close before the offer gets its answer.
  void ready.catch(() => undefined)

  function close(error = new LiveDictationError("voice_realtime_failed")) {
    if (closed) return
    closed = true
    signal.removeEventListener("abort", abort)
    connected.reject(error)
    completed?.reject(error)
    channel.close()
    peer.close()
  }
  const abort = () => close()
  signal.addEventListener("abort", abort, { once: true })

  channel.addEventListener("close", () => close())
  channel.addEventListener("error", () => close())
  peer.addEventListener("connectionstatechange", () => {
    if (peer.connectionState === "failed" || peer.connectionState === "closed") close()
  })

  function finalize(event: LiveEvent) {
    if (event.type === "conversation.item.input_audio_transcription.completed" && typeof event.transcript === "string") {
      completed?.resolve(event.transcript)
    } else {
      completed?.reject(new LiveDictationError("voice_realtime_failed"))
    }
  }

  channel.addEventListener("message", ({ data }) => {
    if (closed || typeof data !== "string") return
    let event: LiveEvent
    try { event = JSON.parse(data) } catch { return }
    switch (event?.type) {
      case "session.created":
        if (event.session?.type === "transcription") connected.resolve()
        else close()
        break
      case "error":
        close(new LiveDictationError(event.error?.code === "input_audio_buffer_commit_empty"
          ? "voice_realtime_audio_empty" : "voice_realtime_failed"))
        break
      case "conversation.item.input_audio_transcription.delta":
        if (typeof event.delta === "string") {
          partial += event.delta
          preview(partial)
        }
        break
      case "input_audio_buffer.committed":
        if (!completed || typeof event.item_id !== "string" || committed) return
        committed = event.item_id
        if (early?.item_id === committed) finalize(early)
        break
      case "conversation.item.input_audio_transcription.completed":
      case "conversation.item.input_audio_transcription.failed":
        if (!completed || typeof event.item_id !== "string") return
        if (!committed) early = event
        else if (event.item_id === committed) finalize(event)
        break
    }
  })

  try {
    // Match the voice assistant and OpenAI WebRTC examples. The transcription
    // session still produces text only, while the standard offer negotiates microphone RTP.
    const sender = peer.addTrack(track)
    const [grant, offer] = await Promise.all([
      connection.fetch("/v1/voice/live-transcription", {
        method: "POST", headers: { "Content-Type": "application/json", "Idempotency-Key": request }, body: "{}", signal,
      }),
      peer.createOffer(),
    ])
    const body = await grant.json()
    if (!grant.ok) throw new LiveDictationError(typeof body.error === "string" ? body.error : body.error?.code)
    if (typeof body.client_secret !== "string" || !body.client_secret) throw new LiveDictationError("voice_realtime_failed")
    signal.throwIfAborted()
    if (!offer.sdp || !connection.current()) throw new LiveDictationError("voice_realtime_failed")
    await peer.setLocalDescription(offer)
    const answer = await fetch("https://api.openai.com/v1/realtime/calls", {
      method: "POST", signal,
      headers: { Authorization: `Bearer ${body.client_secret}`, "Content-Type": "application/sdp" },
      body: offer.sdp,
    })
    if (!answer.ok) throw new LiveDictationError("voice_realtime_failed")
    signal.throwIfAborted()
    await peer.setRemoteDescription({ type: "answer", sdp: await answer.text() })
    const direction = peer.getTransceivers().find((entry) => entry.sender === sender)?.currentDirection
    if (direction !== "sendrecv" && direction !== "sendonly") {
      throw new LiveDictationError("voice_realtime_audio_unavailable")
    }
    await ready
    if (closed) throw new LiveDictationError("voice_realtime_failed")
  } catch (error) {
    close()
    throw error
  }

  return {
    finish() {
      if (closed || completed || channel.readyState !== "open") {
        return Promise.reject(new LiveDictationError("voice_realtime_failed"))
      }
      return new Promise<string>((resolve, reject) => {
        completed = { resolve, reject }
        try {
          channel.send(JSON.stringify({ type: "input_audio_buffer.commit" }))
        } catch {
          close()
        }
      })
    },
    close,
  }
}
