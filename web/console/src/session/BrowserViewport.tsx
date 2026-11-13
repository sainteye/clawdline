import { useEffect, useLayoutEffect, useRef, useState, type PointerEvent, type ReactNode } from "react"
import { createImageZoom } from "../legacy/js/view/transcript-images.js"
import { nextWord } from "../next-strings.js"
import type { InputReceipt } from "./browser-keyboard.js"

type Point = { x: number; y: number }
type Gesture = Point & { span: number }
type Editable = { x: number; y: number; width: number; height: number; kind: string }
type Shot = { data: string; width: number; height: number; url: string; title: string; editables?: Editable[] }

/** The existing picture-preview zoom model also keeps the live tab under the fingers.
 * Gestures change only this view. Only a released, unmoved single pointer may
 * become a browser click, and its coordinates come from the transformed image.
 */
export function BrowserViewport({ shot, canClick, person, onTap, onKeyboard, onFocusKeyboard, onCloseKeyboard, layoutControl }: {
  shot: Shot; canClick: boolean; person: boolean; onTap: (x: number, y: number) => Promise<InputReceipt>;
  onKeyboard: (kind: string, ready: Promise<boolean>) => void; onFocusKeyboard: () => void; onCloseKeyboard: () => void; layoutControl: ReactNode;
}) {
  const frame = useRef<HTMLDivElement>(null)
  const image = useRef<HTMLImageElement>(null)
  const zoom = useRef(createImageZoom())
  const pointers = useRef(new Map<number, Point>())
  const gesture = useRef<Gesture | null>(null)
  const press = useRef<Point | null>(null)
  const pressRemote = useRef<Point | null>(null)
  const moved = useRef(false)
  // iOS Safari raises its keyboard for a programmatic focus inside touchend,
  // not inside pointerup. A phone tap on a pictured field reached the page,
  // but the footer editor stayed unfocused until the person tapped it.
  const focusOnTouchEnd = useRef(false)
  // After that, iOS still sends compatibility mouse events and a click, which
  // focus this tabbable picture: the footer editor flashed and lost focus.
  // Cancel them in touchend, and hand a focus that arrives right after a
  // field tap back to the editor.
  const fieldTapAt = useRef(0)
  const mode = useRef<"fit" | "native" | "custom">(window.matchMedia("(pointer: coarse) and (max-width: 899px)").matches ? "native" : "fit")
  const fitted = useRef(0)
  const [size, setSize] = useState({ width: 0, height: 0 })
  const [view, setView] = useState(zoom.current.state())
  const draw = () => setView({ ...zoom.current.state() })

  useLayoutEffect(() => {
    const element = frame.current
    if (!element) return
    const measure = () => {
      const box = element.getBoundingClientRect()
      if (!box.width || !box.height || !shot.width || !shot.height) return
      const scale = Math.min(1, box.width / shot.width, box.height / shot.height)
      const before = fitted.current * zoom.current.state().scale
      // Native focus can resize the viewport before pointerup. Keep that
      // gesture; its original remote coordinate is retained below.
      zoom.current.layout({ frame: box, content: { width: shot.width * scale, height: shot.height * scale }, natural: { width: shot.width, height: shot.height } })
      fitted.current = scale
      setSize({ width: shot.width * scale, height: shot.height * scale })
      zoom.current.scaleTo(mode.current === "fit" ? 1 : (before || 1) / scale, undefined, undefined)
      // Start a phone at the first readable row, not halfway across the page.
      if (!before && mode.current === "native") zoom.current.panBy(shot.width, shot.height)
      draw()
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(element)
    return () => observer.disconnect()
  }, [shot.width, shot.height])

  useEffect(() => {
    // Handback, a refusal, or a newly busy command cancels an unfinished tap.
    pointers.current.clear(); gesture.current = null; press.current = null
  }, [canClick, shot.width, shot.height])

  const scaleTo = (scale: number, x?: number, y?: number) => {
    const box = frame.current?.getBoundingClientRect()
    if (!box) return
    mode.current = "custom"
    zoom.current.scaleTo(scale, x ?? box.left + box.width / 2, y ?? box.top + box.height / 2)
    draw()
  }
  const fit = () => { mode.current = "fit"; zoom.current.scaleTo(1, undefined, undefined); draw() }
  const native = () => { scaleTo(1 / fitted.current); mode.current = "native" }
  const currentGesture = (): Gesture | null => {
    const held = [...pointers.current.values()]
    if (!held.length) return null
    if (held.length === 1) return { ...held[0], span: 0 }
    return { x: (held[0].x + held[1].x) / 2, y: (held[0].y + held[1].y) / 2, span: Math.hypot(held[1].x - held[0].x, held[1].y - held[0].y) }
  }
  const start = (event: PointerEvent<HTMLElement>) => {
    if (event.button !== 0) return
    event.preventDefault()
    event.currentTarget.setPointerCapture(event.pointerId)
    const point = { x: event.clientX, y: event.clientY }
    if (pointers.current.size === 0) {
      press.current = point; moved.current = false
      const box = image.current?.getBoundingClientRect()
      pressRemote.current = box && point.x >= box.left && point.x < box.right && point.y >= box.top && point.y < box.bottom
        ? { x: Math.floor((point.x - box.left) / box.width * shot.width), y: Math.floor((point.y - box.top) / box.height * shot.height) } : null
    }
    pointers.current.set(event.pointerId, point)
    if (pointers.current.size > 1) moved.current = true
    gesture.current = currentGesture()
  }
  const move = (event: PointerEvent<HTMLElement>) => {
    if (!pointers.current.has(event.pointerId)) return
    event.preventDefault()
    pointers.current.set(event.pointerId, { x: event.clientX, y: event.clientY })
    const now = currentGesture(), before = gesture.current
    if (!now || !before) return
    // The same generous tap slop as the picture preview, including drags at
    // an edge that cannot move the image any further.
    if (press.current && Math.hypot(now.x - press.current.x, now.y - press.current.y) > 12) moved.current = true
    if (pointers.current.size > 1 && before.span > 0 && now.span > 0) {
      mode.current = "custom"
      zoom.current.scaleBy(now.span / before.span, before.x, before.y)
    }
    if (moved.current) {
      zoom.current.panBy(now.x - before.x, now.y - before.y)
      draw()
    }
    gesture.current = now
  }
  const finish = (event: PointerEvent<HTMLElement>, cancelled = false) => {
    if (!pointers.current.has(event.pointerId)) return
    const original = pressRemote.current
    const tap = !cancelled && !moved.current && pointers.current.size === 1 && press.current && Math.hypot(event.clientX - press.current.x, event.clientY - press.current.y) <= 12
    pointers.current.delete(event.pointerId)
    gesture.current = currentGesture()
    if (pointers.current.size === 0) { press.current = null; pressRemote.current = null }
    else moved.current = true
    if (!tap || !canClick || !image.current) return
    const box = image.current.getBoundingClientRect()
    const x = (event.clientX - box.left) / box.width, y = (event.clientY - box.top) / box.height
    if (!original && (x < 0 || x >= 1 || y < 0 || y >= 1)) return
    const remote = original ?? { x: Math.min(shot.width - 1, Math.floor(x * shot.width)), y: Math.min(shot.height - 1, Math.floor(y * shot.height)) }
    const field = shot.editables?.find(f => remote.x >= f.x && remote.x <= f.x + f.width && remote.y >= f.y && remote.y <= f.y + f.height)
    const answer = onTap(remote.x, remote.y)
    // The visible footer editor receives focus in the trusted tap. Its DOM
    // identity is independent of screenshots and editable geometry refreshes.
    if (field) { onKeyboard(field.kind, answer.then(value => value.ok && value.editable !== null)); focusOnTouchEnd.current = event.pointerType === "touch"; fieldTapAt.current = Date.now() }
    else { fieldTapAt.current = 0; onCloseKeyboard() }
  }

  return <div className="browser-picture">
    <div className="browser-zoom" role="group" aria-label={nextWord("browserPanelZoom")}>
      {layoutControl}
      <button type="button" className="chip" aria-label={nextWord("browserPanelZoomOut")} onClick={() => scaleTo(view.scale / 1.5)}>−</button>
      <button type="button" className="browser-scale" aria-label={nextWord("browserPanelOriginal")} title={nextWord("browserPanelOriginal")} onClick={native}>{Math.round(fitted.current * view.scale * 100)}%</button>
      <button type="button" className="chip" aria-label={nextWord("browserPanelZoomIn")} onClick={() => scaleTo(view.scale * 1.5)}>+</button>
      <button type="button" className="chip" onClick={fit}>{nextWord("browserPanelFit")}</button>
    </div>
    <div className="browser-viewport" ref={frame} tabIndex={0} aria-label={nextWord(person ? "browserPanelTapHint" : "browserPanelMoveHint")} data-person={person ? "true" : undefined}
      onPointerDown={start} onPointerMove={move} onPointerUp={(e) => finish(e)} onPointerCancel={(e) => finish(e, true)} onLostPointerCapture={(e) => finish(e, true)}
      onTouchEnd={(e) => { if (focusOnTouchEnd.current) { focusOnTouchEnd.current = false; if (e.cancelable) e.preventDefault(); onFocusKeyboard() } }}
      onFocus={(e) => { if (e.target === e.currentTarget && Date.now() - fieldTapAt.current < 1000) onFocusKeyboard() }}
      onKeyDown={(e) => {
        if (e.key === "+" || e.key === "=") { e.preventDefault(); scaleTo(view.scale * 1.5) }
        else if (e.key === "-") { e.preventDefault(); scaleTo(view.scale / 1.5) }
        else if (["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown"].includes(e.key)) {
          e.preventDefault(); const box = frame.current!.getBoundingClientRect()
          zoom.current.panBy(e.key === "ArrowLeft" ? box.width / 4 : e.key === "ArrowRight" ? -box.width / 4 : 0, e.key === "ArrowUp" ? box.height / 4 : e.key === "ArrowDown" ? -box.height / 4 : 0); draw()
        }
      }}>
      <img ref={image} className="browser-shot" src={"data:image/jpeg;base64," + shot.data} draggable={false}
        width={shot.width} height={shot.height} alt={nextWord("browserPanelImageAlt", { title: shot.title || shot.url })}
        style={{ width: size.width, height: size.height, marginLeft: -size.width / 2, marginTop: -size.height / 2, transform: `translate(${view.x}px, ${view.y}px) scale(${view.scale})` }} />
    </div>
  </div>
}
