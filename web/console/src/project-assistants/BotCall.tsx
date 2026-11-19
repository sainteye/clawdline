import { useLayoutEffect, useRef, useState } from "react"
import { busy as dictating } from "../legacy/voice-bridge"
import { BotCall, type CallPorts, type CallState } from "./bot-call"
import { botCallAudioAvailable, browserCallAudio } from "./bot-call-audio"
import { botCallWords } from "./bot-call-words"
import "./bot-call.css"

export function useBotCall(ports: Pick<CallPorts, "current" | "submit">) {
  const [state, setState] = useState<CallState>({ phase: "idle", muted: false })
  const latest = useRef(ports)
  const [call] = useState(() => new BotCall({
    current: () => latest.current.current(),
    submit: input => latest.current.submit(input),
    changed: setState,
  }))
  useLayoutEffect(() => {
    latest.current = ports
    call.observe()
  }, [call, ports])
  useLayoutEffect(() => {
    const disconnect = () => { if (call.active) call.end("disconnected") }
    const hide = () => { if (document.hidden) disconnect() }
    window.addEventListener("offline", disconnect)
    window.addEventListener("pagehide", disconnect)
    document.addEventListener("visibilitychange", hide)
    return () => {
      window.removeEventListener("offline", disconnect)
      window.removeEventListener("pagehide", disconnect)
      document.removeEventListener("visibilitychange", hide)
      call.end()
    }
  }, [call])
  return { call, state }
}

export function BotCallControls({call, state, enabled, name, language}: {
  call: BotCall; state: CallState; enabled: boolean; name: string; language: string
}) {
  const w = botCallWords(language)
  const supported = botCallAudioAvailable()
  const label = `${w.start} · ${name}`
  if (!call.active) return <div className="pa-bot-call-start">
    {state.phase !== "idle" && <span role="status">{w[state.phase]}</span>}
    <button type="button" className="pa-pill" disabled={!enabled || !supported} aria-label={label}
      title={supported ? label : w.unsupported}
      onClick={() => { if (dictating()) call.end("microphone"); else void call.start(browserCallAudio(language)) }}>
      <CallIcon/>{w.start}
    </button>
  </div>
  const phase = state.muted && state.phase === "listening" ? "muted" : state.phase
  return <section className="pa-bot-call" aria-label={label}>
    <span className="pa-bot-call-status" role="status"><strong>{name}</strong> {phase !== "idle" && w[phase]}</span>
    {state.phase === "speaking" && <button type="button" className="pa-pill" onClick={() => call.interrupt()}>{w.interrupt}</button>}
    <button type="button" className="pa-pill" aria-pressed={state.muted} onClick={() => call.mute()}>{w.mute}</button>
    <button type="button" className="pa-pill pa-bot-call-end" onClick={() => call.end()}>{w.end}</button>
  </section>
}

function CallIcon() {
  return <svg viewBox="0 0 16 16" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.5"
    strokeLinecap="round" aria-hidden="true"><path d="M2 7v2m3-4v6m3-9v12m3-9v6m3-4v2"/></svg>
}
