// How loud each side of a voice copilot call is, measured in this browser for
// the orb. Nothing is recorded or sent: an analyser reads the newest samples
// when the orb asks and keeps nothing.

import { QUIET, levelFromSamples, type OrbLevels } from "./copilot-orb.js"
import { playChime } from "./chime.js"

type Side = { source: MediaStreamAudioSourceNode; analyser: AnalyserNode; samples: Uint8Array<ArrayBuffer> }

/** The ringback's loudness, its tone length and its whole period (a Taiwan and North America ring cadence). */
const RING_LEVEL = 0.08
const RING_ON_S = 1
const RING_PERIOD_MS = 3000

export class CallMeter {
  private context: AudioContext | null = null
  private input: Side | null = null
  private output: Side | null = null

  /**
   * Opens the audio context. Call it synchronously inside the tap that starts
   * the call: iOS Safari leaves a context made after an `await` suspended.
   * Returns the reason when this browser cannot measure, so the call can say so.
   */
  /**
   * Resumes a context iOS left suspended because the call started from a
   * timer (a hold that reached its length) rather than a tap. Call it inside
   * a user activation, such as the hold's lift.
   */
  wake() {
    if (this.context && this.context.state === "suspended") void this.context.resume().catch(() => undefined)
  }

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

  /**
   * Plays a ringback, as a telephone does while the far end rings: 440 and
   * 480 Hz together, one second on and two off, until the returned stop is
   * called. It plays on the context `open` made in the tap, so iOS lets it
   * sound; with no context it is silent.
   */
  ring(): () => void {
    const context = this.context
    if (!context) return () => undefined
    let gain: GainNode
    let tones: OscillatorNode[]
    try {
      gain = context.createGain()
      gain.gain.value = 0
      gain.connect(context.destination)
      tones = [440, 480].map((hz) => {
        const tone = context.createOscillator()
        tone.frequency.value = hz
        tone.connect(gain)
        tone.start()
        return tone
      })
    } catch {
      return () => undefined
    }
    const cycle = () => {
      const at = context.currentTime + 0.02
      gain.gain.setValueAtTime(0, at)
      gain.gain.linearRampToValueAtTime(RING_LEVEL, at + 0.02)
      gain.gain.setValueAtTime(RING_LEVEL, at + RING_ON_S - 0.02)
      gain.gain.linearRampToValueAtTime(0, at + RING_ON_S)
    }
    cycle()
    const timer = setInterval(cycle, RING_PERIOD_MS)
    let stopped = false
    return () => {
      if (stopped) return
      stopped = true
      clearInterval(timer)
      for (const tone of tones) {
        try { tone.stop() } catch { /* refusal-ok: a tone that has stopped stops again silently. */ }
      }
      try { gain.disconnect() } catch { /* refusal-ok: an already disconnected node is fine. */ }
    }
  }

  /**
   * A short rising two-note chime, played when the copilot has done something
   * on the machine (`actionNotice`), on the context the call's tap opened.
   */
  chime(): void {
    if (this.context) playChime(this.context)
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
