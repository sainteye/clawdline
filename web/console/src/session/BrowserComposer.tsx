import { useEffect, useImperativeHandle, useRef, useState, type Ref, type ChangeEvent, type KeyboardEvent } from "react"
import { flushSync } from "react-dom"
import { nextWord } from "../next-strings.js"
import { BROWSER_INPUT_BYTES, type InputReceipt } from "./browser-keyboard.js"

export type BrowserKeyboardHandle = { open: (kind?: string, ready?: Promise<boolean>) => void; close: () => void }
/** The focused page field as the person's own route reads it. A secret field
 * answers only kind "password"; an unknown focus answers kind null. */
export type PageField = { kind: string | null; value?: string; start?: number | null; end?: number | null; truncated?: boolean }

const SHOWN = 200
const shown = (text: string) => text.length > SHOWN ? text.slice(0, SHOWN) + "…" : text
const readable = (field: PageField | null) => field && field.kind && field.kind !== "password" && typeof field.value === "string" ? field : null

/** A local draft lives outside the sampled picture. No keystroke changes the
 * remote page until the person sends it; refreshes never replace this editor. */
export function BrowserComposer({ keyboardRef, person, available, onPrepare, onSend, onDelete, onReadField }: {
  keyboardRef: Ref<BrowserKeyboardHandle>; person: boolean; available: boolean;
  onPrepare: () => Promise<boolean>; onSend: (text: string, replace: boolean) => Promise<InputReceipt>; onDelete: () => void;
  onReadField: () => Promise<PageField | null>;
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
  // A short "Sent", or "Checked" when the field was read back and matched.
  const [sent, setSent] = useState<false | "sent" | "checked">(false)
  // The field differed from what was sent; the person chooses what wins.
  const [mismatch, setMismatch] = useState<{ sent: string; now: string } | null>(null)
  // The tapped field's existing text, which the person may pull into the draft.
  const [pageField, setPageField] = useState<string | null>(null)
  const [replaceMode, setReplaceMode] = useState(false)
  const sentTimer = useRef(0)
  const close = () => {
    epoch.current++; targetReady.current = null; sending.current = false; composing.current = false
    editor.current?.blur(); setDraft(""); setPending(false); setError(null); setSent(false); window.clearTimeout(sentTimer.current)
    setMismatch(null); setPageField(null); setReplaceMode(false)
  }
  useImperativeHandle(keyboardRef, () => ({
    open: (next, ready) => {
      if (ready) {
        epoch.current++; targetReady.current = ready
        const current = epoch.current
        flushSync(() => { setKind(next || "text"); setDraft(""); setError(null); setMismatch(null); setPageField(null); setReplaceMode(false) })
        if (next !== "password") void ready.then(async ok => {
          if (!ok || epoch.current !== current) return
          const field = readable(await read())
          if (epoch.current === current) setPageField(field?.value ? field.value : null)
        })
      }
      editor.current?.focus({ preventScroll: true })
    },
    close,
  }))
  useEffect(() => { if (!person) close() }, [person])
  useEffect(() => () => { epoch.current++; window.clearTimeout(sentTimer.current) }, [])

  const read = () => onReadField().catch(() => null)
  const secret = () => kind === "password"
  // Sends, then reads the field back. Insertion is expected at the selection
  // read just before; a replacement is expected to be the whole field. With
  // no comparable reading (a secret, an unknown focus, contenteditable) the
  // result is only "Sent". A difference is shown, never corrected unasked.
  const deliver = async (text: string, replace: boolean, keepDraft: boolean) => {
    if (sending.current || !available) return
    const current = epoch.current
    sending.current = true; setPending(true); setError(null); setSent(false); setMismatch(null)
    if (!keepDraft) setDraft("")
    try {
      if (!await (targetReady.current ?? onPrepare())) throw new Error("takeover not confirmed")
      if (epoch.current !== current) return
      const before = replace || secret() ? null : readable(await read())
      if (epoch.current !== current) return
      if (!(await onSend(text, replace)).ok) throw new Error("input not confirmed")
      if (epoch.current !== current) return
      setReplaceMode(false)
      const expected = replace ? text
        : before && typeof before.start === "number" && typeof before.end === "number" ? before.value!.slice(0, before.start) + text + before.value!.slice(before.end) : null
      const after = expected === null ? null : readable(await read())
      if (epoch.current !== current) return
      if (after) setPageField(after.value ? after.value : null)
      if (expected !== null && after && after.value !== expected) setMismatch({ sent: text, now: after.value! })
      else {
        setSent(expected !== null && after ? "checked" : "sent"); window.clearTimeout(sentTimer.current)
        sentTimer.current = window.setTimeout(() => setSent(false), 1500)
      }
    } catch {
      if (epoch.current === current && !keepDraft) {
        // Keep the draft for inspection. An uncertain insertion is never retried.
        setDraft(value => text + value); setError("unconfirmed")
      } else if (epoch.current === current) setError("unconfirmed")
    } finally {
      if (epoch.current === current) { sending.current = false; setPending(false) }
    }
  }
  const send = async () => {
    if (!draft || composing.current) return
    if (new TextEncoder().encode(draft).length > BROWSER_INPUT_BYTES) { setError("full"); return }
    await deliver(draft, replaceMode, false)
  }
  const pull = () => {
    if (pageField === null) return
    setDraft(pageField); setReplaceMode(true); setMismatch(null)
    editor.current?.focus({ preventScroll: true })
  }
  const props = {
    id: "browser-native-keyboard", className: "browser-composer-input", value: draft,
    maxLength: BROWSER_INPUT_BYTES, autoComplete: "off", autoCorrect: "off", autoCapitalize: "off", spellCheck: false,
    "aria-label": nextWord("browserPanelNativeInput"), placeholder: nextWord("browserPanelDraftHint"),
    enterKeyHint: "send" as const,
    onChange: (event: ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => setDraft(event.currentTarget.value),
    onCompositionStart: () => { composing.current = true }, onCompositionEnd: () => { composing.current = false },
    onKeyDown: (event: KeyboardEvent<HTMLInputElement | HTMLTextAreaElement>) => {
      event.stopPropagation()
      // An empty draft has nothing left to delete here, so the keyboard's
      // Backspace continues into the page field, exactly like Delete.
      if (!event.nativeEvent.isComposing && !composing.current && event.key === "Backspace" && !draft && person && available) {
        event.preventDefault(); onDelete(); return
      }
      // Enter sends, as in the chat composer; Shift+Enter keeps a new line
      // for a multi-line page field. An IME's confirming Enter never sends.
      if (!event.nativeEvent.isComposing && !composing.current && event.key === "Enter" && !event.shiftKey) {
        event.preventDefault(); void send()
      }
    },
  }
  const keep = (event: { preventDefault: () => void }) => event.preventDefault()
  return <form className="browser-composer" onSubmit={event => { event.preventDefault(); void send() }}>
    {mismatch && <div className="browser-check-card" role="alert">
      <b>{nextWord("browserPanelMismatchTitle")}</b>
      <span>{nextWord("browserPanelMismatchSent", { text: shown(mismatch.sent) })}</span>
      <span>{nextWord("browserPanelMismatchNow", { text: shown(mismatch.now) })}</span>
      <div className="browser-check-actions">
        <button type="button" className="browser-check-replace" onPointerDown={keep} disabled={pending || !available} onClick={() => void deliver(mismatch.sent, true, true)}>{nextWord("browserPanelReplaceField")}</button>
        <button type="button" className="browser-check-keep" onPointerDown={keep} onClick={() => setMismatch(null)}>{nextWord("browserPanelKeepPage")}</button>
      </div>
    </div>}
    {!mismatch && !replaceMode && pageField !== null && <div className="browser-field-strip">
      <span className="browser-field-label">{nextWord("browserPanelFieldNow")}</span>
      <span className="browser-field-value">{shown(pageField)}</span>
      <button type="button" className="browser-field-pull" onPointerDown={keep} onClick={pull}>{nextWord("browserPanelPullField")}</button>
    </div>}
    {replaceMode && <div className="browser-replace-mode">
      <span>{nextWord("browserPanelReplaceMode")}</span>
      <button type="button" onPointerDown={keep} onClick={() => setReplaceMode(false)}>{nextWord("browserPanelReplaceCancel")}</button>
    </div>}
    <div className="browser-composer-row">
      {kind === "password"
        ? <input {...props} type="password" ref={element => { editor.current = element }} />
        : <textarea {...props} rows={1} ref={element => { editor.current = element }} />}
      <button type="submit" className={sent ? "browser-composer-send browser-composer-sent" : "browser-composer-send"} aria-busy={pending ? "true" : undefined} onPointerDown={event => event.preventDefault()} disabled={!draft || pending || !available}>
        {pending ? <><span className="browser-send-spinner" aria-hidden="true" />{nextWord("browserPanelSending")}</> : sent ? <>✓ {nextWord(sent === "checked" ? "browserPanelChecked" : "browserPanelSent")}</> : nextWord("browserPanelSendText")}
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
