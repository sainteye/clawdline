import { useEffect, useLayoutEffect, useRef, useState } from "react"
import type { ReactNode, RefObject } from "react"
import type { SessionRow } from "@clawdline/contract"
import { RefusalError } from "@clawdline/core"
import { client } from "../client.js"
import * as L from "../legacy/bridge.js"
import * as V from "../legacy/voice-bridge.js"
import { toast } from "../overlays/toast.js"
import { Waiting } from "./Waiting.js"

/**
 * The composer, as the original's `form#composer` is built.
 *
 * Same seven children in the same order — `.waiting`, `.shots`, `.voice`,
 * `.skill-menu`, `.box`, `input#pick`, `.why` — and inside the box the same four:
 * `button.attach`, `button.mic`, `div#msg.msg` and `button.send`. The message box
 * is a `contenteditable` div, as it is there; `legacy/composer.css` sizes it by
 * that element and nothing else (see the note on `.composer .msg` there).
 *
 * What this daemon cannot do is still drawn, switched off, rather than left out:
 * a row that is missing reads as something the reader misremembered.
 *
 * - `.waiting` is its own component (`Waiting.tsx`), filled from the row's
 *   parsed `menu` and answered through `POST /key`, as the original's is.
 * - `.shots`, `input#pick` and `button.attach`: the send route here takes text
 *   only (`SendRequest` is `{ text }`), so the attachment button is disabled.
 * - `.voice` and `button.mic`: dictation, through `POST /v1/voice`. The row is
 *   built by `legacy/voice-bridge.ts` exactly as `input/voice.js` builds it —
 *   once per state, so a tick does not take the buttons out from under a thumb
 *   — and this component owns only the microphone's three attributes, the
 *   element the row is drawn into, and where the words land.
 * - `.skill-menu`: there is no skills route, so the menu never opens.
 *
 * Two things differ from the original and are said here rather than hidden:
 * the original reports a failed send in a toast, and this page has no toast
 * element, so the failure is written on the `.why` line under the box; and the
 * original's `write` flag comes from `/v1/health`, which this daemon's health
 * does not carry, so the box is writable until a send is refused with
 * `write_disabled` — the same correction the original makes to its own flag.
 *
 * `hasKeyboard` and `words` come from the copied modules through the bridge.
 * `useKeyboardBar` below is still a local rendering of `input/edges.js`, which
 * is not among the copied modules.
 */
