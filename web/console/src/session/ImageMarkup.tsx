import { useEffect, useRef, useState } from "react"
import type { PointerEvent as ReactPointerEvent, WheelEvent as ReactWheelEvent } from "react"
import type { Shot } from "../legacy/shots-bridge.js"
import { nextWord } from "../next-strings.js"
import {
  clampView,
  fitImage,
  imagePoint,
  markWidth,
  panView,
  zoomView,
  type MarkPoint,
  type MarkStroke,
  type MarkupLayout,
  type MarkupView,
} from "./image-markup.js"
import "./image-markup.css"

type Tool = "pen" | "move"
type Gesture = { x: number; y: number; span: number }
const FIT: MarkupView = { scale: 1, x: 0, y: 0 }

export function ImageMarkup({
  shot,
  onCancel,
  onSave,
}: {
  shot: Shot
  onCancel: () => void
  onSave: (canvas: HTMLCanvasElement) => boolean
}) {
  const stageRef = useRef<HTMLDivElement>(null)
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const imageRef = useRef<HTMLImageElement | null>(null)
  const strokes = useRef<MarkStroke[]>([])
  const active = useRef<{ pointer: number; stroke: MarkStroke } | null>(null)
  const pointers = useRef(new Map<number, MarkPoint>())
  const gesture = useRef<Gesture | null>(null)
  const suppressPen = useRef(false)
  const dialogRef = useRef<HTMLDivElement>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)
  const cancel = useRef(onCancel)
  cancel.current = onCancel
  const [ready, setReady] = useState(false)
  const [strokeCount, setStrokeCount] = useState(0)
  const [tool, setToolState] = useState<Tool>("pen")
  const toolRef = useRef<Tool>("pen")
  const [layout, setLayoutState] = useState<MarkupLayout | null>(null)
  const layoutRef = useRef<MarkupLayout | null>(null)
  const [view, setViewState] = useState<MarkupView>(FIT)
  const viewRef = useRef<MarkupView>(FIT)

  const setTool = (next: Tool) => {
    toolRef.current = next
    setToolState(next)
  }
  const setView = (next: MarkupView) => {
    viewRef.current = next
    setViewState(next)
  }
  const setLayout = (next: MarkupLayout) => {
    layoutRef.current = next
    setLayoutState(next)
  }

  const paint = () => {
    const canvas = canvasRef.current
    const image = imageRef.current
    if (!canvas || !image) return
    const context = canvas.getContext("2d")
    if (!context) return
    context.clearRect(0, 0, canvas.width, canvas.height)
    context.drawImage(image, 0, 0, canvas.width, canvas.height)
    context.strokeStyle = "#ff2d2d"
    context.fillStyle = "#ff2d2d"
    context.lineWidth = markWidth(canvas.width, canvas.height)
    context.lineCap = "round"
    context.lineJoin = "round"
    for (const stroke of strokes.current) drawStroke(context, stroke)
  }

  const measure = (reset = false) => {
    const stage = stageRef.current
    const image = imageRef.current
    if (!stage || !image?.naturalWidth || !image.naturalHeight) return
    const box = stage.getBoundingClientRect()
    const fit = fitImage(image.naturalWidth, image.naturalHeight, Math.max(1, box.width - 16), Math.max(1, box.height - 16))
    const next: MarkupLayout = {
      left: box.left,
      top: box.top,
      width: box.width,
      height: box.height,
      fitWidth: fit.width,
      fitHeight: fit.height,
      maxScale: fit.maxScale,
    }
    setLayout(next)
    setView(reset ? FIT : clampView(viewRef.current, next))
  }

  useEffect(() => {
    const image = new Image()
    imageRef.current = image
    image.onload = () => {
      const canvas = canvasRef.current
      if (!canvas) return
      canvas.width = image.naturalWidth
      canvas.height = image.naturalHeight
      strokes.current = []
      setStrokeCount(0)
      setReady(true)
      paint()
      measure(true)
    }
    image.src = shot.url
    return () => {
      image.onload = null
      imageRef.current = null
    }
  }, [shot.id, shot.url])

  useEffect(() => {
    const stage = stageRef.current
    if (!stage || typeof ResizeObserver === "undefined") return
    const observer = new ResizeObserver(() => measure())
    observer.observe(stage)
    return () => observer.disconnect()
  }, [])

  useEffect(() => {
    const before = document.activeElement as HTMLElement | null
    const key = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault()
        event.stopPropagation()
        cancel.current()
        return
      }
      if (event.key !== "Tab") return
      const buttons = Array.from(dialogRef.current?.querySelectorAll<HTMLButtonElement>("button:not(:disabled)") ?? [])
      if (!buttons.length) return
      const at = buttons.indexOf(document.activeElement as HTMLButtonElement)
      const next = event.shiftKey ? (at <= 0 ? buttons.length - 1 : at - 1) : at === buttons.length - 1 ? 0 : at + 1
      event.preventDefault()
      buttons[next].focus()
    }
    document.addEventListener("keydown", key)
    cancelRef.current?.focus()
    return () => {
      document.removeEventListener("keydown", key)
      before?.focus?.()
    }
  }, [])

  const zoomTo = (scale: number, clientX?: number, clientY?: number) => {
    const at = layoutRef.current
    if (!at) return
    setView(
      zoomView(
        viewRef.current,
        scale,
        clientX ?? at.left + at.width / 2,
        clientY ?? at.top + at.height / 2,
        at,
      ),
    )
  }

  const panBy = (dx: number, dy: number) => {
    const at = layoutRef.current
    if (at) setView(panView(viewRef.current, dx, dy, at))
  }

  const canvasPoint = (event: ReactPointerEvent<HTMLCanvasElement>): MarkPoint => {
    const canvas = event.currentTarget
    return imagePoint(event.clientX, event.clientY, canvas.getBoundingClientRect(), canvas.width, canvas.height)
  }

  const currentGesture = (): Gesture | null => {
    const held = Array.from(pointers.current.values())
    if (!held.length) return null
    if (held.length === 1) return { x: held[0].x, y: held[0].y, span: 0 }
    return {
      x: (held[0].x + held[1].x) / 2,
      y: (held[0].y + held[1].y) / 2,
      span: Math.hypot(held[1].x - held[0].x, held[1].y - held[0].y),
    }
  }

  const discardActiveStroke = () => {
    if (!active.current) return
    strokes.current.pop()
    active.current = null
    paint()
  }

  const start = (event: ReactPointerEvent<HTMLCanvasElement>) => {
    if (!ready) return
    event.preventDefault()
    event.currentTarget.setPointerCapture(event.pointerId)
    pointers.current.set(event.pointerId, { x: event.clientX, y: event.clientY })
    if (pointers.current.size >= 2) {
      discardActiveStroke()
      suppressPen.current = true
      gesture.current = currentGesture()
      return
    }
    if (toolRef.current === "pen" && !suppressPen.current) {
      const stroke = { points: [canvasPoint(event)] }
      strokes.current.push(stroke)
      active.current = { pointer: event.pointerId, stroke }
      paint()
      return
    }
    gesture.current = currentGesture()
  }

  const move = (event: ReactPointerEvent<HTMLCanvasElement>) => {
    if (!pointers.current.has(event.pointerId)) return
    event.preventDefault()
    pointers.current.set(event.pointerId, { x: event.clientX, y: event.clientY })
    const now = currentGesture()
    const before = gesture.current
    if (pointers.current.size >= 2 && now && before) {
      if (before.span > 0 && now.span > 0) zoomTo(viewRef.current.scale * (now.span / before.span), before.x, before.y)
      panBy(now.x - before.x, now.y - before.y)
      gesture.current = now
      return
    }
    if ((toolRef.current === "move" || suppressPen.current) && now && before) {
      panBy(now.x - before.x, now.y - before.y)
      gesture.current = now
      return
    }
    const drawing = active.current
    if (!drawing || drawing.pointer !== event.pointerId) return
    const events = event.nativeEvent.getCoalescedEvents?.() ?? [event.nativeEvent]
    const canvas = event.currentTarget
    const bounds = canvas.getBoundingClientRect()
    for (const sample of events) {
      drawing.stroke.points.push(imagePoint(sample.clientX, sample.clientY, bounds, canvas.width, canvas.height))
    }
    paint()
  }

  const finish = (event: ReactPointerEvent<HTMLCanvasElement>) => {
    if (active.current?.pointer === event.pointerId) {
      active.current = null
      setStrokeCount(strokes.current.length)
    }
    pointers.current.delete(event.pointerId)
    if (!pointers.current.size) {
      suppressPen.current = false
      gesture.current = null
    } else {
      gesture.current = currentGesture()
    }
  }

  const wheel = (event: ReactWheelEvent<HTMLDivElement>) => {
    if (!ready) return
    event.preventDefault()
    if (event.ctrlKey || event.metaKey) {
      zoomTo(viewRef.current.scale * Math.exp(-event.deltaY * 0.01), event.clientX, event.clientY)
    } else {
      panBy(-event.deltaX, -event.deltaY)
    }
  }

  const undo = () => {
    strokes.current.pop()
    active.current = null
    setStrokeCount(strokes.current.length)
    paint()
  }

  const canvasStyle = layout
    ? {
        width: `${layout.fitWidth}px`,
        height: `${layout.fitHeight}px`,
        transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})`,
      }
    : undefined

  return (
    <div ref={dialogRef} className="image-markup" role="dialog" aria-modal="true" aria-labelledby="image-markup-title">
      <div className="image-markup-head">
        <button ref={cancelRef} type="button" className="image-markup-cancel" onClick={onCancel}>
          {nextWord("imageMarkupCancel")}
        </button>
        <div>
          <h2 id="image-markup-title">{nextWord("imageMarkupTitle")}</h2>
          <p>{nextWord("imageMarkupHint")}</p>
        </div>
        <button
          type="button"
          className="image-markup-done"
          disabled={!ready}
          onClick={() => {
            const canvas = canvasRef.current
            if (canvas && onSave(canvas)) onCancel()
          }}
        >
          {nextWord("imageMarkupDone")}
        </button>
      </div>
      <div ref={stageRef} className="image-markup-stage" onWheel={wheel}>
        <canvas
          ref={canvasRef}
          className="image-markup-canvas"
          data-ready={ready && !!layout ? "true" : "false"}
          data-tool={tool}
          aria-label={nextWord("imageMarkupCanvas")}
          style={canvasStyle}
          onPointerDown={start}
          onPointerMove={move}
          onPointerUp={finish}
          onPointerCancel={finish}
        />
      </div>
      <div className="image-markup-tools">
        <div className="image-markup-toolset" role="group" aria-label={nextWord("imageMarkupTools")}>
          <button type="button" aria-pressed={tool === "pen"} onClick={() => setTool("pen")}>
            <span className="image-markup-pen" aria-hidden="true"></span>{nextWord("imageMarkupRedPen")}
          </button>
          <button type="button" aria-pressed={tool === "move"} onClick={() => setTool("move")}>
            <span aria-hidden="true">✥</span>{nextWord("imageMarkupMove")}
          </button>
        </div>
        <div className="image-markup-toolset image-markup-zoom" role="group" aria-label={nextWord("imageMarkupZoom")}>
          <button type="button" disabled={view.scale <= 1} aria-label={nextWord("imageMarkupZoomOut")} onClick={() => zoomTo(view.scale / 1.5)}>−</button>
          <button type="button" disabled={view.scale <= 1} onClick={() => zoomTo(1)}>{Math.round(view.scale * 100)}%</button>
          <button type="button" disabled={!layout || view.scale >= layout.maxScale} aria-label={nextWord("imageMarkupZoomIn")} onClick={() => zoomTo(view.scale * 1.5)}>+</button>
        </div>
        <button className="image-markup-undo" type="button" disabled={!strokeCount} onClick={undo}>
          <span aria-hidden="true">↶</span> {nextWord("imageMarkupUndo")}
        </button>
      </div>
    </div>
  )
}

function drawStroke(context: CanvasRenderingContext2D, stroke: MarkStroke): void {
  const first = stroke.points[0]
  if (!first) return
  if (stroke.points.length === 1) {
    context.beginPath()
    context.arc(first.x, first.y, context.lineWidth / 2, 0, Math.PI * 2)
    context.fill()
    return
  }
  context.beginPath()
  context.moveTo(first.x, first.y)
  for (const point of stroke.points.slice(1)) context.lineTo(point.x, point.y)
  context.stroke()
}
