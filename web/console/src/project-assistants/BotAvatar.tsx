// Adapted from Codync 9f67eb09084db61b3cca9c84a11bc03df9de29b7.
// Copyright 2026 Po Kai Lee and the Codync contributors. Apache-2.0.
// See third_party/codync/{LICENSE,NOTICE}.
import {useEffect,useRef} from "react"
import type {ProjectAssistantAppearance} from "@clawdline/contract"
import {palette,avatarPaths} from "./avatar-paths"
import type {Words} from "./words"
import {dotGrid,gridForAvatar,drawCharacter,type Mood} from "./codync-character"
import {usePixelRatio,useReduceMotion} from "./codync-rendering"

export function BotAvatar({appearance,size=44,mood="idle"}:{appearance:ProjectAssistantAppearance;size?:number;mood?:Mood}) {
  const shape=appearance.shape,color=appearance.color
  const canvas = useRef<HTMLCanvasElement>(null)
  const dark = true // The existing Bots surface uses a dark background.
  const reduce = useReduceMotion()
  const ratio = usePixelRatio()
  const fill = color
  // A whole number of device pixels, so the bitmap shows 1:1 and drawCharacter's pixels are the screen's.
  const pixels = Math.max(1, Math.round(size * ratio))

  useEffect(() => {
    const el = canvas.current
    if (!el) return
    el.width = pixels
    el.height = pixels
    const scale = pixels / size
    const ctx = el.getContext('2d')!
    const prepared = dotGrid(size, scale)
    const points = gridForAvatar(shape, size, prepared.cells)
    const still = mood === 'idle' || mood === 'failed' || reduce
    // A moving character draws at 30 fps (its light turns slowly), and only while on screen.
    let frame = 0
    let drawn = -Infinity
    let visible = true
    const draw = (time: number) => {
      frame = 0
      if (time - drawn >= 33) {
        drawn = time
        ctx.setTransform(scale, 0, 0, scale, 0, 0)
        ctx.clearRect(0, 0, size, size)
        drawCharacter(ctx, { shape, fill, size, mood, dark, t: still ? 0 : (time / 1000) % 3600, still, points })
      }
      if (!still && visible) frame = requestAnimationFrame(draw)
    }
    draw(performance.now())
    if (still) return
    const onScreen = new IntersectionObserver(([e]) => {
      visible = e!.isIntersecting
      if (visible && !frame) frame = requestAnimationFrame(draw)
    })
    onScreen.observe(el)
    return () => {
      onScreen.disconnect()
      cancelAnimationFrame(frame)
    }
  }, [shape, fill, size, mood, dark, reduce, pixels])

  return <canvas className="pa-avatar" data-mood={mood} ref={canvas} aria-hidden style={{ width: pixels / ratio, height: pixels / ratio, color:fill, display: 'block', flex: 'none' }} />
}
export function AppearancePicker({value,onChange,w}: {value: ProjectAssistantAppearance; onChange:(value:ProjectAssistantAppearance)=>void; w:Words}) {
  return <fieldset className="pa-appearance"><legend>{w.appearance}</legend>
    <div className="pa-shapes">{Object.keys(avatarPaths).map(shape=><button type="button" key={shape} aria-label={w[`shape_${shape}` as keyof Words]} aria-pressed={value.shape===shape} onClick={()=>onChange({...value,shape:shape as ProjectAssistantAppearance["shape"]})}><BotAvatar appearance={{...value,shape:shape as ProjectAssistantAppearance["shape"]}} size={40}/></button>)}</div>
    <div className="pa-colors">{palette.map(color=><button type="button" key={color} aria-label={color} aria-pressed={value.color===color} style={{color}} onClick={()=>onChange({...value,color})}>●</button>)}</div>
  </fieldset>
}
