// How loud each side of a voice copilot call is, measured in this browser for
// the orb. Nothing is recorded or sent: an analyser reads the newest samples
// when the orb asks and keeps nothing.

import { QUIET, levelFromSamples, type OrbLevels } from "./copilot-orb.js"
import { borrowAudioSession, CHIME_NOTE_S, playChime } from "./chime.js"

type Side = { source: MediaStreamAudioSourceNode; analyser: AnalyserNode; samples: Uint8Array<ArrayBuffer> }

/** The ringback's loudness, its tone length and its whole period (a Taiwan and North America ring cadence). */
const RING_LEVEL = 0.08
const RING_ON_S = 1
const RING_PERIOD_MS = 3000

/** A call owns this media element and its remote tracks, even while final usage closes. */
export function releaseCallPlayback(audio: HTMLMediaElement): void {
  const stream = audio.srcObject as MediaStream | null
  for (const release of [
    () => audio.pause(),
    () => { audio.autoplay = false },
    () => { audio.srcObject = null },
    () => audio.removeAttribute('src'),
    () => audio.load(),
  ]) {
    try { release() } catch { /* refusal-ok: each remaining release still runs. */ }
  }
  for (const track of stream?.getTracks?.() ?? []) {
    try { track.stop() } catch { /* refusal-ok: an ended remote track is already released. */ }
  }
}

/**
 * A ringback for a call that measures nothing, such as a Bot's browser call:
 * its own context, opened now, inside the tap, under the call's duplex audio
 * session so the microphone route is kept. Stopping it silences the ring and
 * closes that context without a hangup cue.
 */
export function callRingback(observe?: (state: AudioContextState | 'unavailable') => void): () => void {
  const meter = new CallMeter()
  if (meter.open() !== null) { observe?.('unavailable'); return () => {} }
  const stop = meter.ring(observe)
  return () => { stop(); meter.close() }
}

