import { useEffect, useLayoutEffect, useRef, useState } from "react"
import type * as W from "@clawdline/contract"
import { nextWord } from "../next-strings.js"
import { VOICE_CALL_HEADER } from "../cloud/voice-call.js"
import { RealtimeCopilotClient, realtimeCopilot, type RealtimeCopilotState } from "../realtime/realtime-copilot"
import { sameConversation, sameFence } from "./view-state"
import { botVoiceScope, type BotVoicePorts, type BotVoiceObservation } from "./bot-voice-tools"
import { BotAvatar } from "./BotAvatar"
import { botCallWords } from "./bot-call-words"
import "../realtime/realtime.css"

export function browserBotVoiceAvailable() {
  return typeof RTCPeerConnection === "function" && typeof navigator.mediaDevices?.getUserMedia === "function"
}

export function useBotVoice(ports: BotVoicePorts, enabled: boolean) {
  const latest = useRef(ports)
  const [configured, setConfigured] = useState<boolean | null>(null)
  const [state, setState] = useState<RealtimeCopilotState | null>(null)
  const [muted, setMuted] = useState(false)
  const running = useRef<{
    client: RealtimeCopilotClient
    release: () => void
    target: BotVoiceObservation
    turns: Set<string>
    lastMessage: number
    permissions: Set<string>
  } | null>(null)
  const active = state?.status === "connecting" || state?.status === "connected"

  useEffect(() => {
    if (!enabled) return
    if (!browserBotVoiceAvailable()) { setConfigured(false); return }
    const controller = new AbortController()
    void fetch("/v1/settings/copilot", { headers: { [VOICE_CALL_HEADER]: "1" }, signal: controller.signal })
      .then(async response => response.ok && (await response.json()).key_set === true)
      .then(ready => { if (!controller.signal.aborted) setConfigured(ready) })
      .catch(() => { if (!controller.signal.aborted) setConfigured(false) })
    return () => controller.abort()
  }, [enabled])

  useLayoutEffect(() => {
    latest.current = ports
    const call = running.current
    if (!call || !active) return
    const now = ports.current()
    if (!now || now.owner !== call.target.owner || !sameConversation(call.target.fence, now.fence)) {
      call.client.disconnect()
      return
    }
    if (!now.caughtUp || !state?.connectionReady) return
    for (const turn of now.turns) {
      if (call.turns.has(turn.id)) continue
      if (turn.state === "failed" || turn.state === "unknown" || turn.state === "cancelled") {
        call.turns.add(turn.id)
        call.client.scopedNotice("Bot turn ended with state: " + turn.state)
      }
      if (turn.state !== "succeeded") continue
      const replies = now.messages.filter(row => row.kind === "assistant" && !!row.text.trim()
        && row.turn_id === turn.id && sameFence(row.fence, turn.fence) && row.seq > call.lastMessage)
      if (!replies.length) continue
      call.turns.add(turn.id)
      call.client.scopedNotice(replies.map(row => row.text).join("\n").slice(0, 2000))
    }
    for (const permission of now.permissions) {
      if (call.permissions.has(permission.id)) continue
      call.permissions.add(permission.id)
      call.client.scopedNotice("The Bot needs approval. Ask the person to review the approval card in chat.")
    }
  }, [ports, active, state?.connectionReady])

  useEffect(() => {
    const disconnect = () => {
      const call = running.current
      if (call?.client.getState().status === "connecting" || call?.client.getState().status === "connected") call.client.disconnect()
    }
    const hide = () => { if (document.hidden) disconnect() }
    const release = realtimeCopilot.subscribe(call => {
      if (call.status === "connecting" || call.status === "connected") disconnect()
    })
    window.addEventListener("offline", disconnect)
    window.addEventListener("pagehide", disconnect)
    document.addEventListener("visibilitychange", hide)
    return () => {
      release()
      window.removeEventListener("offline", disconnect)
      window.removeEventListener("pagehide", disconnect)
      document.removeEventListener("visibilitychange", hide)
      running.current?.release()
      disconnect()
    }
  }, [])

  function start(name: string, language: string, greeting: boolean) {
    const target = latest.current.current()
    const session = realtimeCopilot.getState()
    if (!target?.caughtUp || active || session.status === "connected" || session.status === "connecting") return
    const previous = running.current?.client.getState().status
    if (previous === "connecting" || previous === "connected") return
    running.current?.release()
    const scope = botVoiceScope(name, language, target, {
      current: () => latest.current.current(),
      submit: input => latest.current.submit(input),
      memory: signal => latest.current.memory(signal),
      stop: (turn, signal) => latest.current.stop(turn, signal),
    })
    const client = new RealtimeCopilotClient({ ...scope, ready: signal => {
      if (!greeting) return
      const now = latest.current.current()
      if (now?.owner === target.owner && sameConversation(now.fence, target.fence) && !now.activeTurn) {
        void latest.current.submit({ callID: crypto.randomUUID(), fence: now.fence, text: "", greeting: true, signal })
      }
    } })
    setMuted(false)
    running.current = {
      client, target, release: client.subscribe(setState),
      turns: new Set(target.turns.filter(turn => turn.state === "succeeded" || turn.state === "cancelled" || turn.state === "failed" || turn.state === "unknown").map(turn => turn.id)),
      lastMessage: target.messages.reduce((last, row) => Math.max(last, row.seq), 0),
      permissions: new Set(),
    }
    void client.start()
  }

  return {
    configured, active, state, muted, start,
    hangup: () => running.current?.client.disconnect(),
    mute: () => {
      running.current?.client.muteMicrophone(!muted)
      setMuted(!muted)
    },
  }
}

export function BotVoicePanel({ bot, language, call }: {
  bot: Pick<W.ProjectAssistantBot, "name" | "appearance">; language: string; call: ReturnType<typeof useBotVoice>;
}) {
  const state = call.state
  if (!state || !call.active) return null
  const w = botCallWords(language)
  const phase = state.status === "connecting" || !state.connectionReady ? "opening"
    : state.speakingState === "thinking" ? "waiting" : state.speakingState === "speaking" ? "speaking" : call.muted ? "muted" : "listening"
  return <section className="pa-bot-voice-panel" aria-label={`${w.start} · ${bot.name}`}>
    <div className="pa-bot-voice-identity">
      <BotAvatar appearance={bot.appearance} size={84} mood={phase === "speaking" ? "needsInput" : phase === "waiting" || phase === "opening" ? "working" : "idle"}/>
      <div><strong>{bot.name}</strong><span role="status">{w[phase]}</span></div>
      <div className="pa-bot-voice-actions">
        <button type="button" className="pa-pill" aria-pressed={call.muted} onClick={call.mute}>{w.mute}</button>
        <button type="button" className="realtime-hangup-btn pa-bot-live-hangup" onClick={call.hangup}>{w.end}</button>
      </div>
    </div>
    {(state.lastTranscript || state.lastAssistantSpeech) && <div className="realtime-transcript-box pa-bot-voice-transcript"
      role="region" tabIndex={0} aria-label={nextWord("copilotTranscript")}>
      {state.lastTranscript && <div className="realtime-bubble user">
        <strong className="bubble-label">{nextWord("copilotYouSaid")}</strong><span>{state.lastTranscript}</span>
      </div>}
      {state.lastAssistantSpeech && <div className="realtime-bubble assistant">
        <strong className="bubble-label">{bot.name}</strong><span>{state.lastAssistantSpeech}</span>
      </div>}
    </div>}
  </section>
}
