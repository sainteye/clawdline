import type { DictationConnection } from "./live-dictation.js"

const RATE = 16000
const WINDOW_BYTES = 3 * RATE * 2
const READ_MS = 5000

/** A transient preview owns neither the media tracks nor the final draft. */
export interface WhisperPreview {
  close(): void
}

export function startWhisperPreview(
  context: AudioContext,
  stream: MediaStream,
  connection: DictationConnection,
  requestKey: () => string,
  encode: (audio: Uint8Array) => string,
  preview: (segment: string) => void,
  unavailable: () => void,
): WhisperPreview {
  let node: AudioWorkletNode | undefined
  let request: AbortController | undefined
  let pending: ArrayBuffer | undefined
  let closed = false

  function close(): void {
    if (closed) return
    closed = true
    pending = undefined
    request?.abort()
    if (node) {
      node.port.onmessage = null
      node.disconnect()
    }
    void context.close().catch(() => undefined)
  }

  async function read(audio: ArrayBuffer): Promise<void> {
    if (closed) return
    if (!connection.current()) { close(); return }
    const abort = new AbortController()
    request = abort
    const expiry = setTimeout(() => abort.abort(), READ_MS)
    try {
      const response = await connection.fetch("/v1/voice/preview", {
        method: "POST",
        headers: { "Content-Type": "application/json", "Idempotency-Key": requestKey() },
        body: JSON.stringify({ rate: RATE, audio: encode(new Uint8Array(audio)) }),
        signal: abort.signal,
      })
      if (closed || abort.signal.aborted) return
      if (!response.ok) {
        if (response.status === 429 || response.status === 408) return
        const refusal = await response.json().catch(() => null) as { error?: string } | null
        if (closed || abort.signal.aborted) return
        // A temporary Cloud or inference failure drops this window. The
        // full recording and the next preview window are still independent.
        if (response.status >= 500 && response.status !== 501 && refusal?.error !== "no_whisper") return
        unavailable()
        close()
        return
      }
      const result = await response.json() as { text?: unknown }
      if (!closed && !abort.signal.aborted && connection.current() && typeof result.text === "string" && result.text.trim()) {
        preview(result.text.trim())
      }
    } catch {
      // The network request can be abandoned or interrupted between windows.
    } finally {
      clearTimeout(expiry)
      request = undefined
      const next = pending
      pending = undefined
      if (next && !closed) void read(next)
    }
  }

  void (async () => {
    try {
      if (!context.audioWorklet || context.sampleRate < RATE) { unavailable(); close(); return }
      // Keep this as a same-origin script in the hosted build as well as in dev.
      await context.audioWorklet.addModule(new URL("./whisper-preview.worklet.js?no-inline", import.meta.url))
      if (closed) return
      node = new AudioWorkletNode(context, "whisper-preview", { channelCount: 1, channelCountMode: "explicit" })
      node.onprocessorerror = () => {
        if (closed) return
        unavailable()
        close()
      }
      node.port.onmessage = (event: MessageEvent<unknown>) => {
        const audio = event.data
        if (closed || !(audio instanceof ArrayBuffer) || audio.byteLength !== WINDOW_BYTES) return
        if (request) pending = audio
        else void read(audio)
      }
      // The worklet's output is always silence; connect it so the graph
      // continues to pull PCM without playing the microphone to the room.
      context.createMediaStreamSource(stream).connect(node).connect(context.destination)
      // A microphone route change on WebKit can interrupt the context that the
      // gesture opened. Resume the same graph after attaching the live stream.
      if (context.state !== "running") await context.resume()
    } catch {
      if (!closed) unavailable()
      close()
    }
  })()

  return { close }
}
