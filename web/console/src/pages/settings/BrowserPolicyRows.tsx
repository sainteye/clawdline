import { useRef, useState, type KeyboardEvent, type PointerEvent } from "react"
import { nextWord, type NextWord } from "../../next-strings.js"
import { openBrowserSetup } from "../../session/BrowserSetup.js"
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
  const [drag, setDrag] = useState<{ browser: BrowserMode; order: BrowserMode[]; pointer: number } | null>(null)
  const list = useRef<HTMLOListElement>(null)
  const order = drag?.order ?? policy.order
  const locked = busy || !supported

  const start = (event: PointerEvent<HTMLButtonElement>, browser: BrowserMode) => {
    if (locked || (event.pointerType === "mouse" && event.button !== 0)) return
    event.preventDefault()
    event.currentTarget.setPointerCapture?.(event.pointerId)
    setDrag({ browser, order: [...policy.order], pointer: event.pointerId })
  }
  const move = (event: PointerEvent<HTMLButtonElement>) => {
    if (!drag || event.pointerId !== drag.pointer || !list.current) return
    const rows = [...list.current.children] as HTMLElement[]
    // Where the row goes: after every other row whose middle is above the pointer.
    let index = 0
    rows.forEach((row, i) => {
      if (drag.order[i] === drag.browser) return
      const box = row.getBoundingClientRect()
      if (box.top + box.height / 2 < event.clientY) index++
    })
    const next = placeBrowser(drag.order, drag.browser, index)
    if (next.join() !== drag.order.join()) setDrag({ ...drag, order: next })
  }
  const end = (event: PointerEvent<HTMLButtonElement>, keep: boolean) => {
    if (!drag || event.pointerId !== drag.pointer) return
    const next = drag.order
    setDrag(null)
    if (keep && next.join() !== policy.order.join()) onChange({ ...policy, order: next })
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
        const id = "browser-policy-" + browser
        return <li key={browser} data-browser={browser} data-dragging={drag?.browser === browser ? "true" : undefined}>
          <button type="button" className="browser-policy-handle" disabled={locked}
            aria-label={nextWord("browserPolicyDrag", { browser: title, position: String(index + 1), count: String(order.length) })}
            onPointerDown={(e) => start(e, browser)} onPointerMove={move}
            onPointerUp={(e) => end(e, true)} onPointerCancel={(e) => end(e, false)}
            onKeyDown={(e) => key(e, browser)}>
            <svg viewBox="0 0 14 20" aria-hidden="true"><circle cx="4" cy="4" r="1.6" /><circle cx="10" cy="4" r="1.6" /><circle cx="4" cy="10" r="1.6" /><circle cx="10" cy="10" r="1.6" /><circle cx="4" cy="16" r="1.6" /><circle cx="10" cy="16" r="1.6" /></svg>
          </button>
          <div className="browser-policy-copy">
            <strong id={id + "-title"}>{title}</strong>
            {ready
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
