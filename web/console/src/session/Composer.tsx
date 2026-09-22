import { useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react"
import type { ReactNode, RefObject } from "react"
import type { AssistantSkill, SessionRow } from "@clawdline/contract"
import { RefusalError } from "@clawdline/core"
import { client } from "../client.js"
import * as L from "../legacy/bridge.js"
import {
  Shots,
  carriesFiles,
  carriesPicture,
  shotsHTML,
  shotsVersion,
  subscribeShots,
} from "../legacy/shots-bridge.js"
import * as V from "../legacy/voice-bridge.js"
import { COMPOSE_APPEND } from "../legacy/snippets-bridge.js"
import {
  clampSkillPickerIndex,
  filterSkills,
  heldSkills,
  loadSkills,
  selectedSkill,
  skillPrefix,
  skillQuery,
} from "../legacy/skills-bridge.js"
import { toast } from "../overlays/toast.js"
import { writeIsOff } from "./outcome.js"
import { deliverUntilSeen, pendingSends } from "./send.js"
import { Waiting } from "./Waiting.js"
import { ImageMarkup } from "./ImageMarkup.js"

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
 * - `.shots`, `input#pick` and `button.attach` are `input/shots.js`
 *   (`legacy/shots-bridge.ts`): a picture is picked, pasted or dropped on the
 *   detail pane, shrunk here, drawn as a thumbnail and sent beside the words
 *   as a `data:` URL. The listeners that file binds at its foot are bound
 *   below, on the same elements.
 * - `.voice` and `button.mic`: dictation, through `POST /v1/voice`. The row is
 *   built by `legacy/voice-bridge.ts` exactly as `input/voice.js` builds it —
 *   once per state, so a tick does not take the buttons out from under a thumb
 *   — and this component owns only the microphone's three attributes, the
 *   element the row is drawn into, and where the words land.
 * - `.skill-menu` is `SkillPicker` (`legacy/skills-bridge.ts`): typing `/`
 *   (or Codex's `$`) as the whole box asks `GET /v1/sessions/<id>/skills`
 *   once and draws the nine best matches; ↑↓ move, Tab or Return choose,
 *   Esc closes, and choosing writes the invocation into the box — it never
 *   sends.
 *
 * Two things differ from the original and are said here rather than hidden:
 * the original reports a failed send in a toast and the words are gone, and
 * here a message that did not go stays on its card at the end of the
 * transcript, saying so, with a way to send it again (`pending.ts`) — only a
 * failed quit line, which has no card, is written on the `.why` line; and the
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
  const [editingShot, setEditingShot] = useState<ReturnType<typeof Shots.shot>>(null)
  const sendWidth = useRef({ word: "", px: 0 })
  const pick = useRef<HTMLInputElement>(null)
  // `renderComposer` reads the pictures on every draw; a change to them is a draw.
  useSyncExternalStore(subscribeShots, shotsVersion)
  const shotsBusy = Shots.busy()
  const voiceRow = useRef<HTMLDivElement>(null)
  const form = useRef<HTMLFormElement>(null)
  // What the microphone is doing, which is the whole of what `renderComposer`
  // reads back from `Voice`: `voiceBusy` for the two waits, `voiceLive` for
  // the one control that must stay alive through them.
  const [voice, setVoice] = useState<V.VoiceState>(V.voiceState)
  // `SkillPicker`'s three fields: the rows drawn, the highlighted one, and
  // whether the menu is up. The catalog itself is held by the bridge.
  const [skills, setSkills] = useState<{ shown: boolean; matches: AssistantSkill[]; selected: number }>({
    shown: false,
    matches: [],
    selected: 0,
  })
  const skillMenu = useRef<HTMLDivElement>(null)
  // The session a catalog that arrives late is compared against.
  const rowRef = useRef(row)
  rowRef.current = row

  useKeyboardBar(msg)

  const on = write && !!row
  // The picture listeners are bound once, and ask what is open when they fire.
  const open = useRef({ on })
  open.current = { on }

  // A picture picked for one session is not a picture for the next one
  // (`session/open.js` clears them on every open and close).
  const openId = row?.id ?? null
  useEffect(() => {
    Shots.clear()
    setEditingShot(null)
    // The menu was about the session that was open; `session/open.js` closes it.
    setSkills({ shown: false, matches: [], selected: 0 })
  }, [openId])

  // `input/shots.js`, the foot of the file: a paste anywhere on the page, and
  // a drag onto the whole detail pane.
  useEffect(() => {
    const say = (text: string, bad: boolean) => toast(text, bad)
    // Paste, because copying a screenshot and pressing paste is how this is
    // done everywhere else. Not while the filter box has the focus.
    const paste = (ev: ClipboardEvent) => {
      if (!open.current.on) return
      if (document.activeElement === document.getElementById("filter")) return
      // The box runs first and takes anything with words in it; a paste
      // already spoken for is left alone.
      if (ev.defaultPrevented) return
      if (!carriesPicture(ev.clipboardData)) return
      ev.preventDefault()
      Shots.add(ev.clipboardData?.files, say)
    }
    // The whole pane is the target, and the document swallows the drops that
    // miss, because the browser's own answer to those is to leave the page.
    const over = (ev: DragEvent) => {
      if (carriesFiles(ev.dataTransfer)) ev.preventDefault()
    }
    const pane = document.getElementById("pane-detail")
    let depth = 0
    const enter = (ev: DragEvent) => {
      if (!carriesFiles(ev.dataTransfer) || !open.current.on) return
      depth += 1
      pane?.classList.add("dropping")
    }
    const leave = () => {
      depth = Math.max(0, depth - 1)
      if (!depth) pane?.classList.remove("dropping")
    }
    const drop = (ev: DragEvent) => {
      depth = 0
      pane?.classList.remove("dropping")
      if (!carriesFiles(ev.dataTransfer)) return
      ev.preventDefault()
      if (!open.current.on) {
        say(T.webShotNeedsSession, true)
        return
      }
      Shots.add(ev.dataTransfer?.files, say)
    }
    document.addEventListener("paste", paste)
    document.addEventListener("dragover", over)
    document.addEventListener("drop", over)
    pane?.addEventListener("dragenter", enter)
    pane?.addEventListener("dragleave", leave)
    pane?.addEventListener("drop", drop)
    return () => {
      document.removeEventListener("paste", paste)
      document.removeEventListener("dragover", over)
      document.removeEventListener("drop", over)
      pane?.removeEventListener("dragenter", enter)
      pane?.removeEventListener("dragleave", leave)
      pane?.removeEventListener("drop", drop)
      pane?.classList.remove("dropping")
    }
  }, [T])
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
    skillsChanged()
  }

  /** `SkillPicker.hide()`. */
  const hideSkills = () => setSkills((was) => (was.shown || was.matches.length ? { shown: false, matches: [], selected: 0 } : was))

  /**
   * `SkillPicker.changed()`: what the box says now decides whether the menu is
   * up. A catalog not yet held is asked for once, and the menu is drawn when it
   * arrives — if the box still asks for it and the same session is open.
   */
  const skillsChanged = () => {
    const at = rowRef.current
    const q = at ? skillQuery(rawText(), at.assistant) : null
    if (q === null || !at) {
      hideSkills()
      return
    }
    const held = heldSkills(at.id)
    if (held) {
      const matches = filterSkills(held, q)
      setSkills((was) =>
        matches.length
          ? { shown: true, matches, selected: clampSkillPickerIndex(was.selected, matches.length) }
          : { shown: false, matches: [], selected: 0 },
      )
      return
    }
    hideSkills()
    void loadSkills(at.id).then(() => {
      if (rowRef.current?.id === at.id) skillsChanged()
    })
  }

  /** `SkillPicker.move(delta)`: false when there is no menu to move in. */
  const moveSkill = (delta: number): boolean => {
    if (!skills.shown || !skills.matches.length) return false
    setSkills((was) => ({ ...was, selected: Math.max(0, Math.min(was.matches.length - 1, was.selected + delta)) }))
    return true
  }

  /**
   * `SkillPicker.accept()`: complete, do not execute. The box becomes the
   * invocation and a space, so the next Return sends the finished line through
   * the ordinary path — many skills take arguments.
   */
  const acceptSkill = (at = skills.selected): boolean => {
    const skill = skills.shown ? selectedSkill(skills.matches, at) : null
    const el = msg.current
    if (!skill || !el) return false
    el.textContent = skillPrefix(row?.assistant) + skill.name + " "
    setText(rawText())
    setFailure("")
    setSkills({ shown: false, matches: [], selected: 0 })
    caretToEnd()
    return true
  }

  // The highlighted row stays in view as the arrows walk past the menu's edge.
  useEffect(() => {
    const el = skillMenu.current?.children[skills.selected] as HTMLElement | undefined
    el?.scrollIntoView?.({ block: "nearest" })
  }, [skills.selected, skills.shown])

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

  // The other thing that writes into the box: a snippet somebody pressed
  // (`session/Snippets.tsx`). Same function dictation uses, through the same
  // `sink`, so the two cannot join their text to what is there by two rules —
  // and like dictation it stops here, because nothing but the send button sends.
  useEffect(() => {
    const appended = (event: Event) => {
      const text = (event as CustomEvent<{ text?: unknown }>).detail?.text
      if (typeof text === "string" && text) sink.current(text)
    }
    document.addEventListener(COMPOSE_APPEND, appended)
    return () => document.removeEventListener(COMPOSE_APPEND, appended)
  }, [])

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
    // A picture still shrinking is part of this message and has not arrived
    // yet; Return comes through here as well as the button.
    if (inFlight.current || Shots.busy()) return
    // With the menu up, Return and the button choose the highlighted skill.
    if (acceptSkill()) return
    const said = rawText().trim()
    const pictures = Shots.urls().slice()
    if ((!said && !pictures.length) || !row || !write) return
    // A quit line is not a message: the original ends the session instead, while
    // its terminal is still known. Exact and per assistant, so a sentence that
    // mentions `/exit` is still an ordinary prompt.
    const quit = !pictures.length && said === (row.assistant === "codex" ? "/quit" : "/exit")
    // **The box empties here, before the request exists**, and nothing puts the
    // words or the pictures back: a send that fails at this end may already
    // have been delivered. They move to a card at the end of the transcript
    // instead (`pending.ts`), which says it is sending, that the Mac has it, or
    // that it did not go — and then keeps them, with a way to send them again.
    if (msg.current) msg.current.textContent = ""
    if (document.activeElement === msg.current) caretToEnd()
    setText("")
    setFailure("")
    Shots.clear()
    inFlight.current = true
    setSending(true)
    try {
      if (quit) {
        await client.close(row.id)
        onDid()
        return
      }
      // A resolved send means the bytes reached the tty, not that the assistant
      // read them. The card says the first; the turn appearing in the
      // transcript, which takes the card's place, is the second — and is
      // enough on its own to free the box when the first never arrives.
      const code = await deliverUntilSeen(pendingSends.add(row.id, said, pictures, Date.now()))
      if (writeIsOff(code)) setWrite(false)
      if (!code) onDid()
    } catch (err) {
      const code = err instanceof RefusalError ? err.code : "unexpected_error"
      if (writeIsOff(code)) setWrite(false)
      // This used to build `webFailWithTag` by hand: the tag was the code and
      // the sentence was always `sendFailed`, so the half of
      // `core/failure-text.js` that chooses words by code was skipped and
      // `write_disabled`, `rate_limited` and `machine_offline` all read as
      // "送不出去". The formatter builds the same line and picks the sentence.
      setFailure(L.failureSentence(err, T.sendFailed))
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
      <div
        className="shots"
        id="shots"
        onClick={(ev) => {
          const target = ev.target as Element
          const drop = target.closest?.("[data-shot]")
          if (drop) {
            Shots.remove(drop.getAttribute("data-shot") ?? "")
            return
          }
          const preview = target.closest?.("[data-preview-shot]")
          if (preview) setEditingShot(Shots.shot(preview.getAttribute("data-preview-shot") ?? ""))
        }}
        dangerouslySetInnerHTML={{ __html: shotsHTML() }}
      ></div>
      {/* Empty here and built in `voice-bridge.ts`, as the original builds it
          in `voice.js`: React renders no children into this row, so it never
          diffs away the meter, the count or the two ways out. `opening` is the
          browser's own permission sheet, which is on top of the page and says
          more than this row could, so the row stays hidden for it. */}
      <div className="voice" id="voice" role="status" hidden={voice === "off" || voice === "opening"} ref={voiceRow}></div>
      {/* The original's label is English in every language; the catalog has no key for it. */}
      <div
        ref={skillMenu}
        className="skill-menu"
        id="skill-menu"
        role="listbox"
        aria-label={`${row?.assistant === "codex" ? "Codex" : "Claude Code"} skills`}
        hidden={!skills.shown}
      >
        {skills.shown &&
          skills.matches.map((skill, i) => (
            <button
              key={skill.name}
              type="button"
              className="skill-option"
              role="option"
              aria-selected={i === skills.selected ? "true" : "false"}
              // Keep the soft keyboard open: the click still arrives, only the focus stays.
              onMouseDown={(e) => e.preventDefault()}
              onClick={() => acceptSkill(i)}
            >
              <span className="command">{skillPrefix(row?.assistant) + skill.name}</span>
              <span className="description">{skill.description || ""}</span>
            </button>
          ))}
      </div>
      <div className="box">
        <button
          className="attach"
          id="attach"
          type="button"
          aria-label={T.webAttach}
          title={T.webAttach}
          disabled={!on || sending}
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => pick.current?.click()}
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
            // A picture goes to the document's handler; anything else with
            // words in it stays here.
            if (carriesPicture(e.clipboardData) || !pasted) return
            e.preventDefault()
            insertText(pasted)
            changed()
          }}
          onKeyDown={(e) => {
            // Not while an input method is mid-word: Return there accepts the candidate.
            if (e.nativeEvent.isComposing || e.keyCode === 229) return
            // The menu's keys, before anything else sees them (`SkillPicker`).
            if (e.key === "ArrowDown" && moveSkill(1)) return e.preventDefault()
            if (e.key === "ArrowUp" && moveSkill(-1)) return e.preventDefault()
            if (e.key === "Tab" && acceptSkill()) return e.preventDefault()
            if (e.key === "Escape" && skills.shown) {
              e.preventDefault()
              hideSkills()
              return
            }
            if (e.key !== "Enter" || e.shiftKey) return
            // Return chooses on every keyboard, a touch screen's included.
            if (acceptSkill()) return e.preventDefault()
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
          disabled={!on || sending || shotsBusy || voiceBusy || (!text.trim() && !Shots.count())}
          title={keyboard ? T.webSendTip : ""}
          style={pinned}
          onMouseDown={(e) => e.preventDefault()}
        >
          {sending ? T.webSending : T.webSend}
        </button>
      </div>
      <input
        ref={pick}
        id="pick"
        type="file"
        accept="image/*"
        multiple
        hidden
        tabIndex={-1}
        onChange={(e) => {
          Shots.add(e.currentTarget.files, (words, bad) => toast(words, bad))
          // Cleared so that picking the same file twice in a row still counts as a change.
          e.currentTarget.value = ""
        }}
      />
      <div className="why" id="why" {...(whyHTML !== null ? { dangerouslySetInnerHTML: { __html: whyHTML } } : {})}>
        {whyHTML === null ? why : null}
      </div>
      {editingShot ? (
        <ImageMarkup
          shot={editingShot}
          onCancel={() => setEditingShot(null)}
          onSave={(canvas) => Shots.replace(String(editingShot.id), canvas, (words, bad) => toast(words, bad))}
        />
      ) : null}
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
