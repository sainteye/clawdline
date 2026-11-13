// The done chime: what a message that went sounds like, wherever it was sent from.

/** Its loudness, the copilot ringback's, and each note's length. */
const CHIME_LEVEL = 0.08
export const CHIME_NOTE_S = 0.12

/**
 * Two rising notes on a context the caller opened inside a tap (iOS leaves a
 * context made later suspended). Shared by the voice copilot and the
 * composer's voice send.
 */
export function playChime(context: AudioContext): void {
  try {
    const gain = context.createGain()
    gain.connect(context.destination)
    const at = context.currentTime + 0.01
    gain.gain.setValueAtTime(0, at)
    for (const [i, hz] of [880, 1320].entries()) {
      const start = at + i * CHIME_NOTE_S
      const tone = context.createOscillator()
      tone.frequency.value = hz
      tone.connect(gain)
      gain.gain.setValueAtTime(0, start)
      gain.gain.linearRampToValueAtTime(CHIME_LEVEL, start + 0.01)
      gain.gain.linearRampToValueAtTime(0, start + CHIME_NOTE_S)
      tone.start(start)
      tone.stop(start + CHIME_NOTE_S)
    }
    setTimeout(() => { try { gain.disconnect() } catch { /* refusal-ok: an already disconnected node is fine. */ } }, 1000)
  } catch {
    // refusal-ok: a chime that cannot play leaves the on-screen notice.
  }
}

type AudioContextClass = new () => AudioContext
type AudioSession = { type: string }

/** Longer than a recording's whole stop, prepare and read deadlines together. */
const PRIMED_MS = 180000

/**
 * A context opened now, in the tap, for a chime that plays later — after a
 * transcription and a delivery, possibly with its composer gone. `play`
 * sounds it once and closes it; `drop` closes it silently, and a chime never
 * played closes itself once no recording could still be waiting for it.
 */
export function primeChime(): { play(): void; drop(): void } {
  let context: AudioContext | null = null
  try {
    const scope = globalThis as { AudioContext?: AudioContextClass; webkitAudioContext?: AudioContextClass }
    const Class = scope.AudioContext ?? scope.webkitAudioContext
    context = Class ? new Class() : null
    void context?.resume().catch(() => undefined)
  } catch {
    context = null
  }
  const close = () => {
    clearTimeout(expiry)
    const was = context
    context = null
    if (was) void was.close().catch(() => undefined)
  }
  const expiry = setTimeout(close, PRIMED_MS)
  return {
    play() {
      const was = context
      if (!was) return
      clearTimeout(expiry)
      context = null
      // iOS mutes plain page audio under the ringer switch. A message
      // the person sent should still be heard, as the copilot's call is;
      // the session goes back to the browser's choice afterwards so the
      // next recording opens the microphone as before.
      const session = (globalThis.navigator as { audioSession?: AudioSession } | undefined)?.audioSession
      const before = session?.type
      try { if (session) session.type = "playback" } catch { /* refusal-ok: an older browser keeps its own session. */ }
      playChime(was)
      setTimeout(() => {
        try { if (session && before !== undefined) session.type = before } catch { /* refusal-ok: as above. */ }
        void was.close().catch(() => undefined)
      }, 1000)
    },
    drop: close,
  }
}
