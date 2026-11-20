import { useEffect, useRef, useState, type FormEvent } from "react"
import { nextWord } from "../../next-strings.js"

import {
  instaBoxProviderPath, instaBoxRequest, instaBoxWakePath, instaBoxWakeReason,
  readInstaBoxBinding, wakeInstaBox, type InstaBoxBinding,
} from "../../cloud/instabox-wake.js"

export function InstaBoxKey({ origin, onChange }: { origin: string; onChange?: () => void }) {
  const [status, setStatus] = useState<{ configured: boolean; hint: string | null } | null>(null)
  const [key, setKey] = useState("")
  const [message, setMessage] = useState("")
  const [busy, setBusy] = useState(false)
  const pending = useRef(false)

  useEffect(() => {
    const read = new AbortController()
    void instaBoxRequest<NonNullable<typeof status>>(origin, instaBoxProviderPath, "GET", undefined, read.signal)
      .then(setStatus, (error) => { if (!read.signal.aborted) setMessage(instaBoxWakeReason(error)) })
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
      const next = await instaBoxRequest<NonNullable<typeof status>>(origin, instaBoxProviderPath, method, data)
      setStatus(next)
      setKey("")
      onChange?.()
    } catch (error) { setMessage(instaBoxWakeReason(error)) }
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

type MachineChoice = { id: string; name: string }

export function InstaBoxWake({ origin, machine, online, linkOnly = false }: {
  origin: string
  machine: string
  online: boolean
  linkOnly?: boolean
}) {
  const path = instaBoxWakePath(machine)
  const [binding, setBinding] = useState<InstaBoxBinding | null>(null)
  const [choices, setChoices] = useState<MachineChoice[] | null>(null)
  const [selected, setSelected] = useState("")
  const [editing, setEditing] = useState(false)
  const [state, setState] = useState<"idle" | "checking" | "binding" | "waking" | "accepted" | "schedule-webhooks">("idle")
  const [message, setMessage] = useState("")
  const pending = useRef(false)

  useEffect(() => {
    const read = new AbortController()
    void readInstaBoxBinding(origin, machine, read.signal).then(setBinding, () => {})
    return () => read.abort()
  }, [origin, machine, online])

  async function act(action: "checking" | "binding" | "waking") {
    if (!binding || pending.current) return
    pending.current = true
    setMessage("")
    setState(action)
    try {
      if (action === "checking") {
        const { machines } = await instaBoxRequest<{ machines: MachineChoice[] }>(origin, `${instaBoxProviderPath}/machines`)
        setChoices(machines)
        setSelected(machines.length === 1 ? machines[0]!.id : "")
        setEditing(true)
      } else if (action === "binding") {
        await instaBoxRequest(origin, path, "PUT", { provider_machine_id: selected })
        setBinding({ ...binding, configured: true, schedule_webhook_wake_enabled: binding.schedule_webhook_wake_enabled === undefined ? undefined : false, schedule_webhook_wake_status: null })
        setEditing(false)
      } else {
        await wakeInstaBox(origin, machine)
        setState("accepted")
        return
      }
      setState("idle")
    } catch (error) { setMessage(instaBoxWakeReason(error)); setState("idle") }
    finally { pending.current = false }
  }
  async function setScheduleWebhookWake(enabled: boolean) {
    if (!binding || pending.current) return
    pending.current = true
    setMessage("")
    setState("schedule-webhooks")
    try {
      const settings = await instaBoxRequest<Pick<InstaBoxBinding, "schedule_webhook_wake_enabled" | "schedule_webhook_wake_status">>(
        origin, `${path}/schedule-webhooks`, "PUT", { enabled },
      )
      setBinding({ ...binding, ...settings })
    } catch (error) { setMessage(instaBoxWakeReason(error)) }
    finally { pending.current = false; setState("idle") }
  }
  if (!binding?.credential_ready && !binding?.configured) return null
  const busy = state === "checking" || state === "binding" || state === "waking" || state === "schedule-webhooks"
  return <div className="am-stack">
    {!linkOnly && binding.configured && !online && !editing && <button type="button" className="am-primary am-wide"
      disabled={busy || !binding.credential_ready} aria-busy={state === "waking"} onClick={() => void act("waking")}>
      {nextWord(state === "waking" ? "msInstaWaking" : "msInstaWake")}
    </button>}
    {!linkOnly && binding.configured && typeof binding.schedule_webhook_wake_enabled === "boolean" && <div className="am-stack">
      <label><input type="checkbox" aria-label={nextWord("msInstaScheduleWebhookWake")} checked={binding.schedule_webhook_wake_enabled} disabled={busy || (!binding.credential_ready && !binding.schedule_webhook_wake_enabled)}
        onChange={event => void setScheduleWebhookWake(event.currentTarget.checked)} /> {nextWord("msInstaScheduleWebhookWake")}</label>
      <p className="am-say">{nextWord("msInstaScheduleWebhookWakeHelp")}</p>
      {binding.schedule_webhook_wake_enabled && binding.schedule_webhook_wake_status && !["provider_running", "wake_accepted", "wake_changed"].includes(binding.schedule_webhook_wake_status) &&
        <p className="am-say" role="status">{nextWord("msInstaScheduleWebhookWakeIssue")} {instaBoxWakeReason(new Error(binding.schedule_webhook_wake_status))}</p>}
    </div>}
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
