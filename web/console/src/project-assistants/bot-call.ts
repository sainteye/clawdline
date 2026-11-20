import type * as W from "@clawdline/contract"
// @ts-expect-error -- Node's strip-types runner uses source extensions.
import { sameConversation, sameFence } from "./view-state.ts"

export type CallFailure = "unsupported" | "language" | "microphone" | "audio" | "disconnected" | "sendFailed" | "sendUnknown"
export type CallPhase = "idle" | "opening" | "listening" | "sending" | "waiting" | "permission" | "speaking" | CallFailure
export interface CallState {
  phase: CallPhase
  muted: boolean
}

export interface CallAudio {
  listen(enabled: boolean): void
  play(text: string, signal: AbortSignal): Promise<void>
  close(): void
}

export interface CallAudioSource {
  open(signal: AbortSignal, heard: (text: string) => void, failed: (why: CallFailure) => void): Promise<CallAudio>
}

export interface CallObservation {
  /** Includes the account, reader, source grant and source epoch. */
  owner: string
  fence: W.ProjectAssistantFence
  activeTurn: string
  turns: readonly W.ProjectAssistantReadTurnSummary[]
  messages: readonly W.ProjectAssistantMessage[]
  frontier: number
  caughtUp: boolean
}

export interface CallInput {
  callID: string
  fence: W.ProjectAssistantFence
  text: string
  greeting?: boolean
  signal: AbortSignal
}

type Submission = { state: "admitted"; turn: string; eventSeq: number } | { state: "sendFailed" | "sendUnknown" | "skipped" }

export interface CallPorts {
  current(): CallObservation | null
  submit(input: CallInput): Promise<Submission>
  changed(state: CallState): void
}

interface LiveCall {
  id: string
  owner: string
  fence: W.ProjectAssistantFence
  life: AbortController
  audio?: CallAudio
  turn?: { id: string; eventSeq: number }
  playback?: AbortController
}

/** A half-duplex call owns at most one submitted turn and one audio lease. */
export class BotCall {
  state: CallState = { phase: "idle", muted: false }
  private call?: LiveCall

  private ports: CallPorts

  constructor(ports: CallPorts) { this.ports = ports }

  get active() { return !!this.call }

  async start(source: CallAudioSource, greeting = false) {
    const ready = this.ports.current()
    if (this.call || !ready?.caughtUp) return
    const call: LiveCall = {
      id: crypto.randomUUID(), owner: ready.owner, fence: ready.fence, life: new AbortController(),
    }
    this.call = call
    this.set("opening")
    try {
      const audio = await source.open(call.life.signal, text => void this.heard(call, text), why => {
        if (this.call === call) this.end(why)
      })
      const current = this.current(call)
      if (!current) {
        audio.close()
        return
      }
      call.audio = audio
      if (greeting && !current.activeTurn) await this.submit(call, current.fence, "", true)
      else this.set("listening")
    } catch (error) {
      if (this.call === call) this.end(error instanceof CallAudioError ? error.code : "microphone")
    }
  }

  end(phase: "idle" | CallFailure = "idle") {
    const call = this.call
    if (!call && this.state.phase === phase) return
    this.call = undefined
    call?.life.abort()
    call?.playback?.abort()
    call?.audio?.close()
    this.set(phase, false)
  }

  mute() {
    if (this.call && this.current(this.call)) this.set(this.state.phase, !this.state.muted)
  }

  interrupt() {
    const call = this.call
    if (!call?.playback || !this.current(call)) return
    call.playback.abort()
    call.playback = undefined
    this.set("listening")
  }

  observe() {
    const call = this.call
    if (!call) return
    const now = this.current(call)
    if (!now || !call.turn) return
    // Only the receipt's turn can carry a first binding or an execution rebind.
    const owned = call.turn
    const turn = now.turns.find(t => t.id === owned.id && sameFence(t.fence, now.fence))
    if (!turn) {
      if (now.caughtUp && now.frontier >= owned.eventSeq && !sameFence(call.fence, now.fence)) this.end("disconnected")
      return
    }
    call.fence = turn.fence
    if (turn.state === "waiting_permission") this.set("permission")
    else if (this.state.phase === "permission") this.set("waiting")
    if (!now.caughtUp) return
    if (turn.state === "unknown") return this.end("sendUnknown")
    if (turn.state === "failed") return this.end("sendFailed")
    if (turn.state === "cancelled") {
      call.turn = undefined
      this.set("listening")
      return
    }
    if (turn.state !== "succeeded") return
    const text = now.messages.filter(message => message.kind === "assistant" && message.turn_id === turn.id
      && sameFence(message.fence, turn.fence)).map(message => message.text.trim()).filter(Boolean).join("\n")
    call.turn = undefined
    if (!text) this.set("listening")
    else void this.play(call, text)
  }

  private current(call: LiveCall) {
    if (this.call !== call) return null
    const now = this.ports.current()
    if (now?.owner === call.owner && sameConversation(call.fence, now.fence)
      && (sameFence(call.fence, now.fence) || this.state.phase === "sending" || !!call.turn)) return now
    this.end("disconnected")
    return null
  }

  private set(phase: CallPhase, muted = this.state.muted) {
    const changed = this.state.phase !== phase || this.state.muted !== muted
    if (!changed) return
    this.state = { phase, muted }
    this.ports.changed(this.state)
    this.call?.audio?.listen(phase === "listening" && !muted)
  }

  private async heard(call: LiveCall, text: string) {
    if (this.call !== call || this.state.phase !== "listening" || this.state.muted || !text.trim()) return
    const now = this.current(call)
    if (!now?.caughtUp || now.activeTurn) return
    await this.submit(call, now.fence, text.trim())
  }

  private async submit(call: LiveCall, fence: W.ProjectAssistantFence, text: string, greeting = false) {
    this.set("sending")
    try {
      const result = await this.ports.submit({ callID: call.id, fence, text, greeting, signal: call.life.signal })
      if (!this.current(call)) return
      if (result.state === "skipped") return this.set("listening")
      if (result.state !== "admitted") return this.end(result.state)
      call.turn = { id: result.turn, eventSeq: result.eventSeq }
      this.set("waiting")
      this.observe()
    } catch {
      if (this.call === call) this.end("sendUnknown")
    }
  }

  private async play(call: LiveCall, text: string) {
    if (!this.current(call) || !call.audio) return
    const playback = new AbortController()
    call.playback = playback
    this.set("speaking")
    try {
      await call.audio.play(text, playback.signal)
      if (this.current(call) && call.playback === playback) {
        call.playback = undefined
        this.set("listening")
      }
    } catch {
      if (this.call === call && call.playback === playback) this.end("audio")
    }
  }
}

export class CallAudioError extends Error {
  readonly code: CallFailure

  constructor(code: CallFailure) {
    super(code)
    this.code = code
  }
}

const callSendPrefix = "bot-call-"

/** The existing source receipt carries call attribution without a second send API. */
export function botCallSendID(callID: string) {
  return callSendPrefix + callID + "/" + crypto.randomUUID()
}

export function isBotCallSend(id: string) {
  return id.startsWith(callSendPrefix) || id.startsWith("lead-call-")
}

/** The source rejects this exact input before its admission transaction mutates.
 * Never inspect nested transport evidence: an unknown response stays unknown. */
export function handoffInputRefused(cause: unknown): boolean {
  const value = cause as Partial<W.ProjectAssistantErrorResponse> | null
  return value?.contract_version === "project-assistants.v1" && value.error?.code === "pa_stale_version"
    && value.error.field === "handoff_revision" && value.error.retry_after_seconds === 0
}
