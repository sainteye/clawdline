// Adapted from Codync 9f67eb09084db61b3cca9c84a11bc03df9de29b7.
// Copyright 2026 Po Kai Lee and the Codync contributors. Apache-2.0.
// See third_party/codync/{LICENSE,NOTICE}.
export type Mood = "idle" | "working" | "needsInput" | "failed"
/** A full-shade dot's radius, as a share of the pitch: large dots with a clear gap between them. */
export const DOT_RADIUS = 0.4

/**
 * The radius of the dot centered at (`x`, `y`) in a `size` box, for its shade (0.3…1). Below 2x the
 * snapped grid can put an edge dot less than a radius from the box edge; that dot is cut to fit.
 */
export function dotRadius(pitch: number, shade: number, x: number, y: number, size: number) {
  return Math.min(pitch * DOT_RADIUS * (0.7 + 0.3 * shade), x, size - x, y, size - y)
}

/**
 * Where a character's dots go in a `size` box drawn at `scale` device pixels per unit: `cells` per
 * side, `pitch` apart, the first centered at `first`. Every character is 9 × 9 large dots; tiny ones
 * are 7 × 7. Dots need whole device pixels, so 9 cells need 3 px each (27 px). Below 2x the pitch
 * is whole pixels too (rounded, or rounded down when full dots would overflow the box by more than
 * a pixel), so every gap is equal. A dot is about a pixel narrower than the pitch: an even width
 * (odd pitch) stays crisp centered on a pixel corner, an odd width on a pixel center. Retina keeps
 * the exact grid.
 */
export function dotGrid(size: number, scale: number) {
  // The canvas is whole device pixels: rounding drops the float error of size × scale.
  const px = Math.round(size * scale)
  const cells = size < 18 || px < 27 ? 7 : 9
  const step = size / cells
  if (scale >= 2) return { cells, pitch: step, first: step / 2 }
  const exact = step * scale
  const rounded = Math.round(exact)
  const devicePitch = Math.max(1, rounded * (cells - 1 + 2 * DOT_RADIUS) <= px + 1 ? rounded : Math.floor(exact))
  const centered = (px - devicePitch * (cells - 1)) / 2
  const first = devicePitch % 2 ? Math.round(centered) : Math.floor(centered) + 0.5
  return { cells, pitch: devicePitch / scale, first: first / scale }
}

export function characterPath(kind: string, w: number, h: number) {
  const p = new Path2D()
  let stroke = 0
  switch (kind) {
    case 'pebble':
      p.ellipse(w / 2, h / 2, w / 2, h * 0.4, 0, 0, Math.PI * 2)
      break
    case 'squircle':
      p.roundRect(w * 0.04, h * 0.04, w * 0.92, h * 0.92, w * 0.3)
      break
    case 'tablet':
      p.roundRect(w * 0.14, 0, w * 0.72, h, w * 0.22)
      break
    case 'wedge':
      p.moveTo(w * 0.5, h * 0.04)
      p.quadraticCurveTo(w * 0.9, h * 0.4, w * 0.98, h * 0.86)
      p.quadraticCurveTo(w * 0.5, h * 1.04, w * 0.02, h * 0.86)
      p.quadraticCurveTo(w * 0.1, h * 0.4, w * 0.5, h * 0.04)
      break
    case 'hex':
      for (let i = 0; i < 6; i++) {
        const a = (i * Math.PI) / 3 - Math.PI / 2
        const x = w / 2 + Math.cos(a) * w * 0.49
        const y = h / 2 + Math.sin(a) * h * 0.49
        if (i === 0) p.moveTo(x, y)
        else p.lineTo(x, y)
      }
      p.closePath()
      stroke = w * 0.08
      break
    case 'cloud':
      p.ellipse(w * 0.275, h * 0.575, w * 0.275, h * 0.275, 0, 0, Math.PI * 2)
      p.moveTo(w, h * 0.575)
      p.ellipse(w * 0.725, h * 0.575, w * 0.275, h * 0.275, 0, 0, Math.PI * 2)
      p.moveTo(w * 0.82, h * 0.4)
      p.ellipse(w * 0.5, h * 0.4, w * 0.32, h * 0.32, 0, 0, Math.PI * 2)
      p.roundRect(w * 0.1, h * 0.5, w * 0.8, h * 0.35, w * 0.15)
      break
    case 'teardrop':
      p.moveTo(w * 0.5, 0)
      p.bezierCurveTo(w * 0.62, h * 0.2, w * 0.94, h * 0.38, w * 0.94, h * 0.62)
      p.arc(w * 0.5, h * 0.62, w * 0.44, 0, Math.PI, false)
      p.bezierCurveTo(w * 0.06, h * 0.38, w * 0.38, h * 0.2, w * 0.5, 0)
      break
    default: {
      // blob
      const steps = 64
      for (let i = 0; i <= steps; i++) {
        const a = (i / steps) * 2 * Math.PI
        const rr = 0.46 + 0.035 * Math.sin(a * 3 + 0.6)
        const x = w / 2 + Math.cos(a) * w * rr
        const y = h / 2 + Math.sin(a) * h * rr
        if (i === 0) p.moveTo(x, y)
        else p.lineTo(x, y)
      }
      p.closePath()
    }
  }
  return { path: p, stroke }
}