export class CallMeter {
  private context: AudioContext | null = null
  private releaseAudio?: () => void
  private finishClosing?: () => void
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
    if (this.context && this.context.state !== "running" && this.context.state !== "closed") void this.context.resume().catch(() => undefined)
  }

  open(): string | null {
    if (this.context) return null
    const Ctor = typeof window === "undefined" ? undefined
      : (window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext)
    if (!Ctor) return "this browser has no Web Audio"
    const release = borrowAudioSession("play-and-record")
    try {
      this.context = new Ctor()
      this.releaseAudio = release
      this.finishClosing?.()
      void this.context.resume().catch(() => undefined)
      return null
    } catch (err) {
      this.context = null
      this.releaseAudio = undefined
      release()
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
  ring(observe: (state: AudioContextState | 'unavailable') => void = () => {}): () => void {
    const context = this.context
    if (!context) { observe('unavailable'); return () => {} }
    let timer: ReturnType<typeof setInterval> | undefined
    let gain: GainNode | undefined
    const tones: OscillatorNode[] = []
    let stopped = false
    const stop = () => {
      if (stopped) return
      stopped = true
      clearInterval(timer)
      context.removeEventListener('statechange', sync)
      for (const tone of tones) {
        try { tone.stop(); tone.disconnect() } catch { /* refusal-ok: a stopped tone is already silent. */ }
      }
      gain?.disconnect()
    }
    const cycle = () => {
      if (!gain || context.state !== 'running') return
      const at = context.currentTime + 0.02
      gain.gain.cancelScheduledValues(at)
      gain.gain.setValueAtTime(0, at)
      gain.gain.linearRampToValueAtTime(RING_LEVEL, at + 0.02)
      gain.gain.setValueAtTime(RING_LEVEL, at + RING_ON_S - 0.02)
      gain.gain.linearRampToValueAtTime(0, at + RING_ON_S)
    }
    let prior: AudioContextState | undefined
    const sync = () => {
      if (stopped || context.state === prior) return
      prior = context.state
      clearInterval(timer)
      try {
        if (prior === 'running') {
          if (!gain) {
            gain = context.createGain()
            gain.gain.value = 0
            gain.connect(context.destination)
            for (const hz of [440, 480]) {
              const tone = context.createOscillator()
              tones.push(tone)
              tone.frequency.value = hz
              tone.connect(gain)
              tone.start()
            }
          }
          cycle()
          timer = setInterval(cycle, RING_PERIOD_MS)
        }
        observe(prior)
      } catch {
        stop()
        observe('unavailable')
      }
    }
    context.addEventListener('statechange', sync)
    sync()
    return stop
  }

  /** Distinct local cues for connection readiness and entering a Session. */
  connected(): void { this.notes([480, 660], RING_LEVEL / 4) }
  sessionChanged(): void { this.notes([660, 880, 660], RING_LEVEL) }

  private withRunningContext(play: (context: AudioContext) => void): void {
    const context = this.context
    if (!context || context.state === 'closed') return
    if (context.state === 'running') {
      play(context)
      return
    }
    // The call's original, tap-activated context can be interrupted by the
    // browser's mic or output routing. Never play on a replacement call.
    void context.resume().then(() => {
      if (this.context === context && context.state === 'running') play(context)
    }).catch(() => undefined)
  }

  private notes(frequencies: readonly number[], volume: number): void {
    this.withRunningContext(context => {
      try { this.playNotes(context, frequencies, volume) }
      catch { /* refusal-ok: the visible state remains when local audio is unavailable. */ }
    })
  }

  private playNotes(context: AudioContext, frequencies: readonly number[], volume: number, done?: () => void): void {
    const onset = context.currentTime + 0.01
    for (const [index, hz] of frequencies.entries()) {
      const gain = context.createGain(), tone = context.createOscillator()
      const at = onset + index * CHIME_NOTE_S
      gain.connect(context.destination)
      tone.connect(gain)
      tone.frequency.value = hz
      gain.gain.setValueAtTime(0, at)
      gain.gain.linearRampToValueAtTime(volume, at + 0.01)
      gain.gain.linearRampToValueAtTime(0, at + CHIME_NOTE_S)
      tone.onended = () => {
        tone.disconnect()
        gain.disconnect()
        if (index === frequencies.length - 1) done?.()
      }
      tone.start(at)
      tone.stop(at + CHIME_NOTE_S)
    }
  }

  /** One start note: background work is lower and warmer than an active tool. No loop. */
  pending(source: 'tool' | 'background' = 'tool', active: () => boolean = () => true): void {
    this.withRunningContext(context => {
      if (!active()) return
      try {
        const gain = context.createGain(), tone = context.createOscillator()
        const at = context.currentTime
        gain.connect(context.destination)
        tone.connect(gain)
        tone.frequency.value = source === 'background' ? 220 : 330
        tone.type = source === 'background' ? 'triangle' : 'sine'
        gain.gain.setValueAtTime(0, at)
        gain.gain.linearRampToValueAtTime(RING_LEVEL / 3, at + 0.01)
        gain.gain.linearRampToValueAtTime(0, at + CHIME_NOTE_S)
        tone.onended = () => { tone.disconnect(); gain.disconnect() }
        tone.start(at)
        tone.stop(at + CHIME_NOTE_S)
      } catch { /* refusal-ok: unavailable sound leaves the visible processing state. */ }
    })
  }

  /**
   * A short rising two-note chime, played when the copilot has done something
   * on the machine (`actionNotice`), on the context the call's tap opened.
   */
  chime(): void {
    this.withRunningContext(playChime)
  }

  close(ended=false): void {
    this.drop(this.input)
    this.drop(this.output)
    this.input = null
    this.output = null
    const context = this.context
    const releaseAudio = this.releaseAudio
    this.context = null
    this.releaseAudio = undefined
    if (!context) { releaseAudio?.(); return }
    let closed = false
    let recovery: ReturnType<typeof setTimeout> | undefined
    const interrupted = () => { if (context.state !== 'running') close() }
    const close = () => {
      if (closed) return
      closed = true
      clearTimeout(recovery)
      context.removeEventListener('statechange', interrupted)
      if (this.finishClosing === close) this.finishClosing = undefined
      releaseAudio?.()
      void context.close().catch(() => undefined)
    }
    if (!ended || context.state === 'closed') { close(); return }

    // The microphone and remote tracks are already detached. Give the old
    // context only the cue's length to recover. A replacement cancels it.
    const frequencies = [660, 480]
    this.finishClosing = close
    const play = () => {
      if (closed || context.state !== 'running') return
      clearTimeout(recovery)
      context.addEventListener('statechange', interrupted)
      try { this.playNotes(context, frequencies, RING_LEVEL / 4, close); interrupted() }
      catch { close() }
    }
    if (context.state === 'running') play()
    else {
      recovery = setTimeout(close, frequencies.length * CHIME_NOTE_S * 1000)
      void context.resume().then(play, close)
    }
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
