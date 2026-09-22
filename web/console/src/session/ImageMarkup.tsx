import { useEffect, useRef, useState } from "react"
import type { PointerEvent as ReactPointerEvent } from "react"
import type { Shot } from "../legacy/shots-bridge.js"
import { nextWord } from "../next-strings.js"
import { imagePoint, markWidth, type MarkPoint, type MarkStroke } from "./image-markup.js"
import "./image-markup.css"

export function ImageMarkup({
  shot,
  onCancel,
  onSave,
}: {
  shot: Shot
  onCancel: () => void
  onSave: (canvas: HTMLCanvasElement) => boolean
}) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const imageRef = useRef<HTMLImageElement | null>(null)
  const strokes = useRef<MarkStroke[]>([])
  const active = useRef<{ pointer: number; stroke: MarkStroke } | null>(null)
  const dialogRef = useRef<HTMLDivElement>(null)
  const cancelRef = useRef<HTMLButtonElement>(null)
  const cancel = useRef(onCancel)
  cancel.current = onCancel
  const [ready, setReady] = useState(false)
  const [strokeCount, setStrokeCount] = useState(0)

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
    }
    image.src = shot.url
    return () => {
      image.onload = null
      imageRef.current = null
    }
  }, [shot.id, shot.url])

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

  const point = (event: ReactPointerEvent<HTMLCanvasElement>): MarkPoint => {
    const canvas = event.currentTarget
    return imagePoint(event.clientX, event.clientY, canvas.getBoundingClientRect(), canvas.width, canvas.height)
  }

  const start = (event: ReactPointerEvent<HTMLCanvasElement>) => {
    if (!ready || active.current) return
    event.preventDefault()
    event.currentTarget.setPointerCapture(event.pointerId)
    const stroke = { points: [point(event)] }
    strokes.current.push(stroke)
    active.current = { pointer: event.pointerId, stroke }
    paint()
  }

  const move = (event: ReactPointerEvent<HTMLCanvasElement>) => {
    const drawing = active.current
    if (!drawing || drawing.pointer !== event.pointerId) return
    event.preventDefault()
    const events = event.nativeEvent.getCoalescedEvents?.() ?? [event.nativeEvent]
    for (const sample of events) {
      drawing.stroke.points.push(
        imagePoint(
          sample.clientX,
          sample.clientY,
          event.currentTarget.getBoundingClientRect(),
          event.currentTarget.width,
          event.currentTarget.height,
        ),
      )
    }
    paint()
  }

  const finish = (event: ReactPointerEvent<HTMLCanvasElement>) => {
    if (active.current?.pointer !== event.pointerId) return
    active.current = null
    setStrokeCount(strokes.current.length)
  }

  const undo = () => {
    strokes.current.pop()
    active.current = null
    setStrokeCount(strokes.current.length)
    paint()
  }

  return (
    <div
      ref={dialogRef}
      className="image-markup"
      role="dialog"
      aria-modal="true"
      aria-labelledby="image-markup-title"
    >
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
      <div className="image-markup-stage">
        <canvas
          ref={canvasRef}
          className="image-markup-canvas"
          data-ready={ready ? "true" : "false"}
          aria-label={nextWord("imageMarkupCanvas")}
          onPointerDown={start}
          onPointerMove={move}
          onPointerUp={finish}
          onPointerCancel={finish}
        />
      </div>
      <div className="image-markup-tools">
        <span className="image-markup-pen" aria-hidden="true"></span>
        <span>{nextWord("imageMarkupRedPen")}</span>
        <button type="button" disabled={!strokeCount} onClick={undo}>
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
