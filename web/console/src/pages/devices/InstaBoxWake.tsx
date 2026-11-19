import { useEffect, useRef, useState, type FormEvent } from "react"
import { nextWord } from "../../next-strings.js"

const providerPath = "/v1/cloud-providers/instabox"
const wakePath = (machine: string) => `/v1/machines/${encodeURIComponent(machine)}/instabox-wake`

async function request<T>(origin: string, path: string, method = "GET", body?: object, signal?: AbortSignal): Promise<T> {
  const response = await fetch(new URL(path, origin), {
    method, credentials: "include", cache: "no-store", signal,
    ...(body ? { headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) } : {}),
  })
  const result = await response.json().catch(() => null)
  if (!response.ok || !result) throw new Error(typeof result?.error?.code === "string" ? result.error.code : "instabox_unavailable")
  return result
}

function reason(error: unknown) {
  switch (error instanceof Error ? error.message : "") {
    case "wake_cooldown": return nextWord("msInstaCooldown")
    case "wake_in_progress": return nextWord("msInstaInProgress")
    case "wake_unconfirmed": return nextWord("msInstaUnconfirmed")
    case "wake_manual_required": return nextWord("msInstaManualStart")
    case "wake_key_missing":
    case "wake_key_relink": return nextWord("msInstaKeyMissing")
    case "instabox_key_invalid":
    case "instabox_key_too_broad":
    case "instabox_key_refused": return nextWord("msInstaKeyRefused")
    case "instabox_machine_not_found": return nextWord("msInstaMachineGone")
    default: return nextWord("msInstaFailed")
  }
}

export function InstaBoxKey({ origin }: { origin: string }) {
  const [status, setStatus] = useState<{ configured: boolean; hint: string | null } | null>(null)
  const [key, setKey] = useState("")
  const [message, setMessage] = useState("")
  const [busy, setBusy] = useState(false)
  const pending = useRef(false)

  useEffect(() => {
    const read = new AbortController()
    void request<NonNullable<typeof status>>(origin, providerPath, "GET", undefined, read.signal)
      .then(setStatus, (error) => { if (!read.signal.aborted) setMessage(reason(error)) })
    return () => read.abort()
  }, [origin])

  async function save(event: FormEvent) {
    event.preventDefault()
    if (!key.trim()) return
    await change("PUT", { key: key.trim() })
  }

  async function change(method: "PUT" | "DELETE", data?: { key: string }) {
    if (pending.current) return
    pending.current = true
    setBusy(true)
    setMessage("")
    try {
      const next = await request<NonNullable<typeof status>>(origin, providerPath, method, data)
      setStatus(next)
      setKey("")
    } catch (error) { setMessage(reason(error)) }
    finally { pending.current = false; setBusy(false) }
  }

  return <details className="am-cloud-option">
    <summary><span>{nextWord("msInstaKey")}</span>{status?.hint && <span className="am-quiet">•••• {status.hint}</span>}</summary>
    <div className="am-stack">
      <p className="am-say">{nextWord("msInstaKeyHelp")}</p>
      <a href="https://dashboard.instabox.sh/dashboard/api-keys" target="_blank" rel="noopener noreferrer">{nextWord("msInstaKeyDashboard")}</a>
      <form className="am-cloud-enrollment" onSubmit={save}>
        <label>{nextWord("msInstaKey")}<input type="password" autoComplete="off" spellCheck={false} placeholder="ibk_…" value={key}
          disabled={busy} onChange={event => setKey(event.currentTarget.value)} /></label>
        <button type="submit" className="am-primary" disabled={busy || !key.trim()}>{nextWord("msInstaKeySave")}</button>
        {status?.configured && <button type="button" className="am-ghost" disabled={busy} onClick={() => void change("DELETE")}>{nextWord("msInstaKeyRemove")}</button>}
      </form>
      {message && <p className="am-say" role="status">{message}</p>}
    </div>
  </details>
}

type Binding = { configured: boolean; credential_ready: boolean }
type MachineChoice = { id: string; name: string }

export function InstaBoxWake({ origin, machine, online }: { origin: string; machine: string; online: boolean }) {
  const path = wakePath(machine)
  const [binding, setBinding] = useState<Binding | null>(null)
  const [choices, setChoices] = useState<MachineChoice[] | null>(null)
  const [selected, setSelected] = useState("")
  const [editing, setEditing] = useState(false)
  const [state, setState] = useState<"idle" | "checking" | "binding" | "waking" | "accepted">("idle")
  const [message, setMessage] = useState("")
  const pending = useRef(false)

  useEffect(() => {
    const read = new AbortController()
    void request<Binding>(origin, path, "GET", undefined, read.signal).then(setBinding, () => {})
    return () => read.abort()
  }, [origin, path])

  async function act(action: "checking" | "binding" | "waking") {
    if (!binding || pending.current) return
    pending.current = true
    setMessage("")
    setState(action)
    try {
      if (action === "checking") {
        const { machines } = await request<{ machines: MachineChoice[] }>(origin, `${providerPath}/machines`)
        setChoices(machines)
        setSelected(machines.length === 1 ? machines[0]!.id : "")
        setEditing(true)
      } else if (action === "binding") {
        await request(origin, path, "PUT", { provider_machine_id: selected })
        setBinding({ ...binding, configured: true })
        setEditing(false)
      } else {
        await request(origin, `${path}/wake`, "POST")
        setState("accepted")
        return
      }
      setState("idle")
    } catch (error) { setMessage(reason(error)); setState("idle") }
    finally { pending.current = false }
  }
  if (!binding?.credential_ready && !binding?.configured) return null
  const busy = state === "checking" || state === "binding" || state === "waking"
  return <div className="am-stack">
    {binding.configured && !online && !editing && <button type="button" className="am-primary am-wide"
      disabled={busy || !binding.credential_ready} aria-busy={state === "waking"} onClick={() => void act("waking")}>
      {nextWord(state === "waking" ? "msInstaWaking" : "msInstaWake")}
    </button>}
    {state === "accepted" && <p className="am-say" role="status">{nextWord(online ? "msInstaOnline" : "msInstaAccepted")}</p>}
    {binding.configured && !binding.credential_ready && <p className="am-say">{nextWord("msInstaKeyMissing")}</p>}
    {binding.credential_ready && !editing && <button type="button" className="am-ghost am-wide" disabled={busy}
      onClick={() => void act("checking")}>{nextWord(binding.configured ? "msInstaRelink" : "msInstaLink")}</button>}
    {editing && choices && <form className="am-cloud-enrollment" onSubmit={event => { event.preventDefault(); if (selected) void act("binding") }}>
      <label>{nextWord("msInstaSelectMachine")}<select value={selected} onChange={event => setSelected(event.currentTarget.value)}>
        <option value="">{nextWord("msInstaSelectMachine")}</option>
        {choices.map(choice => <option key={choice.id} value={choice.id}>{choice.name}</option>)}
      </select></label>
      <button type="submit" className="am-primary" disabled={!selected || busy}>{nextWord("msInstaLink")}</button>
    </form>}
    {message && <p className="am-say" role="status">{message}</p>}
  </div>
}
