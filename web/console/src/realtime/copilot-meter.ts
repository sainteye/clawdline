// How loud each side of a voice copilot call is, measured in this browser for
// the orb. Nothing is recorded or sent: an analyser reads the newest samples
// when the orb asks and keeps nothing.

import { QUIET, levelFromSamples, type OrbLevels } from "./copilot-orb.js"

type Side = { source: MediaStreamAudioSourceNode; analyser: AnalyserNode; samples: Uint8Array<ArrayBuffer> }

export class CallMeter {
  private context: AudioContext | null = null
  private input: Side | null = null
  private output: Side | null = null

  /**
   * Opens the audio context. Call it synchronously inside the tap that starts
   * the call: iOS Safari leaves a context made after an `await` suspended.
   * Returns the reason when this browser cannot measure, so the call can say so.
   */
  open(): string | null {
    if (this.context) return null
    const Ctor = typeof window === "undefined" ? undefined
      : (window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext)
    if (!Ctor) return "this browser has no Web Audio"
    try {
      this.context = new Ctor()
      void this.context.resume().catch(() => undefined)
      return null
    } catch (err) {
      this.context = null
      return err instanceof Error ? err.message : String(err)
    }
  }

  /** Measures the person's microphone. */
  attachInput(stream: MediaStream): string | null {
    const side = this.side(stream)
    if (typeof side === "string") return side
    this.drop(this.input)
    this.input = side
    return null
  }

  /** Measures the copilot's voice as it arrives. */
  attachOutput(stream: MediaStream): string | null {
    const side = this.side(stream)
    if (typeof side === "string") return side
    this.drop(this.output)
    this.output = side
    return null
  }

  /** The current loudness of both sides; silence when nothing is attached. */
  read(): OrbLevels {
    if (!this.context) return QUIET
    return { input: this.level(this.input), output: this.level(this.output) }
  }

  close(): void {
    this.drop(this.input)
    this.drop(this.output)
    this.input = null
    this.output = null
    const context = this.context
    this.context = null
    if (context) void context.close().catch(() => undefined)
  }

  private side(stream: MediaStream): Side | string {
    if (!this.context) return "the audio context is not open"
    try {
      const source = this.context.createMediaStreamSource(stream)
      const analyser = this.context.createAnalyser()
      analyser.fftSize = 512
      // The analyser is not connected to the speakers: the copilot's voice
      // already plays through its <audio> element, and the person's own
      // voice must never be played back to them.
      source.connect(analyser)
      return { source, analyser, samples: new Uint8Array(analyser.fftSize) }
    } catch (err) {
      return err instanceof Error ? err.message : String(err)
    }
  }

  private level(side: Side | null): number {
    if (!side) return 0
    side.analyser.getByteTimeDomainData(side.samples)
    return levelFromSamples(side.samples)
  }

  private drop(side: Side | null): void {
    if (!side) return
    try { side.source.disconnect() } catch { /* refusal-ok: an already disconnected node has nothing to release. */ }
  }
}