export function Composer({ row, onDid }: { row: SessionRow | null; onDid: () => void }) {
  const T = L.strings
  const msg = useRef<HTMLDivElement>(null)
  const send = useRef<HTMLButtonElement>(null)
  const [text, setText] = useState("")
  // True from the moment a send starts until its answer comes back. A ref as
  // well as state, because Return does not go through the disabled button and a
  // second Return inside the same render would otherwise send a second request.
  const [sending, setSending] = useState(false)
  const inFlight = useRef(false)
  const [write, setWrite] = useState(true)
  const [failure, setFailure] = useState("")
  const sendWidth = useRef({ word: "", px: 0 })
  const voiceRow = useRef<HTMLDivElement>(null)
  const form = useRef<HTMLFormElement>(null)
  // What the microphone is doing, which is the whole of what `renderComposer`
  // reads back from `Voice`: `voiceBusy` for the two waits, `voiceLive` for
  // the one control that must stay alive through them.
  const [voice, setVoice] = useState<V.VoiceState>(V.voiceState)

  useKeyboardBar(msg)

  const on = write && !!row
  const voiceBusy = voice !== "off"
  const voiceLive = voice === "recording"
  let placeholder = T.placeholder
  if (row?.assistant === "codex") {
    placeholder = placeholder.replace("Claude Code", "Codex").replace("Claude", "Codex")
    if (placeholder === T.placeholder) placeholder = "Codex…"
  }
  const keyboard = L.keyboard()

  // **The width is pinned before the word changes**, as `renderComposer` does:
  // measured while the button says the shorter word and held while it says the
  // longer one, so a press does not move the box under it.
  useLayoutEffect(() => {
    const button = send.current
    if (!button || sending) return
    if (sendWidth.current.word !== T.webSend) sendWidth.current = { word: T.webSend, px: 0 }
    if (!sendWidth.current.px) sendWidth.current.px = button.offsetWidth
  })

  /** What is in the box, as text. `innerText`, so line breaks survive; a non-breaking space becomes an ordinary one. */
  const rawText = () => String(msg.current?.innerText || "").replace(/\u00A0/g, " ")
  const changed = () => {
    setText(rawText())
    setFailure("")
  }

  const caretToEnd = () => {
    const el = msg.current
    const selection = window.getSelection()
    if (!el || !selection) return
    const range = document.createRange()
    range.selectNodeContents(el)
    range.collapse(false)
    selection.removeAllRanges()
    selection.addRange(range)
  }

  /** Text in at the caret through the browser's own edit, so Undo knows about it; the range is the fallback. */
  const insertText = (value: string) => {
    const el = msg.current
    if (!el || !value) return
    const selection = window.getSelection()
    const plain = !!selection && selection.isCollapsed && value.indexOf("\n") < 0
    const before = plain ? (el.textContent ?? "").length : -1
    try {
      if (document.execCommand?.("insertText", false, value) && (!plain || (el.textContent ?? "").length !== before)) return
    } catch {
      /* the range, then */
    }
    if (!selection || !selection.rangeCount) return
    const range = selection.getRangeAt(0)
    if (!el.contains(range.commonAncestorContainer)) return
    range.deleteContents()
    const node = document.createTextNode(value)
    range.insertNode(node)
    range.setStartAfter(node)
    range.collapse(true)
    selection.removeAllRanges()
    selection.addRange(range)
  }

  /**
   * `input/composer.js`'s `appendMsg`: what dictation does with the words.
   *
   * **It stops here.** Nothing on the voice path sends, which is the whole
   * design rather than a step that was left out — a dictation that heard the
   * wrong thing is then a typo rather than an incident. The join rule is the
   * copied `core/compose-text.js`, so a sentence dictated after a typed one is
   * a sentence and not one long word, and a box holding the `<br>` a browser
   * left behind does not get a leading space.
   */
  const appendMsg = (said: string) => {
    const el = msg.current
    if (!el || !said) return
    // Whitespace only is nothing.
    const had = rawText().trim() ? rawText() : ""
    if (document.activeElement === el) {
      caretToEnd()
      insertText(V.appendGap(had) + said)
    } else {
      el.textContent = V.appendedText(had, said)
    }
    changed()
    // The box scrolls at 140px and a dictated paragraph is longer than that.
    // The end is the part worth seeing: it is what just arrived, and it is
    // where the next word would go.
    el.scrollTop = el.scrollHeight
  }
  const sink = useRef(appendMsg)
  sink.current = appendMsg

  // The microphone, attached once. `Voice` is a module rather than a hook for
  // the reason the original is one object: there is one recorder on the page,
  // so there is one of these at a time, and a second composer mounting must
  // not open a second stream.
  useEffect(() => {
    const host = voiceRow.current
    if (!host) return
    V.attachVoice({ say: toast, changed: setVoice })
    V.attachComposerVoice({
      host,
      composer: form.current,
      sink: (said) => sink.current(said),
      // **The composer has gone**, which is the original's `els.composer.hidden`:
      // a microphone left open behind a row that is no longer on the page is
      // one with no button left to shut it. Asked of the element rather than
      // of React, because by then React has already let go of it.
      guard: () => !host.isConnected,
    })
    return () => V.attachComposerVoice(null)
  }, [])

  const submit = async () => {
    if (inFlight.current) return
    const said = rawText().trim()
    if (!said || !row || !write) return
    // A quit line is not a message: the original ends the session instead, while
    // its terminal is still known. Exact and per assistant, so a sentence that
    // mentions `/exit` is still an ordinary prompt.
    const quit = said === (row.assistant === "codex" ? "/quit" : "/exit")
    // **The box empties here, before the request exists**, and nothing puts the
    // words back: a send that fails at this end may already have been delivered.
    if (msg.current) msg.current.textContent = ""
    if (document.activeElement === msg.current) caretToEnd()
    setText("")
    setFailure("")
    inFlight.current = true
    setSending(true)
    try {
      // A resolved send means the bytes reached the tty, not that the assistant
      // read them. Nothing is said on success — the turn appearing in the
      // transcript is the answer.
      await (quit ? client.close(row.id) : client.send(row.id, said))
      onDid()
    } catch (err) {
      const code = err instanceof RefusalError ? err.code : "unexpected_error"
      if (code === "write_disabled") setWrite(false)
      setFailure(L.fillString(T.webFailWithTag, { text: T.sendFailed, tag: code }))
    } finally {
      inFlight.current = false
      setSending(false)
    }
  }

  // The write-off notice is the catalog's own markup, rendered by the copied
  // `words()` exactly as the original renders it; everything else is text.
  const whyHTML = !failure && !write ? L.wordsHTML(T.webWriteOff) : null
  const why: ReactNode = failure || (write ? (row ? "" : T.webWriteOpen) : null)
  const pinned = sending && sendWidth.current.px ? { minWidth: `${sendWidth.current.px}px` } : undefined

  return (
    <form
      ref={form}
      className="composer"
      id="composer"
      data-write={write ? "on" : "off"}
      data-sending={sending ? "on" : "off"}
      data-closing="off"
      onSubmit={(e) => {
        e.preventDefault()
        void submit()
      }}
    >
      <Waiting row={row} write={write} />
      <div className="shots" id="shots"></div>
      {/* Empty here and built in `voice-bridge.ts`, as the original builds it
          in `voice.js`: React renders no children into this row, so it never
          diffs away the meter, the count or the two ways out. `opening` is the
          browser's own permission sheet, which is on top of the page and says
          more than this row could, so the row stays hidden for it. */}
      <div className="voice" id="voice" role="status" hidden={voice === "off" || voice === "opening"} ref={voiceRow}></div>
      {/* The original's label is English in every language; the catalog has no key for it. */}
      <div
        className="skill-menu"
        id="skill-menu"
        role="listbox"
        aria-label={`${row?.assistant === "codex" ? "Codex" : "Claude Code"} skills`}
        hidden
      ></div>
      <div className="box">
        <button
          className="attach"
          id="attach"
          type="button"
          aria-label={T.webAttach}
          title={T.webAttach}
          disabled
          onMouseDown={(e) => e.preventDefault()}
        >
          +
        </button>
        {/* Two icons and never a label — the words are the `aria-label`, which
            is swapped for "stop" while it records so that a screen reader is
            told what the second press will do.

            **Except while it is recording, it goes dead for the two waits.**
            The microphone takes the same permission as the attachment beside
            it, and during a transcription it is not the control that does
            anything — the row above carries the Cancel for exactly that
            stretch. But a session that closes under an open microphone must
            not disable the only control that can shut it: the light would stay
            on with nothing left on screen to press. */}
        <button
          className="mic"
          id="mic"
          type="button"
          aria-label={voiceLive ? T.webVoiceStop : T.webVoiceStart}
          title={voiceLive ? T.webVoiceStop : T.webVoiceStart}
          aria-pressed={voiceLive ? "true" : "false"}
          disabled={voiceLive ? false : !on || sending || voiceBusy}
          onClick={() => V.press()}
          // Pressing it must not take the focus off the box somebody is typing
          // in; the click still lands.
          onMouseDown={(e) => e.preventDefault()}
        >
          <svg className="ico ico-mic" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            <rect x="9" y="3" width="6" height="11" rx="3" fill="currentColor"></rect>
            <path
              d="M5.75 11.75v0.5a6.25 6.25 0 0 0 12.5 0v-0.5M12 18.5V21"
              fill="none"
              stroke="currentColor"
              strokeWidth="1.8"
              strokeLinecap="round"
            ></path>
          </svg>
          <svg className="ico ico-stop" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            <rect x="7" y="7" width="10" height="10" rx="2.5" fill="currentColor"></rect>
          </svg>
        </button>
        {/* Uncontrolled: React renders no children here, so it never rewrites
            what is being typed. Only the attributes follow state, and React
            writes an attribute only when it changes — the same reason the
            original compares before it writes (`setAttr`). */}
        <div
          ref={msg}
          className={text.trim() ? "msg" : "msg blank"}
          id="msg"
          role="textbox"
          aria-multiline="true"
          autoCapitalize="none"
          autoCorrect="off"
          spellCheck={false}
          enterKeyHint={keyboard ? "send" : "enter"}
          data-placeholder={placeholder}
          aria-label={placeholder}
          contentEditable={on ? (PLAINTEXT_ONLY ? "plaintext-only" : "true") : "false"}
          suppressContentEditableWarning
          onInput={changed}
          onPaste={(e) => {
            const pasted = e.clipboardData?.getData("text/plain") || ""
            if (!pasted) return
            e.preventDefault()
            insertText(pasted)
            changed()
          }}
          onKeyDown={(e) => {
            // Not while an input method is mid-word: Return there accepts the candidate.
            if (e.nativeEvent.isComposing || e.keyCode === 229) return
            if (e.key !== "Enter" || e.shiftKey) return
            // On a touch screen Return is a new line and the button is how you send.
            if (!L.keyboard()) return
            e.preventDefault()
            void submit()
          }}
        ></div>
        <button
          ref={send}
          className="send"
          id="send"
          type="submit"
          disabled={!on || sending || voiceBusy || !text.trim()}
          title={keyboard ? T.webSendTip : ""}
          style={pinned}
          onMouseDown={(e) => e.preventDefault()}
        >
          {sending ? T.webSending : T.webSend}
        </button>
      </div>
      <input id="pick" type="file" accept="image/*" multiple hidden tabIndex={-1} />
      <div className="why" id="why" {...(whyHTML !== null ? { dangerouslySetInnerHTML: { __html: whyHTML } } : {})}>
        {whyHTML === null ? why : null}
      </div>
    </form>
  )
}

