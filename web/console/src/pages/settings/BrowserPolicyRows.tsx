import { useEffect, useRef, useState, type KeyboardEvent, type PointerEvent } from "react"
import { nextWord, type NextWord } from "../../next-strings.js"
import { openBrowserSetup } from "../../session/BrowserSetup.js"
import { extensionCurrent } from "../../session/browser-extension.js"
import type { BrowserMode, BrowserPolicy, BrowserProfileReadiness } from "./api.js"
import { Switch } from "./Switch.js"

const TITLES: Record<BrowserMode, NextWord> = { native: "browserModeNative", isolated: "browserModeIsolated", user_chrome: "browserModeUserChrome" }

export function moveBrowser(policy: BrowserPolicy, browser: BrowserMode, direction: -1 | 1): BrowserPolicy {
  const index = policy.order.indexOf(browser), target = index + direction
  if (index < 0 || target < 0 || target >= policy.order.length) return policy
  const order = [...policy.order]
  ;[order[index], order[target]] = [order[target], order[index]]
  return { ...policy, order }
}

/** The order with browser placed at index, the others keeping their order. */
export function placeBrowser(order: readonly BrowserMode[], browser: BrowserMode, index: number): BrowserMode[] {
  const rest = order.filter((b) => b !== browser)
  const at = Math.max(0, Math.min(index, rest.length))
  return [...rest.slice(0, at), browser, ...rest.slice(at)]
}

/**
 * The order agents try browsers in, one row each: a drag handle, the name, one
 * line of state and the automatic switch. Dragging the handle reorders the
 * rows live and saves once on release; on the focused handle the arrow keys
 * move a row one place, so no arrow buttons are drawn. Only a browser that is
 * not connected offers its setup.
 */
