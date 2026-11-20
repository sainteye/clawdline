import type { ProjectAssistantShape } from "@clawdline/contract"
import { avatarPaths, avatarLights, palette } from "../avatar-paths"
import type { TowerMood } from "./tower-state"

const paths = new Map<string, Path2D>()
const pathOf = (shape: string) => {
  let p = paths.get(shape)
  if (!p) { p = new Path2D(avatarPaths[shape as ProjectAssistantShape] ?? avatarPaths.blob); paths.set(shape, p) }
  return p
}

function hexRgb(hex: string) {
  const n = parseInt(hex.slice(1), 16)
  return `${n >> 16},${n >> 8 & 255},${n & 255}`
}

function lighten(hex: string, f: number) {
  const n = parseInt(hex.slice(1), 16)
  const c = [n >> 16, n >> 8 & 255, n & 255].map(v => Math.round(v + (255 - v) * f))
  return `rgb(${c[0]},${c[1]},${c[2]})`
}

export interface DrawBot {
  shape: string
  color: string
  mood: TowerMood
  seed: number
  unread?: boolean
  /** The lead Bot wears a short cape and a star badge. */
  lead?: boolean
  /** Neon outline (the default) or a body filled with its colour's gradient. */
  style?: TowerStyle
}

export type TowerStyle = "neon" | "gradient"

const Gold = "#e3bc6b"

