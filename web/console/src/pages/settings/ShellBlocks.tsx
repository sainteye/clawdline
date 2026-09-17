import { useEffect, useMemo, useRef, useState } from "react"
import * as L from "../../legacy/bridge.js"
import { writeSettings } from "./api.js"
import {
  SHELL_RECORDING_EVENT,
  SHELL_STATE_EVENT,
  askShell,
  shellSettingsWords,
  type ShellRecording,
  type ShellSettingsState,
} from "./shell.js"

/**
 * The two settings the shell itself acts on, drawn only inside the shell.
 *
 * They are the General tab's first rows in the Swift app's native settings
 * window (`Settings.swift` `generalPane`): the hotkey, and where it is live.
 * That window is not a web page, so the console's settings page never had
 * them, and in a browser this renders nothing — the page stays the original's,
 * element for element. In the shell, "Settings…" opens this page, and these
 * are what that menu item used to lead to.
 *
 * They use the sheet's own furniture (`.block`, `.say`, `.row`, `.chip`,
 * `.said`), so no rule is added for them, and the native window's words, which
 * the shell supplies.
 *
 * The behaviour is the native window's:
 * - The hotkey chip records. The next key pressed with a modifier is the
 *   answer, Escape leaves it alone, and a bare letter is swallowed — the shell
 *   does the listening (`startRecording`), because only it can see the key
 *   before the page does.
 * - Every change applies at once: written, then the shell is told
 *   (`clawdlineConfigChanged`) and re-applies what it reads. What the chip then
 *   shows is what the shell says it registered, not what was asked for.
 * - "Every app" on empties `scope_app`; off restores the list that was there,
 *   and only a config that has always been global falls back to iTerm2
 *   (`globalScopeChanged`).
 */
export function ShellBlocks({ shown }: { shown: boolean }) {
  const words = useMemo(shellSettingsWords, [])
  const [state, setState] = useState<ShellSettingsState | null>(null)
  const [recording, setRecording] = useState(false)
  const [pending, setPending] = useState<string | null>(null)
  const [said, setSaid] = useState("")
  const [scopeSaid, setScopeSaid] = useState("")
  const [saving, setSaving] = useState(false)
  const remembered = useRef("")
  const recordingRef = useRef(false)
  recordingRef.current = recording

  useEffect(() => {
    if (!words) return
    const onState = (ev: Event) => {
      const next = (ev as CustomEvent<ShellSettingsState>).detail
      if (!next) return
      if (next.scopeApp) remembered.current = next.scopeApp
      setState(next)
      setPending(null)
    }
    const onRecording = (ev: Event) => {
      const answer = (ev as CustomEvent<ShellRecording>).detail || {}
      setRecording(false)
      if (!answer.spec) return
      setPending(answer.display || answer.spec)
      setSaid("")
      writeSettings({ hotkey: answer.spec }).then(
        () => {
          // No shell to re-apply it means no reading will replace this one.
          if (!askShell("changed")) setPending(null)
        },
        (error: unknown) => {
          // The shell let the old combination go while listening; it comes back.
          askShell("stopRecording")
          setPending(null)
          setSaid(L.failureSentence(error, L.strings.webRequestFailed))
        },
      )
    }
    window.addEventListener(SHELL_STATE_EVENT, onState)
    window.addEventListener(SHELL_RECORDING_EVENT, onRecording)
    return () => {
      window.removeEventListener(SHELL_STATE_EVENT, onState)
      window.removeEventListener(SHELL_RECORDING_EVENT, onRecording)
    }
  }, [words])

  // A reading on every arrival, as the native window refreshes when shown; and
  // a recording does not outlive the page it was started from.
  useEffect(() => {
    if (!words) return
    if (shown) askShell("state")
    else if (recordingRef.current) {
      askShell("stopRecording")
      setRecording(false)
    }
  }, [shown, words])

  if (!words) return null

  const record = () => {
    if (recording) {
      askShell("stopRecording")
      setRecording(false)
      return
    }
    setSaid("")
    if (askShell("record")) setRecording(true)
  }

  const global = state ? state.scopeApp === "" : false
  const scope = state && !global ? state.scopeApp.split(",").map((id) => id.trim()).filter(Boolean) : []

  const toggleGlobal = () => {
    if (!state || saving) return
    const next = global ? remembered.current || "com.googlecode.iterm2" : ""
    setSaving(true)
    setScopeSaid("")
    writeSettings({ scope_app: next })
      .then(
        () => {
          askShell("changed")
        },
        (error: unknown) => setScopeSaid(L.failureSentence(error, L.strings.webRequestFailed)),
      )
      .finally(() => setSaving(false))
  }

  const hotkeyText = recording
    ? words.recording
    : pending ?? (state ? (state.hotkey ? state.display : words.off) : "")

  return (
    <>
      <div className="block" id="settings-hotkey-block">
        <b id="settings-hotkey-title">{words.hotkey}</b>
        <div className="row">
          <button
            className={recording ? "chip on" : "chip"}
            id="settings-hotkey"
            type="button"
            aria-pressed={recording}
            disabled={pending !== null}
            onClick={record}
          >
            {hotkeyText}
          </button>
        </div>
        <p className="said" id="settings-hotkey-said" role="status" aria-live="polite">
          {said || state?.failure || ""}
        </p>
      </div>

      <div className="block" id="settings-scope-block">
        <b id="settings-scope-title">{words.scope}</b>
        {scope.length > 0 && (
          <p className="say" id="settings-scope-say">
            {scope.map((id, i) => (
              <span key={id}>
                {i > 0 && " "}
                <code>{id}</code>
              </span>
            ))}
          </p>
        )}
        <div className="row">
          <button
            className={global ? "chip on" : "chip"}
            id="settings-scope-global"
            type="button"
            aria-pressed={global}
            disabled={!state || saving}
            onClick={toggleGlobal}
          >
            {words.scopeGlobal}
          </button>
        </div>
        <p className="said" id="settings-scope-said" role="status" aria-live="polite">
          {scopeSaid}
        </p>
      </div>
    </>
  )
}
