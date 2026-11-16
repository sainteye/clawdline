import { useEffect, useId, useRef, useState } from "react"
import { catalogWord } from "../catalog.js"
import { mayWriteThroughCurrentTransport } from "../client.js"
import { captureMachineFetch } from "../cloud/install.js"
import { Switch } from "../pages/settings/Switch.js"
import "./tool-suggestions-setting.css"

const word = (key: string) => catalogWord("integrationSettings", key)
const setting = "tool_library_suggestions_enabled"

/** Both entry points write the same machine setting through a captured transport. */
export function ToolSuggestionsSetting() {
  const id = useId()
  const connection = useRef(captureMachineFetch())
  const [on, setOn] = useState<boolean | null>(null)
  const [busy, setBusy] = useState(true)
  const [said, setSaid] = useState("")
  const [retry, setRetry] = useState(0)
  const operation = useRef<AbortController | null>(null)
  const readOnly = !mayWriteThroughCurrentTransport()

  async function request(signal: AbortSignal, enabled?: boolean) {
    const target = connection.current
    const response = await target.fetch("/v1/settings/integrations", { signal, cache: "no-store", ...(enabled === undefined ? {} : {
      method: "POST", headers: { "Content-Type": "application/json", "Idempotency-Key": crypto.randomUUID() },
      body: JSON.stringify({ [setting]: enabled }),
    }) })
    const answer = await response.json()
    if (!target.current() || !response.ok) throw new Error("integration_unavailable")
    return typeof answer?.[setting] === "boolean" ? answer[setting] as boolean : null
  }
  useEffect(() => {
    const abort = new AbortController()
    operation.current = abort
    setOn(null); setBusy(true); setSaid("")
    void request(abort.signal).then(value => {
      if (!abort.signal.aborted) { setOn(value); if (value === null) setSaid(word("unavailable")) }
    }, () => { if (!abort.signal.aborted) setSaid(word("readFailed")) })
      .finally(() => { if (!abort.signal.aborted) setBusy(false) })
    return () => operation.current?.abort()
  }, [retry])

  async function toggle() {
    if (on === null || busy || readOnly || !connection.current.current()) return
    const abort = new AbortController()
    operation.current = abort
    setBusy(true); setSaid("")
    try {
      const value = await request(abort.signal, !on)
      if (!abort.signal.aborted) { setOn(value); setSaid(word(value === null ? "unavailable" : "saved")) }
    } catch { if (!abort.signal.aborted) setSaid(word("saveFailed")) }
    finally { if (!abort.signal.aborted) setBusy(false) }
  }
  return <section className="tool-suggestions-setting" aria-busy={busy}>
    <div className="tool-suggestions-row">
      <strong id={id + "-title"}>{word("tool_library_suggestions_enabledTitle")}</strong>
      <Switch labelledBy={id + "-title"} describedBy={id + "-description"} on={on === true}
        disabled={busy || on === null || readOnly} stateText={word(busy ? "loading" : on === null ? "unavailable" : on ? "on" : "off")}
        onToggle={() => void toggle()} />
    </div>
    <p id={id + "-description"}>{word("tool_library_suggestions_enabledDescription")}</p>
    <p>{word("tool_library_suggestions_enabledScope")}</p>
    {said && <p role="status" aria-live="polite">{said}</p>}
    {!busy && on === null && <button type="button" onClick={() => setRetry(n => n + 1)}>{word("retry")}</button>}
  </section>
}
