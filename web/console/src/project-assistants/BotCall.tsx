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

/** Whether, and how, this chat can place a call from the composer's trailing button. */
export function useBotCallStart({call, available, ready, name, language, greeting=false, shortcut=0, realtime}: {
  call: BotCall; available: boolean; ready: boolean; name: string; language: string; greeting?: boolean; shortcut?: number; realtime?: ReturnType<typeof useBotVoice>
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
  // Availability is what the browser and settings allow; readiness is this moment's
  // chat (caught up, nothing sending). Only availability decides whether the button
  // shows, so a send in flight disables it instead of hiding it.
  const canStart = available && (!!realtime || readyLanguage === language)
  const startable = canStart && ready && !call.active && !realtime?.active
  const label = `${w.start} · ${name}`
  const started = useRef(0)
  const start = () => {
    if (!startable) return
    if (dictating()) call.end("microphone")
    else if (realtime) realtime.start(name, language, greeting)
    else void call.start(browserCallAudio(language), greeting)
  }
  useEffect(() => {
    if (!shortcut || shortcut === started.current || !startable) return
    started.current = shortcut
    start()
  }, [call, startable, shortcut, language, greeting, realtime])
  return { canStart, ready: startable, start, label }
}

/** The browser call's controls, and any call status or start error, under the chat header. */
export function BotCallBar({call, state, name, language, realtime}: {
  call: BotCall; state: CallState; name: string; language: string; realtime?: ReturnType<typeof useBotVoice>
}) {
  const w = botCallWords(language)
  if (realtime?.active) return null
  const label = `${w.start} · ${name}`
  if (!call.active) {
    const error = realtime?.state?.error
    if (!error && state.phase === "idle") return null
    return <div className="pa-bot-call-notice">
      {error && <span role="alert">{error}</span>}
      {state.phase !== "idle" && <span role="status">{w[state.phase]}</span>}
    </div>
  }
  const phase = state.muted && state.phase === "listening" ? "muted" : state.phase
  return <section className="pa-bot-call" aria-label={label}>
    <span className="pa-bot-call-status" role="status"><strong>{name}</strong> {phase !== "idle" && w[phase]}</span>
    {state.phase === "speaking" && <button type="button" className="pa-pill" onClick={() => call.interrupt()}>{w.interrupt}</button>}
    <button type="button" className="pa-pill" aria-pressed={state.muted} onClick={() => call.mute()}>{w.mute}</button>
    <button type="button" className="pa-pill pa-bot-call-end" onClick={() => call.end()}>{w.end}</button>
  </section>
}
