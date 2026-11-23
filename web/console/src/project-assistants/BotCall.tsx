import { useEffect, useLayoutEffect, useRef, useState } from "react"
import { busy as dictating } from "../legacy/voice-bridge"
import { callRingback } from "../realtime/copilot-meter"
import { BotCall, type CallPorts, type CallState } from "./bot-call"
import { botCallAudioAvailable, browserCallAudio, browserCallAudioReady } from "./bot-call-audio"
import type { useBotVoice } from "./BotVoicePanel"
import { botCallWords } from "./bot-call-words"
import "./bot-call.css"

export function useBotCall(ports: Pick<CallPorts, "current" | "submit">) {
  const [state, setState] = useState<CallState>({ phase: "idle", muted: false })
  const latest = useRef(ports)
  const [call] = useState(() => new BotCall({
    current: () => latest.current.current(),
    submit: input => latest.current.submit(input),
    changed: setState,
    ringback: () => callRingback(),
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

export function BotCallControls({call, state, enabled, name, language, greeting=false, shortcut=0, realtime}: {
  call: BotCall; state: CallState; enabled: boolean; name: string; language: string; greeting?: boolean; shortcut?:number; realtime?:ReturnType<typeof useBotVoice>
}) {
  const w = botCallWords(language)
  const [readyLanguage,setReadyLanguage] = useState<string|null>(null)
  useEffect(() => {
    if (!botCallAudioAvailable()) return
    let current = true
    const update = () => void browserCallAudioReady(language).then(ready => {
      if (current) setReadyLanguage(ready ? language : null)
    })
    update()
    window.speechSynthesis.addEventListener("voiceschanged", update)
    return () => {
      current = false
      window.speechSynthesis.removeEventListener("voiceschanged", update)
    }
  }, [language])
  const canStart = !!realtime || (enabled && readyLanguage === language)
  const label = `${w.start} · ${name}`
  const started = useRef(0)
  const start = () => {
    if (dictating()) call.end("microphone")
    else if (realtime) realtime.start(name, language, greeting)
    else void call.start(browserCallAudio(language), greeting)
  }
  useEffect(() => {
    if (!shortcut || shortcut === started.current || !canStart) return
    started.current = shortcut
    start()
  }, [call, canStart, shortcut, language, greeting, realtime])
  if (realtime?.active) return null
  if (!call.active) return canStart || state.phase !== "idle" ? <div className="pa-bot-call-start">
    {realtime?.state?.error && <span role="alert">{realtime.state.error}</span>}
    {state.phase !== "idle" && <span role="status">{w[state.phase]}</span>}
    {canStart && <button type="button" className="pa-pill" aria-label={label} title={label}
      onClick={start}>
      <CallIcon/>{w.start}
    </button>}
  </div> : null
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
