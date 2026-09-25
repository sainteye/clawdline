import { useEffect, useId, useRef, useState, type TextareaHTMLAttributes } from "react"
import * as L from "../../legacy/bridge.js"
import * as V from "../../legacy/voice-bridge.js"
import { toast } from "../../overlays/toast.js"

/**
 * A Board or to-do text box with the composer's microphone in its lower right
 * corner. It records through the same one recorder the composer uses, so a
 * press here while the composer is listening stops that recording, as a
 * second press always has. What is heard is appended to the box and never
 * submitted: the person still reads it and presses the dialog's own button.
 *
 * **Its heading is `label`, never a `<label>` around it.** Done and Cancel are
 * drawn into the row above the box, so while it is listening a wrapping
 * `<label>`'s first control is Cancel. Done redraws the row, which takes the
 * button it was pressed on off the page before the click bubbles; the
 * `<label>` then reads the click as its own and forwards it to Cancel. The
 * recording was dropped before it was sent, in WebKit and Chromium alike, and
 * a tap on the running count or on the heading word did the same.
 */
export function VoiceTextarea({ value, onValue, disabled, label, ...rest }: {
  value: string
  onValue: (value: string) => void
  label?: string
} & Omit<TextareaHTMLAttributes<HTMLTextAreaElement>, "value" | "onChange">) {
  const row = useRef<HTMLDivElement>(null)
  const box = useRef<HTMLTextAreaElement>(null)
  const mine = useRef(false)
  const gone = useRef(false)
  const latest = useRef(value)
  latest.current = value
  const [state, setState] = useState<V.VoiceState>("off")
  const recording = mine.current && state === "recording"
  const heading = useId()

  useEffect(() => {
    gone.current = false
    return () => {
      gone.current = true
      // A dialog shut under an open microphone must not leave it listening.
      if (mine.current && V.busy()) V.cancel()
    }
  }, [])

  const press = () => {
    const host = row.current
    if (!host) return
    mine.current = true
    V.press({
      host,
      composer: null,
      guard: () => gone.current,
      say: toast,
      changed: (next) => {
        setState(next)
        if (next === "off") mine.current = false
      },
      sink: (said) => {
        const text = said.trim()
        if (!text) return
        const next = V.appendedText(latest.current, text)
        onValue(next)
        box.current?.focus({ preventScroll: true })
      },
    })
  }

  const words = L.strings
  const field = <div className="work-voice-field">
    <div className="voice work-voice-row" role="status" hidden ref={row}></div>
    <div className="work-voice-box">
      <textarea {...rest} ref={box} aria-labelledby={label ? heading : undefined} value={value} disabled={disabled} onChange={(event) => onValue(event.target.value)} />
      <button className="work-voice-mic" type="button" aria-pressed={recording ? "true" : "false"}
        aria-label={recording ? words.webVoiceStop : words.webVoiceStart} title={recording ? words.webVoiceStop : words.webVoiceStart}
        disabled={recording ? false : !!disabled}
        onMouseDown={(event) => event.preventDefault()} onClick={press}>
        {recording
          ? <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false"><rect x="7" y="7" width="10" height="10" rx="2.5" fill="currentColor" /></svg>
          : <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
            <rect x="9" y="3" width="6" height="11" rx="3" fill="currentColor" />
            <path d="M5.75 11.75v0.5a6.25 6.25 0 0 0 12.5 0v-0.5M12 18.5V21" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
          </svg>}
      </button>
    </div>
  </div>
  if (!label) return field
  // A tap on the heading still puts the caret in the box, as a <label> did.
  return <div className="work-voice-labelled">
    <span id={heading} onClick={() => box.current?.focus()}>{label}</span>
    {field}
  </div>
}