/** One neon BR-06 Bot standing on (x, y) in canvas pixels; size is its height. */
export function drawBot(g: CanvasRenderingContext2D, b: DrawBot, x: number, y: number, size: number, moving: boolean, now: number, still: boolean, dpr: number, alpha = 1) {
  const color = (palette as readonly string[]).includes(b.color) ? b.color : palette[0]
  const glow = avatarLights[color as typeof palette[number]]?.[1] ?? color
  let lift = 0, sx = 1, sy = 1, rot = 0
  if (!still) {
    if (moving) { const p = Math.abs(Math.sin(now / 120)); lift = p * size * .2; sy = 1 - .1 * (1 - p); sx = 1 + .08 * (1 - p) }
    else if (b.mood === "work") { rot = Math.sin(now / 170) * .06; lift = Math.abs(Math.sin(now / 170)) * size * .04 }
    else if (b.mood === "wait") lift = Math.abs(Math.sin(now / 260)) * size * .14
    else if (b.mood === "recent") lift = Math.abs(Math.sin(now / 900)) * size * .05
  }
  const sleeping = b.mood === "sleep", dim = sleeping ? .7 : b.mood === "idle" ? .9 : 1
  // A pool of the Bot's own light on the floor, brighter while it works, so it
  // stands in the scene rather than on top of the picture. Asleep, none.
  if (!sleeping) {
    const r = size * (b.mood === "work" ? .95 : .75), pool = g.createRadialGradient(x, y, 0, x, y, r)
    const rgb = hexRgb(color), a = (b.mood === "work" ? .42 : .26) * alpha
    pool.addColorStop(0, `rgba(${rgb},${a})`); pool.addColorStop(1, `rgba(${rgb},0)`)
    g.save(); g.globalCompositeOperation = "lighter"; g.fillStyle = pool
    g.beginPath(); g.ellipse(x, y, r, r * .32, 0, 0, Math.PI * 2); g.fill(); g.restore()
  }
  g.save()
  g.globalAlpha = alpha * dim
  g.fillStyle = "rgba(0,0,0,.5)"
  g.beginPath(); g.ellipse(x, y, size * .38 * (1 - lift / size * .8), size * .1, 0, 0, Math.PI * 2); g.fill()
  if (b.mood === "wait" || b.mood === "error") {
    const p = still ? .6 : (Math.sin(now / 200) + 1) / 2, ring = b.mood === "wait" ? "227,149,103" : "214,92,92"
    g.strokeStyle = `rgba(${ring},${.3 + .45 * p})`; g.lineWidth = Math.max(1.5, size * .05)
    g.beginPath(); g.ellipse(x, y, size * .55 + p * size * .08, size * .16 + p * size * .03, 0, 0, Math.PI * 2); g.stroke()
  }
  g.translate(x, y - lift); g.rotate(rot); g.scale(sx, sy)
  if (sleeping) g.rotate(-.12)
  if (b.mood === "error") g.scale(1, .86)
  const k = size / 110
  g.translate(-size / 2, -size); g.scale(k, k); g.translate(5, 5)
  const p = pathOf(b.shape)
  // The lead's cape hangs behind the body, in the body's own 110-unit space,
  // and sways a little unless motion is reduced.
  if (b.lead) {
    // Walking, the cape streams behind; working, it flutters.
    const sway = still ? 0 : moving ? 8 + Math.sin(now / 140) * 3 : Math.sin(now / (b.mood === "work" ? 220 : 650) + b.seed) * 4
    g.save()
    g.beginPath(); g.moveTo(24, 46); g.quadraticCurveTo(4 - sway, 78, 2 - sway, 106); g.lineTo(98 - sway, 106); g.quadraticCurveTo(96 - sway, 78, 76, 46); g.closePath()
    g.fillStyle = `rgba(${hexRgb(color)},.32)`; g.fill()
    g.lineJoin = "round"; g.shadowColor = glow; g.shadowBlur = 10 * dpr; g.strokeStyle = color; g.lineWidth = 4; g.stroke()
    g.restore()
  }
  if (b.style === "gradient") {
    // The same outline, filled from a light top to the full colour, with dark
    // eyes: the picker's gradient avatar rather than a lit tube.
    const fill = g.createLinearGradient(0, 5, 0, 105)
    fill.addColorStop(0, lighten(color, .5)); fill.addColorStop(1, color)
    g.fillStyle = fill; g.shadowColor = glow; g.shadowBlur = 8 * dpr; g.fill(p)
    g.lineJoin = "round"; g.shadowBlur = 0; g.strokeStyle = lighten(color, .7); g.lineWidth = 3; g.stroke(p)
    g.lineCap = "round"; g.lineWidth = 6.5; g.strokeStyle = "#120a26"
  } else {
    g.fillStyle = "rgba(9,6,22,.86)"; g.fill(p)
    g.lineJoin = "round"; g.shadowColor = glow; g.shadowBlur = 16 * dpr; g.strokeStyle = color; g.lineWidth = 6; g.stroke(p)
    g.shadowBlur = 5 * dpr; g.strokeStyle = lighten(color, .55); g.lineWidth = 2.2; g.stroke(p)
    g.lineCap = "round"; g.lineWidth = 6.5; g.shadowColor = color; g.shadowBlur = 8 * dpr; g.strokeStyle = color
  }
  g.beginPath()
  if (sleeping) { g.moveTo(30, 55); g.lineTo(42, 55); g.moveTo(57, 55); g.lineTo(69, 55) }
  else {
    const blink = !still && (Math.floor(now / 120) + b.seed) % 42 === 0
    const top = b.mood === "wait" ? 37 : 40, len = blink ? 1 : 15
    g.moveTo(36, top + (15 - len) / 2); g.lineTo(36, top + (15 + len) / 2)
    g.moveTo(63, top + (15 - len) / 2); g.lineTo(63, top + (15 + len) / 2)
  }
  g.stroke()
  if (b.lead) {
    // A gold star on the chest, lit so it reads at the smallest size.
    g.beginPath()
    // Every few seconds the star swells and flashes four rays.
    const t = still ? 1 : ((now + b.seed * 37) % 3800) / 3800, flash = t < .12 ? Math.sin(t / .12 * Math.PI) : 0, grow = 1 + flash * .35
    for (let i = 0; i < 10; i++) { const a = -Math.PI / 2 + i * Math.PI / 5, r = (i % 2 ? 6.5 : 14) * grow; g.lineTo(70 + Math.cos(a) * r, 78 + Math.sin(a) * r) }
    g.closePath(); g.shadowColor = Gold; g.shadowBlur = (10 + flash * 14) * dpr; g.fillStyle = Gold; g.fill()
    g.shadowBlur = 0; g.lineWidth = 2; g.strokeStyle = "#fff3d6"; g.stroke()
    if (flash > 0) {
      g.globalAlpha *= flash; g.lineWidth = 2.5; g.beginPath()
      for (let i = 0; i < 4; i++) { const a = i * Math.PI / 2 + Math.PI / 4; g.moveTo(70 + Math.cos(a) * 18, 78 + Math.sin(a) * 18); g.lineTo(70 + Math.cos(a) * 30, 78 + Math.sin(a) * 30) }
      g.stroke()
    }
  }
  g.restore()
  // Working: a few pixels drift up from the keyboard and fade, a pure function of time.
  if (b.mood === "work" && !still) {
    const px = Math.max(2, size * .045)
    g.save(); g.fillStyle = lighten(color, .35)
    for (let i = 0; i < 4; i++) {
      const phase = (now / 1100 + i / 4 + (b.seed % 7) / 7) % 1
      const dx = (((b.seed >>> (i * 3)) % 9) / 8 - .5) * size * .7
      g.globalAlpha = alpha * (1 - phase) * .9
      g.fillRect(Math.round(x + dx), Math.round(y - lift - size * (.95 + phase * .55)), px, px)
    }
    g.restore()
  }
  if (b.mood === "wait") {
    const s = Math.max(10, size * .3)
    g.save(); g.globalAlpha = alpha; g.fillStyle = "#e39567"; g.fillRect(x + size * .25, y - lift - size - s * .9, s, s)
    g.fillStyle = "#120a26"; g.font = `700 ${Math.round(s * .8)}px -apple-system, system-ui, sans-serif`; g.textAlign = "center"; g.textBaseline = "middle"
    g.fillText("!", x + size * .25 + s / 2, y - lift - size - s * .4); g.restore()
  }
  if (b.unread && b.mood !== "wait") {
    const r = Math.max(3, size * .07)
    g.save(); g.globalAlpha = alpha; g.fillStyle = "#fafafa"; g.strokeStyle = "#080808"; g.lineWidth = Math.max(1, r * .45)
    g.beginPath(); g.arc(x + size * .36, y - lift - size * .92, r, 0, Math.PI * 2); g.fill(); g.stroke(); g.restore()
  }
  if (sleeping) {
    const z = still ? 1 : Math.floor((now + b.seed) / 550) % 3
    g.save(); g.globalAlpha = alpha * .85; g.fillStyle = lighten(color, .5)
    g.font = `600 ${Math.round(Math.max(8, size * .22))}px -apple-system, system-ui, sans-serif`
    g.fillText("z".repeat(z + 1), x + size * .35, y - size * 1.05); g.restore()
  }
}
