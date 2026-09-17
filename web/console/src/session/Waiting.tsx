import { useEffect, useLayoutEffect, useRef } from "react"
import type { SessionMenu, SessionMenuOption, SessionMenuStep, SessionMenuSubmit, SessionRow } from "@clawdline/contract"
import { toast, toastFailure } from "../overlays/toast.js"
import * as L from "../legacy/bridge.js"
import { menuKey, pressKey, waitingHTML } from "../legacy/waiting-bridge.js"

/**
 * `div#waiting`: the line above the composer when the open session has
 * stopped to ask something (`view/composer.js` `renderWaiting`).
 *
 * The markup is the original's, built by the same string code
 * (`legacy/waiting-bridge.ts`) and written the way the original writes it —
 * `innerHTML` only when it changed, so the button somebody is reaching for is
 * not replaced under them every beat, and `hidden` beside it. React renders
 * the element once and never its children.
 *
 * **Answering goes through `POST /key`, never `/send`.** A picker throws a
 * bracketed paste away and acts on the Return after it: with the caret on the
 * third option, sending the word "Tea" answered "Water". A digit outside a
 * paste is the only press that answers the question that was asked.
 *
 * Three things this daemon cannot do are drawn switched off rather than left
 * out: the refresh button (no `/v1/sessions/refresh` here, so
 * `aria-disabled="true"`), "Show on Mac" and "Live screen" (no `/focus` or
 * `/screen` route yet, so `disabled`).
 */
export function Waiting({ row, write }: { row: SessionRow | null; write: boolean }) {
  const box = useRef<HTMLDivElement>(null)
  const state = useRef<CardState>({ drawn: null, answered: null, dismissed: null, folded: null })
  const current = useRef<{ row: SessionRow | null; write: boolean }>({ row, write })
  current.current = { row, write }

  const render = () => {
    const el = box.current
    if (!el) return
    const st = state.current
    const restoreRefreshFocus = !!(document.activeElement as Element | null)?.closest?.("[data-refresh]")
    const open = current.current.row
    const key = open ? open.id : null
    const menu: SessionMenu | null = open && open.state === "waiting" && open.menu ? open.menu : null
    let rows: SessionMenuOption[] | null = menu && menu.options && menu.options.length ? menu.options : null
    let submit: SessionMenuSubmit | null = menu && menu.submit && menu.submit.label ? menu.submit : null
    let question = menu && typeof menu.question === "string" ? menu.question : ""
    let steps: SessionMenuStep[] | null = menu && Array.isArray(menu.steps) && menu.steps.length ? menu.steps : null
    if (!question.trim()) question = ""
    // Given up after ten seconds: past that, the session still waiting is not
    // the answer's own gap, and the honest fallback beats a dead menu.
    if (st.answered && (!open || st.answered.key !== key || open.state !== "waiting" || Date.now() - st.answered.at > 10000)) {
      st.answered = null
    }
    if (st.dismissed && (!open || st.dismissed.key !== key || open.state !== "waiting")) st.dismissed = null
    if (st.folded && (!open || st.folded.key !== key || open.state !== "waiting")) st.folded = null
    const hushed = !!(st.dismissed && st.dismissed.menu === menuKey(menu))
    const folded = !!(st.folded && st.folded.menu === menuKey(menu))
    const sent = !rows && !!st.answered
    if (sent && st.answered) {
      rows = st.answered.rows
      submit = st.answered.submit
      question = st.answered.question
      steps = st.answered.steps
    }
    const want =
      !open || open.state !== "waiting" || hushed
        ? ""
        : waitingHTML({
            folded,
            steps,
            question,
            rows,
            submit,
            sent,
            write: current.current.write,
            refresh: rows ? null : { busy: false, status: "", off: true },
            focusOff: true,
            screenOff: true,
          })
    if (want === st.drawn) return
    st.drawn = want
    el.innerHTML = want
    el.hidden = !want
    if (!want) return
    if (restoreRefreshFocus) {
      const replacement = el.querySelector<HTMLElement>("[data-refresh]")
      replacement?.focus({ preventScroll: true })
    }
    document.dispatchEvent(new CustomEvent("clawdline:rendered"))
  }

  useLayoutEffect(render)

  useEffect(() => {
    const el = box.current
    if (!el) return
    const redraw = () => {
      state.current.drawn = null
      render()
    }
    const click = (ev: MouseEvent) => {
      const target = ev.target as Element | null
      if (!target?.closest) return
      const st = state.current
      const open = current.current.row

      if (target.closest("[data-fold]")) {
        const m = menuKey(open ? open.menu : null)
        st.folded = st.folded && st.folded.menu === m ? null : { key: open ? open.id : null, menu: m }
        redraw()
        return
      }

      if (target.closest("[data-dismiss]")) {
        if (open) st.dismissed = { key: open.id, menu: menuKey(open.menu) }
        redraw()
        return
      }

      const opt = target.closest<HTMLButtonElement>("[data-key]")
      if (opt) {
        if (!open || opt.disabled) return
        // Every option goes dead on the first press: they answer one question,
        // and a second tap in flight would be a stray key in the next one.
        el.querySelectorAll<HTMLButtonElement>(".opt").forEach((b) => {
          b.disabled = true
        })
        // Held, so the next render has something to draw once the picker has
        // gone and the session has not yet stopped waiting.
        if (open.menu && open.menu.options && open.menu.options.length) {
          st.answered = {
            key: open.id,
            rows: open.menu.options,
            submit: open.menu.submit || null,
            question: open.menu.question || "",
            steps: null,
            at: Date.now(),
          }
        }
        const asked = open.id
        const T = L.strings
        pressKey(asked, opt.dataset.key || "")
          .then(() => {
            if (current.current.row?.id === asked) toast(T.webMenuSent)
          })
          .catch((err: unknown) => {
            if (current.current.row?.id !== asked) return
            toastFailure(err, T.webRequestFailed)
            // Drawn again from scratch: the markup has not changed, so the
            // guard would otherwise keep the dead buttons on screen.
            st.answered = null
            redraw()
          })
        return
      }

      // No refresh, focus or screen route on this daemon: those buttons are
      // drawn switched off, and a press on them does nothing.
    }
    el.addEventListener("click", click)
    return () => el.removeEventListener("click", click)
    // `render` reads everything through refs, so the listener is bound once.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return <div ref={box} className="waiting" id="waiting" role="status" hidden></div>
}

interface CardState {
  /** The markup last written (`waitingDrawn`). */
  drawn: string | null
  /** An answer sent, and the session not caught up with it yet (`answeredMenu`). */
  answered: {
    key: string | null
    rows: SessionMenuOption[]
    submit: SessionMenuSubmit | null
    question: string
    steps: SessionMenuStep[] | null
    at: number
  } | null
  /** A menu waved away (`dismissedMenu`), keyed on what it says. */
  dismissed: { key: string | null; menu: string } | null
  /** A card folded out of the way (`foldedMenu`). */
  folded: { key: string | null; menu: string } | null
}
