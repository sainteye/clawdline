import { assistantMenuAdmits, type AssistantFacts } from "./antigravity-ui.js"
import { useEffect, useLayoutEffect, useRef } from "react"
import type { SessionMenu, SessionMenuOption, SessionMenuStep, SessionMenuSubmit, SessionRow } from "@clawdline/contract"
import { toast, toastFailure } from "../overlays/toast.js"
import * as L from "../legacy/bridge.js"
import { askInterrupt } from "../legacy/screen-bridge.js"
import { menuKey, pressKey, rereadSession, waitingHTML, type KeyFailure } from "../legacy/waiting-bridge.js"
import { nextWord } from "../next-strings.js"
import { MenuFollow, MENU_FOLLOW } from "./menu-follow.js"
import { FOLLOW_MS } from "./transcript-follow.js"
import { menuFingerprint } from "./fingerprint.js"
import { outcomeOf } from "./outcome.js"
import { PressHolds } from "./press-holds.js"
import { turnPendingSpinners } from "./spinners.js"
import "./pending.css"

/**
 * The page's presses, one per session, kept outside the card: switching to
 * another session and back must not open the options under a press still on
 * its way (`press-holds.ts`).
 */
const holds = new PressHolds()

/**
 * How long a press may take before the card says it is on its way — the start
 * sheet's `startPress` rule, so a daemon on this machine, which answers inside
 * a frame, never flashes the line.
 */