export function BrowserPolicyRows({ policy, profiles, busy, supported, onChange }: {
  policy: BrowserPolicy
  profiles?: Partial<Record<BrowserMode, BrowserProfileReadiness>>
  busy: boolean
  supported: boolean
  onChange: (value: BrowserPolicy) => void
}) {
  // While dragging, the DOM order stays put: the picked row follows the
  // pointer and the rows it passes slide out of its way. Release commits the
  // new order once; until the saved policy shows it, that order is kept so
  // the rows do not jump back.
  type Drag = { browser: BrowserMode; pointer: number; from: number; startY: number; dy: number; target: number; tops: number[]; heights: number[] }
  const [drag, setDrag] = useState<Drag | null>(null)
  const [settled, setSettled] = useState<BrowserMode[] | null>(null)
  const list = useRef<HTMLOListElement>(null)
  const order = settled ?? policy.order
  useEffect(() => {
    // The saved order arrived, or the save finished without it (a refusal
    // keeps the old order): follow the policy again.
    if (settled && (settled.join() === policy.order.join() || !busy)) setSettled(null)
  }, [policy.order, busy])
  const locked = busy || !supported

  const start = (event: PointerEvent<HTMLButtonElement>, browser: BrowserMode) => {
    if (locked || !list.current || (event.pointerType === "mouse" && event.button !== 0)) return
    event.preventDefault()
    event.currentTarget.setPointerCapture?.(event.pointerId)
    const rows = [...list.current.children] as HTMLElement[]
    const boxes = rows.map((row) => row.getBoundingClientRect())
    setDrag({ browser, pointer: event.pointerId, from: order.indexOf(browser), startY: event.clientY, dy: 0, target: order.indexOf(browser),
      tops: boxes.map((b) => b.top), heights: boxes.map((b) => b.height) })
  }
  const move = (event: PointerEvent<HTMLButtonElement>) => {
    if (!drag || event.pointerId !== drag.pointer) return
    const last = drag.tops.length - 1
    // Keep the row within the list, and place it where its middle now is.
    const dy = Math.max(drag.tops[0] - drag.tops[drag.from], Math.min(drag.tops[last] - drag.tops[drag.from], event.clientY - drag.startY))
    const middle = drag.tops[drag.from] + dy + drag.heights[drag.from] / 2
    let target = 0
    drag.tops.forEach((top, i) => { if (i !== drag.from && top + drag.heights[i] / 2 < middle) target++ })
    setDrag({ ...drag, dy, target })
  }
  const end = (event: PointerEvent<HTMLButtonElement>, keep: boolean) => {
    if (!drag || event.pointerId !== drag.pointer) return
    setDrag(null)
    if (!keep || drag.target === drag.from) return
    const next = placeBrowser(order, drag.browser, drag.target)
    setSettled(next)
    onChange({ ...policy, order: next })
  }
  // How far each row sits from its place while another is dragged over it.
  const shift = (index: number): number => {
    if (!drag) return 0
    if (index === drag.from) return drag.dy
    const h = drag.heights[drag.from]
    if (drag.from < drag.target && index > drag.from && index <= drag.target) return -h
    if (drag.from > drag.target && index >= drag.target && index < drag.from) return h
    return 0
  }
  const key = (event: KeyboardEvent<HTMLButtonElement>, browser: BrowserMode) => {
    if (locked || (event.key !== "ArrowUp" && event.key !== "ArrowDown")) return
    event.preventDefault()
    const next = moveBrowser(policy, browser, event.key === "ArrowUp" ? -1 : 1)
    if (next !== policy) onChange(next)
  }

  return <div className="browser-policy">
    {!supported && <p className="say">{nextWord("browserPolicyNeedsUpdate")}</p>}
    <ol className="browser-policy-list" ref={list}>
      {order.map((browser, index) => {
        const title = nextWord(TITLES[browser])
        const automatic = policy.automatic[browser]
        const ready = browser === "native" || profiles?.[browser]?.ready === true
        // A connected extension older than the console needs is updated
        // from its setup guide; it does not update itself.
        const outdated = browser !== "native" && ready && !extensionCurrent(profiles?.[browser]?.host?.version)
        const id = "browser-policy-" + browser
        const offset = shift(index)
        return <li key={browser} data-browser={browser} data-dragging={drag?.browser === browser ? "true" : undefined}
          data-moving={drag ? "true" : undefined} style={offset ? { transform: `translateY(${offset}px)` } : undefined}>
          <button type="button" className="browser-policy-handle" disabled={locked}
            aria-label={nextWord("browserPolicyDrag", { browser: title, position: String(index + 1), count: String(order.length) })}
            onPointerDown={(e) => start(e, browser)} onPointerMove={move}
            onPointerUp={(e) => end(e, true)} onPointerCancel={(e) => end(e, false)}
            onKeyDown={(e) => key(e, browser)}>
            <svg viewBox="0 0 14 20" aria-hidden="true"><circle cx="4" cy="4" r="1.6" /><circle cx="10" cy="4" r="1.6" /><circle cx="4" cy="10" r="1.6" /><circle cx="10" cy="10" r="1.6" /><circle cx="4" cy="16" r="1.6" /><circle cx="10" cy="16" r="1.6" /></svg>
          </button>
          <div className="browser-policy-copy">
            <strong id={id + "-title"}>{title}</strong>
            {outdated
              ? <button type="button" className="browser-policy-setup-link" onClick={() => openBrowserSetup(browser as "isolated" | "user_chrome")}>{nextWord("browserPolicyUpdate")}</button>
              : ready
              ? <span className={browser === "native" ? "browser-policy-state" : "browser-policy-ready"} role="status">{nextWord(browser === "native" ? "browserPolicyNativeReady" : "browserPolicyReady")}</span>
              : <button type="button" className="browser-policy-setup-link" onClick={() => openBrowserSetup(browser as "isolated" | "user_chrome")}>{nextWord("browserPolicyGoSetup")}</button>}
          </div>
          <Switch id={id} labelledBy={id + "-title " + id + "-auto"} on={automatic}
            disabled={locked || (!automatic && !ready)} stateText={nextWord(automatic ? "browserSettingsOn" : "browserSettingsOff")}
            onToggle={() => onChange({ ...policy, automatic: { ...policy.automatic, [browser]: !automatic } })} />
          <span id={id + "-auto"} className="sr-only">{nextWord("browserPolicyAuto")}</span>
        </li>
      })}
    </ol>
  </div>
}
