import { useEffect, useImperativeHandle, useLayoutEffect, useRef, useState, type PointerEvent, type ReactNode, type Ref } from "react"
import { flushSync } from "react-dom"
import { createImageZoom } from "../legacy/js/view/transcript-images.js"
import { nextWord } from "../next-strings.js"
import { BrowserInputQueue, type BrowserEdit, type InputReceipt } from "./browser-keyboard.js"

type Point = { x: number; y: number }
type Gesture = Point & { span: number }
type Editable = { x: number; y: number; width: number; height: number; kind: string }
type Shot = { data: string; width: number; height: number; url: string; title: string; editables?: Editable[] }
export type BrowserKeyboardHandle = { open: () => void; close: () => void }
const CARET = "\u200b"

/** The existing picture-preview zoom model also keeps the live tab under the fingers.
 * Gestures change only this view. Only a released, unmoved single pointer may
 * become a browser click, and its coordinates come from the transformed image.
 */
export function BrowserViewport({ shot, canClick, person, onTap, onEdit, onPrepareKeyboard, keyboardRef, layoutControl }: {
  shot: Shot; canClick: boolean; person: boolean; onTap: (x: number, y: number) => Promise<InputReceipt>;
  onEdit: (edit: BrowserEdit) => Promise<InputReceipt>; onPrepareKeyboard: () => Promise<boolean>;
  keyboardRef: Ref<BrowserKeyboardHandle>; layoutControl: ReactNode;
}) {
  const frame = useRef<HTMLDivElement>(null)
  const image = useRef<HTMLImageElement>(null)
  const zoom = useRef(createImageZoom())
  const pointers = useRef(new Map<number, Point>())
  const gesture = useRef<Gesture | null>(null)
  const press = useRef<Point | null>(null)
  const moved = useRef(false)
  const mode = useRef<"fit" | "native" | "custom">(window.matchMedia("(pointer: coarse) and (max-width: 899px)").matches ? "native" : "fit")
  const fitted = useRef(0)
  const [size, setSize] = useState({ width: 0, height: 0 })
  const [view, setView] = useState(zoom.current.state())
  const editor = useRef<HTMLInputElement>(null)
  const composing = useRef(false)
  const keyboardEpoch = useRef(0)
  const activeField = useRef<Point | null>(null)
  const [kind, setKind] = useState("text")
  const [inputPending, setInputPending] = useState(false)
  const [inputError, setInputError] = useState<"full" | "unconfirmed" | null>(null)
  const send = useRef(onEdit); send.current = onEdit
  const input = useRef<BrowserInputQueue | null>(null)
  if (!input.current) input.current = new BrowserInputQueue(edit => send.current(edit), reason => { setInputError(reason); editor.current?.blur() }, setInputPending, next => { if (next === null) editor.current?.blur() })
  const draw = () => setView({ ...zoom.current.state() })
  const closeKeyboard = () => { keyboardEpoch.current++; input.current!.cancel(); editor.current?.blur(); activeField.current = null; composing.current = false }
  const openKeyboard = (next: string, ready: Promise<boolean>, point: Point | null = null) => {
    const epoch = ++keyboardEpoch.current
    input.current!.start(ready); activeField.current = point
    flushSync(() => { setKind(next); setInputError(null) })
    if (editor.current) {
      editor.current.value = CARET
      editor.current.focus({ preventScroll: true })
      editor.current.setSelectionRange(1, 1)
    }
    void ready.then(ok => { if (!ok && epoch === keyboardEpoch.current) closeKeyboard() })
  }
  useImperativeHandle(keyboardRef, () => ({ open: () => openKeyboard("text", onPrepareKeyboard()), close: closeKeyboard }))
  useEffect(() => { if (!person) closeKeyboard() }, [person])
  useEffect(() => () => input.current?.cancel(), [])

  const commitInput = (inputType: string) => {
    const element = editor.current
    if (!element || composing.current) return
    if (inputType.startsWith("delete")) input.current!.add({ op: "key", args: { key: "Backspace" } })
    else {
      const value = element.value.startsWith(CARET) ? element.value.slice(1) : element.value
      if (value) input.current!.add({ op: "type", args: { text: value } })
    }
    element.value = CARET; element.setSelectionRange(1, 1)
  }

  useLayoutEffect(() => {
    const element = frame.current
    if (!element) return
    const measure = () => {
      const box = element.getBoundingClientRect()
      if (!box.width || !box.height || !shot.width || !shot.height) return
      const scale = Math.min(1, box.width / shot.width, box.height / shot.height)
      const before = fitted.current * zoom.current.state().scale
      pointers.current.clear(); gesture.current = null; press.current = null
      zoom.current.layout({ frame: box, content: { width: shot.width * scale, height: shot.height * scale }, natural: { width: shot.width, height: shot.height } })
      fitted.current = scale
      setSize({ width: shot.width * scale, height: shot.height * scale })
      zoom.current.scaleTo(mode.current === "fit" ? 1 : (before || 1) / scale, undefined, undefined)
      // Start a phone at the first readable row, not halfway across the page.
      if (!before && mode.current === "native") zoom.current.panBy(shot.width, shot.height)
      if (activeField.current && document.activeElement === editor.current) {
        const value = zoom.current.state(), point = activeField.current
        zoom.current.panBy(-(point.x - shot.width / 2) * scale * value.scale - value.x, -(point.y - shot.height / 2) * scale * value.scale - value.y)
      }
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
  const start = (event: PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0) return
    event.preventDefault()
    event.currentTarget.setPointerCapture(event.pointerId)
    const point = { x: event.clientX, y: event.clientY }
    if (pointers.current.size === 0) { press.current = point; moved.current = false }
    pointers.current.set(event.pointerId, point)
    if (pointers.current.size > 1) moved.current = true
    gesture.current = currentGesture()
  }
  const move = (event: PointerEvent<HTMLDivElement>) => {
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
  const finish = (event: PointerEvent<HTMLDivElement>, cancelled = false) => {
    if (!pointers.current.has(event.pointerId)) return
    const tap = !cancelled && !moved.current && pointers.current.size === 1 && press.current && Math.hypot(event.clientX - press.current.x, event.clientY - press.current.y) <= 12
    pointers.current.delete(event.pointerId)
    gesture.current = currentGesture()
    if (pointers.current.size === 0) press.current = null
    else moved.current = true
    if (!tap || !canClick || input.current!.pending || !image.current) return
    const box = image.current.getBoundingClientRect()
    const x = (event.clientX - box.left) / box.width, y = (event.clientY - box.top) / box.height
    if (x < 0 || x >= 1 || y < 0 || y >= 1) return
    const remote = { x: Math.min(shot.width - 1, Math.floor(x * shot.width)), y: Math.min(shot.height - 1, Math.floor(y * shot.height)) }
    const field = shot.editables?.find(f => remote.x >= f.x && remote.x <= f.x + f.width && remote.y >= f.y && remote.y <= f.y + f.height)
    const answer = onTap(remote.x, remote.y)
    // Focus inside the original trusted tap. Waiting for Cloud before focus
    // loses iOS's keyboard activation, so geometry arrives with the picture.
    if (field) openKeyboard(field.kind, answer.then(value => value.ok && value.editable !== null), remote)
    else closeKeyboard()
    const epoch = keyboardEpoch.current
    void answer.then(value => { if (epoch === keyboardEpoch.current && (!value.ok || value.editable === null)) closeKeyboard() })
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
      <input ref={editor} id="browser-native-keyboard" className="browser-native-keyboard" tabIndex={-1} type={kind === "password" ? "password" : "text"}
        inputMode={kind === "email" ? "email" : kind === "number" ? "decimal" : kind === "tel" ? "tel" : kind === "url" ? "url" : "text"}
        aria-label={nextWord("browserPanelNativeInput")} autoComplete="off" autoCorrect="off" autoCapitalize="off" spellCheck={false}
        onInput={e => { if (!(e.nativeEvent as InputEvent).isComposing) commitInput((e.nativeEvent as InputEvent).inputType || "insertText") }}
        onCompositionStart={() => { composing.current = true }} onCompositionEnd={() => { composing.current = false; commitInput("insertText") }}
        onKeyDown={e => { e.stopPropagation(); if (e.nativeEvent.isComposing || composing.current) return; if (e.key === "Enter" || e.key === "Tab") { e.preventDefault(); input.current!.add({ op: "key", args: { key: e.key } }) } }} />
      {inputPending && <span className="browser-input-pending" role="status">{nextWord("browserPanelTyping")}</span>}
      {inputError && <p className="browser-input-error" role="alert">{nextWord(inputError === "full" ? "browserPanelInputFull" : "browserPanelInputUnconfirmed")}</p>}
    </div>
  </div>
}
