import { useEffect, useImperativeHandle, useRef, useState, type Ref, type ChangeEvent, type KeyboardEvent } from "react"
import { flushSync } from "react-dom"
import { nextWord } from "../next-strings.js"
import { BROWSER_INPUT_BYTES, type InputReceipt } from "./browser-keyboard.js"

export type BrowserKeyboardHandle = { open: (kind?: string, ready?: Promise<boolean>) => void; close: () => void }

/** A local draft lives outside the sampled picture. No keystroke changes the
 * remote page until the person sends it; refreshes never replace this editor. */
export function BrowserComposer({ keyboardRef, person, available, onPrepare, onSend, onDelete }: {
  keyboardRef: Ref<BrowserKeyboardHandle>; person: boolean; available: boolean;
  onPrepare: () => Promise<boolean>; onSend: (text: string) => Promise<InputReceipt>; onDelete: () => void;
}) {
  const editor = useRef<HTMLInputElement | HTMLTextAreaElement | null>(null)
  const epoch = useRef(0)
  const sending = useRef(false)
  const composing = useRef(false)
  const targetReady = useRef<Promise<boolean> | null>(null)
  const [kind, setKind] = useState("text")
  const [draft, setDraft] = useState("")
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<"full" | "unconfirmed" | null>(null)
  // A short "Sent" on the button after the page acknowledged the text.
  const [sent, setSent] = useState(false)
  const sentTimer = useRef(0)
  const close = () => {
    epoch.current++; targetReady.current = null; sending.current = false; composing.current = false
    editor.current?.blur(); setDraft(""); setPending(false); setError(null); setSent(false); window.clearTimeout(sentTimer.current)
  }
  useImperativeHandle(keyboardRef, () => ({
    open: (next, ready) => {
      if (ready) {
        epoch.current++; targetReady.current = ready
        flushSync(() => { setKind(next || "text"); setDraft(""); setError(null) })
      }
      editor.current?.focus({ preventScroll: true })
    },
    close,
  }))
  useEffect(() => { if (!person) close() }, [person])
  useEffect(() => () => { epoch.current++; window.clearTimeout(sentTimer.current) }, [])

  const send = async () => {
    if (!draft || sending.current || composing.current || !available) return
    if (new TextEncoder().encode(draft).length > BROWSER_INPUT_BYTES) { setError("full"); return }
    const text = draft, current = epoch.current
    sending.current = true; setPending(true); setError(null); setSent(false); setDraft("")
    try {
      if (!await (targetReady.current ?? onPrepare())) throw new Error("takeover not confirmed")
      if (epoch.current !== current) return
      if (!(await onSend(text)).ok) throw new Error("input not confirmed")
      if (epoch.current === current) {
        setSent(true); window.clearTimeout(sentTimer.current)
        sentTimer.current = window.setTimeout(() => setSent(false), 1500)
      }
    } catch {
      if (epoch.current === current) {
        // Keep the draft for inspection. An uncertain insertion is never retried.
        setDraft(value => text + value); setError("unconfirmed")
      }
    } finally {
      if (epoch.current === current) { sending.current = false; setPending(false) }
    }
  }
  const props = {
    id: "browser-native-keyboard", className: "browser-composer-input", value: draft,
    maxLength: BROWSER_INPUT_BYTES, autoComplete: "off", autoCorrect: "off", autoCapitalize: "off", spellCheck: false,
    "aria-label": nextWord("browserPanelNativeInput"), placeholder: nextWord("browserPanelDraftHint"),
    onChange: (event: ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => setDraft(event.currentTarget.value),
    onCompositionStart: () => { composing.current = true }, onCompositionEnd: () => { composing.current = false },
    onKeyDown: (event: KeyboardEvent<HTMLInputElement | HTMLTextAreaElement>) => {
      event.stopPropagation()
      // An empty draft has nothing left to delete here, so the keyboard's
      // Backspace continues into the page field, exactly like Delete.
      if (!event.nativeEvent.isComposing && !composing.current && event.key === "Backspace" && !draft && person && available) {
        event.preventDefault(); onDelete(); return
      }
      if (!event.nativeEvent.isComposing && !composing.current && event.key === "Enter" && (event.ctrlKey || event.metaKey)) {
        event.preventDefault(); void send()
      }
    },
  }
  return <form className="browser-composer" onSubmit={event => { event.preventDefault(); void send() }}>
    <div className="browser-composer-row">
      {kind === "password"
        ? <input {...props} type="password" ref={element => { editor.current = element }} />
        : <textarea {...props} rows={1} ref={element => { editor.current = element }} />}
      <button type="submit" className={sent ? "browser-composer-sent" : undefined} aria-busy={pending ? "true" : undefined} onPointerDown={event => event.preventDefault()} disabled={!draft || pending || !available}>
        {pending ? <><span className="browser-send-spinner" aria-hidden="true" />{nextWord("browserPanelSending")}</> : sent ? <>✓ {nextWord("browserPanelSent")}</> : nextWord("browserPanelSendText")}
      </button>
      {/* One Backspace on the page field. Like Send text it never takes focus,
          so the phone keyboard stays up; the local draft is edited by the
          keyboard's own Backspace. */}
      <button type="button" className="browser-composer-delete" onPointerDown={event => event.preventDefault()} disabled={!person || pending || !available}
        aria-label={nextWord("browserPanelKeyBackspace")} onClick={onDelete}>⌫</button>
    </div>
    {pending && <span className="browser-input-pending" role="status">{nextWord("browserPanelSending")}</span>}
    {error && <p className="browser-input-error" role="alert">{nextWord(error === "full" ? "browserPanelInputFull" : "browserPanelInputUnconfirmed")}</p>}
  </form>
}
