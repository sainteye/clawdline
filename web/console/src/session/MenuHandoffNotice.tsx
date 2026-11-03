import { useEffect, useId, useRef, useState } from "react"
import { catalogFormat, catalogWord } from "../catalog.js"
import { showModalWithoutChoice } from "../modal-focus.js"
import { MENU_HANDOFF_COUNTDOWN_S } from "./menu-handoff.js"
import "./native-terminal.css"

/**
 * Says that a question Clawdline could not read is about to continue in the
 * Session's Terminal, and switches when the countdown ends unless the person
 * stays. The parent unmounts it as soon as the question's options arrive or
 * the Session moves on, so a late read never switches anyone.
 */
export function MenuHandoffNotice({ onSwitch, onStay }: { onSwitch: () => void; onStay: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null)
  const title = useId()
  const [left, setLeft] = useState(MENU_HANDOFF_COUNTDOWN_S)
  const decided = useRef(false)
  const decide = (switching: boolean) => {
    if (decided.current) return
    decided.current = true
    dialog.current?.close()
    if (switching) onSwitch()
    else onStay()
  }

  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null
    showModalWithoutChoice(dialog.current)
    return () => {
      dialog.current?.close()
      if (opener?.isConnected) opener.focus({ preventScroll: true })
    }
  }, [])

  useEffect(() => {
    if (left <= 0) {
      decide(true)
      return
    }
    const timer = window.setTimeout(() => setLeft(value => value - 1), 1_000)
    return () => window.clearTimeout(timer)
  }, [left])

  return (
    <dialog ref={dialog} className="menu-handoff" aria-labelledby={title}
      onCancel={(event) => { event.preventDefault(); decide(false) }}>
      <h2 id={title}>{catalogWord("nativeTerminal", "handoffTitle")}</h2>
      <p>{catalogWord("nativeTerminal", "handoffBody")}</p>
      <p className="menu-handoff-countdown" aria-live="polite">{catalogFormat("nativeTerminal", "handoffCountdown", [left])}</p>
      <div className="menu-handoff-actions">
        <button className="chip" type="button" onClick={() => decide(false)}>{catalogWord("nativeTerminal", "handoffStay")}</button>
        <button className="chip primary" type="button" onClick={() => decide(true)}>{catalogWord("nativeTerminal", "handoffNow")}</button>
      </div>
    </dialog>
  )
}