/**
 * `input/edges.js` `keyboardBar`: while the box has the focus, nothing else on
 * the page is in the tab order, so the `^ v` arrows iOS draws over the keyboard
 * cannot jump the writer out of their message. Every tab stop gets
 * `tabindex="-1"` on focus and its own value back on blur. The original repeats
 * the pass after each render; here that is a watch on the document's children
 * for as long as the focus stays, since other components render on their own.
 */
function useKeyboardBar(msg: RefObject<HTMLDivElement | null>) {
  useEffect(() => {
    const box = msg.current
    if (!box) return
    let moved: [Element, string | null][] | null = null
    let watch: MutationObserver | null = null
    const offstage = () => {
      moved = moved ? moved.filter(([el]) => document.contains(el)) : []
      for (const el of document.querySelectorAll("a[href], button, input, select, textarea, [tabindex]")) {
        if (el === box || el.getAttribute("tabindex") === "-1") continue
        moved.push([el, el.getAttribute("tabindex")])
        el.setAttribute("tabindex", "-1")
      }
    }
    const onstage = () => {
      watch?.disconnect()
      watch = null
      for (const [el, was] of moved ?? []) {
        if (was === null) el.removeAttribute("tabindex")
        else el.setAttribute("tabindex", was)
      }
      moved = null
    }
    const focus = () => {
      offstage()
      watch ??= new MutationObserver(() => {
        if (document.activeElement === box) offstage()
      })
      watch.observe(document.body, { childList: true, subtree: true })
    }
    box.addEventListener("focus", focus)
    box.addEventListener("blur", onstage)
    return () => {
      box.removeEventListener("focus", focus)
      box.removeEventListener("blur", onstage)
      onstage()
    }
  }, [msg])
}

/** Whether this browser takes `contenteditable="plaintext-only"`. The property throws where it does not. */
const PLAINTEXT_ONLY = (() => {
  const probe = document.createElement("div")
  try {
    probe.contentEditable = "plaintext-only"
  } catch {
    return false
  }
  return probe.contentEditable === "plaintext-only"
})()