const PRESS_SHOWN_MS = 150

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
 * **A press names the question it answers** (`fingerprint.ts`), and the
 * daemon types nothing at any other: a question that moved comes back as
 * `menu_moved`, with nothing typed (F1).
 *
 * **A press says where it has got to**, as a message does: on its way (the
 * card's line becomes `webSending` with the pending card's spinner, once the
 * press has taken longer than `PRESS_SHOWN_MS`), then `webMenuSent`. A refusal
 * that proves nothing was typed is said in a toast with every option live
 * again; any other failure may have answered, so the options stay shut and the
 * line says it is not known, beside "choose again" (`press-holds.ts`, F3).
 * Across Clawdline Cloud the first of those is a second or more
 * (`cloud/relay-writer.ts`).
 *
 * **"Read again" is on every waiting card.** A question can reach the page
 * late — the machine reads a screen it holds, and across Cloud states a row at
 * its next pass — and the card had no way to say whether anything was being
 * read. The button asks the machine to capture this Session's screen now and
 * state its row again (`rereadSession`), says it is reading while it does, and
 * says when the machine has taken the ask. "Live screen" opens the session's
 * screen panel when the page gave the card a way to (`onScreen`). The card
 * folds out of the way and does not close: a closed card left a question with
 * no way to answer it from the page until it changed, and the bring-forward button was
 * never live here (both removed 2026-10-14).
 *
 * **A menu whose choices could not be read still has a way out.** The card
 * says so and offers two things that need no reading: the live screen, to see
 * what is asked, and one Esc (`POST /interrupt`, the stop row's own route),
 * which closes the picker so the answer can be said in the message box. On
 * 2026-09-26 a session on a Linux machine sat on a picker its 80×24 pane had
 * cut the top off; the card told the person to answer "on the Mac", and there
 * was nothing on the phone that could move it.
 */
export function Waiting({ row, write, onScreen, assistantFacts }: { row: SessionRow | null; write: boolean; assistantFacts?: AssistantFacts; onScreen?: () => void }) {
  const box = useRef<HTMLDivElement>(null)
  const follow = useRef(new MenuFollow())
  const state = useRef<CardState>({ drawn: null, answered: null, folded: null, reread: null })
  const current = useRef<{ row: SessionRow | null; write: boolean; onScreen?: () => void; assistantFacts?: AssistantFacts }>({ row, write, onScreen, assistantFacts })
  current.current = { row, write, onScreen, assistantFacts }

  const render = () => {
    const el = box.current
    if (!el) return
    const st = state.current
    const open = current.current.row
    follow.current.observe(open?.id, open?.execution_generation, open?.state === "waiting" && open.menu?.options?.length ? menuKey(open.menu) : "", Date.now(), open?.state === "waiting")
    const following = !!follow.current.active
    const key = open ? open.id : null
    const menu: SessionMenu | null = open && open.state === "waiting" && open.menu ? open.menu : null
    let rows: SessionMenuOption[] | null = menu && menu.options && menu.options.length ? menu.options : null
    let submit: SessionMenuSubmit | null = menu && menu.submit && menu.submit.label ? menu.submit : null
    let question = menu && typeof menu.question === "string" ? menu.question : ""
    let steps: SessionMenuStep[] | null = menu && Array.isArray(menu.steps) && menu.steps.length ? menu.steps : null
    if (!question.trim()) question = ""
    // Given up after ten seconds: past that, the session still waiting is not
    // the answer's own gap, and the honest fallback beats a dead menu.
    if (st.answered && (!open || st.answered.key !== key || (!following && open.state !== "waiting") || Date.now() - st.answered.at > 10000)) {
      st.answered = null
    }
    if (st.reread && (!open || st.reread.key !== key || open.state !== "waiting" && !following)) st.reread = null
    if (st.folded && (!open || st.folded.key !== key || open.state !== "waiting")) st.folded = null
    // A press is held until the question it answered has gone — the session
    // moved on, or asks something else — or for ten seconds after the machine
    // said yes, the same allowance `answered` has (`press-holds.ts`).
    const press = open ? holds.current(open.id, menuKey(menu), open.state === "waiting", Date.now()) : null
    const folded = !!(st.folded && st.folded.menu === menuKey(menu))
    const sent = !!st.answered && (following || !rows) ||
      following && follow.current.active?.before === menuKey(menu) && !!follow.current.active?.before
    if (sent && st.answered) {
      rows = st.answered.rows
      submit = st.answered.submit
      question = st.answered.question
      steps = st.answered.steps
    }
    const want =
      !open || open.state !== "waiting" && !following
        ? ""
        : waitingHTML({
            folded,
            steps,
            question,
            rows,
            submit,
            sent,
            pressing: !press
              ? null
              : press.state === "unknown"
                ? "unknown"
                : press.state === "sent"
                  ? press.ticks
                    ? "ticked"
                    : "sent"
                  : press.shown
                    ? "sending"
                    : null,
            write: current.current.write && assistantMenuAdmits(open, current.current.assistantFacts),
            refresh: {
              busy: !!st.reread?.busy,
              status: st.reread && !st.reread.busy && st.reread.done ? nextWord("menuRereadAsked") : "",
            },
            screenOff: !current.current.onScreen,
            cancel: current.current.write && assistantMenuAdmits(open, current.current.assistantFacts),
            screen: open.screen_reading ?? null,
            arriving: following && !follow.current.expired(Date.now()) || open.closeability?.source?.provenance === "pinned_list" ||
              (open as { menu_arriving?: unknown }).menu_arriving === true,
            inferred: !sent && menu?.source === "transcript",
          })
    if (want === st.drawn) return
    st.drawn = want
    el.innerHTML = want
    el.hidden = !want
    // A press still on its way holds every option, however often the card is
    // written again under it: a second tap would be a stray key in the next question.
    if (press) {
      el.querySelectorAll<HTMLButtonElement>(".opt").forEach((b) => {
        b.disabled = true
      })
    }
    // A line on its way came or went with that write: the clock follows it.
    turnPendingSpinners()
    if (!want) return
    document.dispatchEvent(new CustomEvent("clawdline:rendered"))
  }

  useLayoutEffect(render)

  // An unread waiting menu needs an actual read, even if no further event arrives.
  const missing = row?.state === "waiting" && !row.menu?.options?.length
  useEffect(() => {
    if (missing && row && !follow.current.active) follow.current.start(row.id, row.execution_generation, "", Date.now(), false)
  }, [row?.id, row?.execution_generation, missing])

  useEffect(() => {
    const el = box.current
    if (!el) return
    const redraw = () => {
      state.current.drawn = null
      render()
    }
    const tick = () => {
      render()
      if (state.current.reread?.busy) return
      void follow.current.read(Date.now(), document.visibilityState !== "hidden", async (id) => {
        const ask = { key: id, busy: true, done: false }
        state.current.reread = ask
        redraw()
        try {
          await rereadSession(id)
          ask.done = true
        } finally { ask.busy = false }
      }, redraw)
    }
    const start = (ev: Event) => {
      const open = current.current.row
      if (!open || (ev as CustomEvent<string>).detail !== open.id) return
      follow.current.start(open.id, open.execution_generation, "", Date.now())
      tick()
    }
    const timer = setInterval(tick, FOLLOW_MS)
    document.addEventListener(MENU_FOLLOW, start)
    document.addEventListener("visibilitychange", tick)
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

      // Read again: one ask at a time, its answer said on the card. The
      // question itself comes the way every row does.
      const reread = target.closest<HTMLButtonElement>("[data-refresh]")
      if (reread) {
        if (!open || reread.disabled || st.reread?.busy) return
        const asked = open.id
        const ask = { key: asked, busy: true, done: false }
        st.reread = ask
        redraw()
        rereadSession(asked).then(
          () => {
            if (state.current.reread !== ask) return
            ask.busy = false
            ask.done = true
            redraw()
          },
          (err: KeyFailure) => {
            if (state.current.reread !== ask) return
            state.current.reread = null
            redraw()
            if (current.current.row?.id === asked) toastFailure(err, L.strings.webRequestFailed)
          },
        )
        return
      }

      // "Choose again" under a press that may have landed: the person's
      // decision. The daemon still checks the next press against its screen.
      if (target.closest("[data-press-again]")) {
        if (open) holds.release(open.id)
        st.answered = null
        redraw()
        return
      }

      const opt = target.closest<HTMLButtonElement>("[data-key]")
      if (opt) {
        if (!open || opt.disabled || !assistantMenuAdmits(open, current.current.assistantFacts)) return
        // Every option goes dead on the first press: they answer one question,
        // and a second tap in flight would be a stray key in the next one.
        el.querySelectorAll<HTMLButtonElement>(".opt").forEach((b) => {
          b.disabled = true
        })
        // A multi-select's rows carry a tick, and pressing one toggles it:
        // nothing is sent, the question does not move, and the next press is
        // the next box. The card is told that much so it neither says the
        // answer went nor shuts the rows somebody is still choosing between.
        const pressed = opt.dataset.key || ""
        const ticks = (open.menu?.options ?? []).some(
          (o) => String(o.n) === pressed && typeof o.checked === "boolean",
        )
        // Held, so the next render has something to draw once the picker has
        // gone and the session has not yet stopped waiting. A tick never
        // empties the picker, so it holds nothing.
        if (!ticks && open.menu && open.menu.options && open.menu.options.length) {
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
        const expect = open.menu ? menuFingerprint(open.menu) : ""
        const press = holds.start(asked, menuKey(open.menu), Date.now(), ticks)
        setTimeout(() => {
          if (holds.current(asked, press.menu, true, Date.now()) !== press) return
          press.shown = true
          redraw()
        }, PRESS_SHOWN_MS)
        pressKey(asked, pressed, expect, press.request)
          .then(() => {
            // Still held — the question has not gone yet — and now saying so.
            holds.settled(press, Date.now())
            const stillOpen = current.current.row
            if (stillOpen?.id === asked && (open.menu?.question === "Select Model and Effort" ||
              /^More reasoning/.test(open.menu?.options?.find((option) => String(option.n) === pressed)?.label ?? ""))) {
              follow.current.start(asked, stillOpen.execution_generation, menuKey(open.menu), Date.now())
              tick()
            }
            redraw()
            if (current.current.row?.id === asked) toast(ticks ? nextWord("menuTicked") : T.webMenuSent)
          })
          .catch((err: KeyFailure) => {
            const code = typeof err?.code === "string" ? err.code : ""
            const outcome = outcomeOf({ status: err?.status ?? null, code, said: err?.outcome })
            holds.failed(press, outcome)
            // Refused before anything was typed: the options are live again,
            // drawn from scratch — the markup has not changed, so the guard
            // would otherwise keep the dead buttons on screen. Otherwise the
            // press may have answered and the card says so instead.
            if (outcome === "not_done") st.answered = null
            if (current.current.row?.id === asked) {
              if (code === "menu_moved") toast(nextWord("menuMoved"))
              else if (code === "menu_unverified") toast(nextWord("menuUnverified"))
              else if (outcome === "not_done") toastFailure(err, T.webRequestFailed)
            }
            redraw()
          })
        return
      }

      if (target.closest("[data-screen]")) {
        current.current.onScreen?.()
        return
      }

      // One Esc, and nothing read first: there was nothing to read. The
      // button goes dead until the answer comes back, so a second tap is not
      // a second Esc into whatever the session does next.
      const cancel = target.closest<HTMLButtonElement>("[data-cancel]")
      if (cancel) {
        if (!open || cancel.disabled || !assistantMenuAdmits(open, current.current.assistantFacts)) return
        cancel.disabled = true
        const asked = open.id
        askInterrupt(asked).then(
          () => {
            if (current.current.row?.id === asked) {
              follow.current.active = null
              redraw()
              toast(nextWord("menuCancelSent"))
            }
          },
          (err: unknown) => {
            cancel.disabled = false
            if (current.current.row?.id === asked) toastFailure(err, L.strings.webRequestFailed)
          },
        )
        return
      }
    }
    el.addEventListener("click", click)
    return () => {
      el.removeEventListener("click", click)
      document.removeEventListener(MENU_FOLLOW, start)
      document.removeEventListener("visibilitychange", tick)
      clearInterval(timer)
      follow.current.active = null
    }
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
  /** A card folded out of the way (`foldedMenu`). */
  folded: { key: string | null; menu: string } | null
  /** A "read again" on its way (`busy`) or taken by the machine (`done`). */
  reread: { key: string | null; busy: boolean; done: boolean } | null
}
