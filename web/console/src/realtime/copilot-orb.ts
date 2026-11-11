// How the voice copilot's cloud orb moves: what each call state looks like,
// how loud each side is, and how often it may be drawn. The drawing itself is
// in CopilotOrb.tsx; this file holds the numbers so they can be tested.

export type OrbMode = "idle" | "connecting" | "listening" | "thinking" | "speaking"

/** How loud the person (input) and the copilot (output) are, each 0..1. */
export interface OrbLevels { input: number; output: number }

/** What the shader is steered toward in a given state. */
export interface OrbLook {
  /** How fast the clouds turn, in radians of phase per second. */
  speed: number
  /** The sphere's size relative to its resting size. */
  swell: number
  /** How bright the halo around the sphere is, 0..1. */
  glow: number
  /** The sphere's overall brightness multiplier. */
  bright: number
  /** How tightly the clouds gather into a whirl (thinking). */
  swirl: number
}

/** The most frames a second the orb is drawn: a cloud needs no more, and a phone's battery notices. */
export const ORB_MAX_FPS = 30
/** The most device pixels per CSS pixel the orb's canvas is drawn at. */
export const ORB_MAX_PIXEL_RATIO = 2

export const QUIET: OrbLevels = { input: 0, output: 0 }

/**
 * The look a state is steered toward. A still orb when `still` (the system
 * asks for reduced motion): nothing turns or swells, and only the brightness
 * says someone is speaking.
 */
export function orbLook(mode: OrbMode, levels: OrbLevels, still = false): OrbLook {
  const input = clamp01(levels.input)
  const output = clamp01(levels.output)
  let look: OrbLook
  switch (mode) {
    case "idle":
      look = { speed: 0, swell: 0.94, glow: 0.12, bright: 0.72, swirl: 0 }
      break
    case "connecting":
      look = { speed: 0.18, swell: 0.97, glow: 0.3, bright: 0.86, swirl: 0 }
      break
    case "listening":
      look = { speed: 0.28 + input * 1.1, swell: 1 + input * 0.08, glow: 0.38 + input * 0.5, bright: 1 + input * 0.08, swirl: 0 }
      break
    case "thinking":
      look = { speed: 0.4, swell: 0.97, glow: 0.34, bright: 0.96, swirl: 2.4 }
      break
    case "speaking":
      look = { speed: 0.7 + output * 2, swell: 1 + output * 0.06, glow: 0.6 + output * 0.4, bright: 1.04 + output * 0.1, swirl: 0 }
      break
  }
  if (!still) return look
  const loud = Math.max(input, output)
  return { speed: 0, swell: 1, glow: look.glow, bright: (mode === "idle" ? 0.72 : 1) + loud * 0.12, swirl: 0 }
}

/**
 * A loudness from an analyser's time-domain bytes (128 is silence). The
 * square root makes ordinary speech, whose RMS is 0.03-0.2, fill most of 0..1.
 */
export function levelFromSamples(samples: ArrayLike<number>): number {
  if (samples.length === 0) return 0
  let sum = 0
  for (let i = 0; i < samples.length; i++) {
    const v = (samples[i] - 128) / 128
    sum += v * v
  }
  const rms = Math.sqrt(sum / samples.length)
  return clamp01(Math.sqrt(Math.max(rms - 0.008, 0)) * 2.2)
}

/** A level that rises quickly with a voice and falls slowly after it. */
export function smoothLevel(current: number, target: number, dtSeconds: number): number {
  const rate = target > current ? 18 : 4
  return approach(current, target, dtSeconds, rate)
}

/** Every number of the look moved toward the target at `rate` per second. */
export function easeLook(current: OrbLook, target: OrbLook, dtSeconds: number, rate = 5): OrbLook {
  return {
    speed: approach(current.speed, target.speed, dtSeconds, rate),
    swell: approach(current.swell, target.swell, dtSeconds, rate),
    glow: approach(current.glow, target.glow, dtSeconds, rate),
    bright: approach(current.bright, target.bright, dtSeconds, rate),
    swirl: approach(current.swirl, target.swirl, dtSeconds, rate),
  }
}

/** Whether nothing would visibly change by drawing again. */
export function settled(current: OrbLook, target: OrbLook): boolean {
  return target.speed === 0 && current.speed < 0.001 &&
    Math.abs(current.swell - target.swell) < 0.001 &&
    Math.abs(current.glow - target.glow) < 0.002 &&
    Math.abs(current.bright - target.bright) < 0.002 &&
    Math.abs(current.swirl - target.swirl) < 0.002
}

/** Whether enough time has passed since the last frame to draw another. */
export function frameDue(lastMs: number, nowMs: number): boolean {
  // A little slack so a 60 Hz display draws every other frame, not every third.
  return nowMs - lastMs >= 1000 / ORB_MAX_FPS - 2
}

/** The canvas's pixel ratio: the device's, but never above ORB_MAX_PIXEL_RATIO. */
export function orbPixelRatio(device: number): number {
  if (!Number.isFinite(device) || device <= 0) return 1
  return Math.min(device, ORB_MAX_PIXEL_RATIO)
}

function approach(current: number, target: number, dt: number, rate: number): number {
  const step = 1 - Math.exp(-rate * Math.max(dt, 0))
  return current + (target - current) * step
}

function clamp01(v: number): number {
  return Number.isFinite(v) ? Math.min(Math.max(v, 0), 1) : 0
}