interface Dot {
  row: number
  col: number
  x: number
  y: number
}

let probe: CanvasRenderingContext2D | null = null

/** Square-grid dot centers that fall inside the silhouette. */
export function gridForAvatar(shape: string, size: number, cells: number): Dot[] {
  probe ??= document.createElement('canvas').getContext('2d')!
  const { path, stroke } = characterPath(shape, size, size)
  const step = size / cells
  const dots: Dot[] = []
  probe.lineWidth = stroke
  probe.lineJoin = 'round'
  for (let row = 0; row < cells; row++) {
    for (let col = 0; col < cells; col++) {
      const x = (col + 0.5) * step
      const y = (row + 0.5) * step
      if (probe.isPointInPath(path, x, y, 'nonzero') || (stroke > 0 && probe.isPointInStroke(path, x, y))) dots.push({ row, col, x, y })
    }
  }
  return dots
}

export function drawCharacter(
  ctx: CanvasRenderingContext2D,
  { shape, fill, size, mood, dark, t, still, points }: { shape: string; fill: string; size: number; mood: Mood; dark: boolean; t: number; still: boolean; points?: Dot[] },
) {
  // The grid fits the context's device pixels (its scale).
  const { cells, pitch, first } = dotGrid(size, ctx.getTransform().a || 1)
  const eyeColumns = cells === 7 ? [2, 4] : [3, 6]
  const eyeRowsAll = cells === 7 ? [2, 3] : [3, 4]
  const dots = points ?? gridForAvatar(shape, size, cells)
  const [tr, tg, tb] = dark ? [250, 250, 250] : [20, 20, 20]
  const n = parseInt(fill.slice(1), 16)
  const [cr, cg, cb] = [(n >> 16) & 255, (n >> 8) & 255, n & 255]
  // Glance: whole-cell steps left / center / right, like a small display.
  const glance = mood === 'working' ? Math.round(Math.sin((t * 2 * Math.PI) / 3.2) * 1.4) : 0
  const blinking = !still && (t / 4.7) % 1 < 0.035
  const eyeRows = blinking ? eyeRowsAll.slice(-1) : eyeRowsAll
  const eyeCols = eyeColumns.map((c) => c + glance)
  const half = size / 2
  const yaw = mood === 'working' ? t * 1.4 : -0.7
  const lx = Math.sin(yaw) * 0.8
  const ly = 0.55
  const lz = Math.cos(yaw) * 0.5 + 0.6 // never fully behind
  const ll = Math.hypot(lx, ly, lz)
  const mouthCenter = Math.floor(cells / 2)
  const closedEyeRow = eyeRowsAll[eyeRowsAll.length - 1]!
  for (const d of dots) {
    // Closed eyes droop away from the nose, like a disappointed face.
    const isEye = mood === 'failed'
      ? eyeColumns.some((col) => Math.abs(d.col - col) <= 1
        && d.row === closedEyeRow - (Math.abs(d.col - mouthCenter) < Math.abs(col - mouthCenter) ? 1 : 0))
      : eyeCols.includes(d.col) && eyeRows.includes(d.row)
    if (isEye) continue
    // A downturned mouth, cut out of the same dot grid as the eyes.
    const mouthOffset = Math.abs(d.col - mouthCenter)
    if (mood === 'failed' && mouthOffset <= 1 && d.row === Math.floor(cells * 0.65) + mouthOffset) continue
    const u = (d.x - half) / half
    const v = (half - d.y) / half
    const z = Math.sqrt(Math.max(0.2, 1 - u * u - v * v))
    const nl = Math.hypot(u, v, z)
    let shade = 0.3 + 0.7 * Math.max(0, (u * lx + v * ly + z * lz) / (nl * ll))
    if (mood === 'needsInput') {
      const ripple = 0.5 + 0.5 * Math.sin(Math.hypot(u, v) * 9 - t * 5)
      shade *= 0.6 + 0.4 * ripple
    }
    const x = first + d.col * pitch
    const y = first + d.row * pitch
    const ink = 0.4 + 0.4 * shade
    ctx.beginPath()
    ctx.arc(x, y, dotRadius(pitch, shade, x, y, size), 0, Math.PI * 2)
    ctx.fillStyle = `rgba(${tr},${tg},${tb},${ink})`
    ctx.fill()
    if (shade > 0.6) {
      ctx.fillStyle = `rgba(${cr},${cg},${cb},${(shade - 0.6) / 0.4})`
      ctx.fill()
    }
  }
}

