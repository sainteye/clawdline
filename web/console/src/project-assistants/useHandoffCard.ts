import {useCallback, useEffect, useRef, useState} from "react"
import type * as W from "@clawdline/contract"
import {authorized, command, errorCode, failure, type SourceClient} from "./client"
import {sameFence} from "./view-state"

export function useHandoffCard(source: SourceClient, current: () => readonly SourceClient[],
  fence: W.ProjectAssistantFence | undefined, enabled: boolean, revision: string) {
  const available = !!(enabled && fence?.generation && source.canHandoff?.() && source.handoff && source.writeHandoff)
  const [view, setView] = useState<W.ProjectAssistantHandoffResponse | null>(null)
  const [error, setError] = useState("")
  const [busy, setBusy] = useState(false)
  const read = useRef<AbortController | null>(null)
  const write = useRef<AbortController | null>(null)
  const frame = useRef({source, current, fence, available})
  frame.current = {source, current, fence, available}
  const matches = (expected: W.ProjectAssistantFence, answer: W.ProjectAssistantHandoffResponse) =>
    answer?.contract_version === "project-assistants.v1" && !!answer.fence && sameFence(answer.fence, expected)
  const stillCurrent = (expected: typeof frame.current) =>
    frame.current.available && frame.current.source === expected.source && !!frame.current.fence && !!expected.fence &&
    sameFence(frame.current.fence, expected.fence)

  const refresh = useCallback(async () => {
    const expected = frame.current
    if (!expected.available || !expected.fence || write.current) return
    read.current?.abort()
    const control = new AbortController()
    read.current = control
    try {
      const {fence, source, current} = expected
      const answer = await authorized(source, current, signal => source.handoff!(fence.key, fence.conversation_id, signal), control.signal)
      if (!stillCurrent(expected) || control.signal.aborted) return
      if (!matches(fence, answer)) throw failure("pa_stale_generation")
      setView(answer)
      setError("")
    } catch (cause) {
      if (stillCurrent(expected) && !control.signal.aborted) {
        setView(null)
        setError(errorCode(cause))
      }
    } finally {
      if (read.current === control) read.current = null
    }
  }, [])

  useEffect(() => {
    if (!available) {
      write.current?.abort()
      setBusy(false)
      setView(null)
      setError("")
      return
    }
    void refresh()
    const checkVisible = () => { if (!document.hidden) void refresh() }
    document.addEventListener("visibilitychange", checkVisible)
    window.addEventListener("focus", checkVisible)
    return () => {
      read.current?.abort()
      document.removeEventListener("visibilitychange", checkVisible)
      window.removeEventListener("focus", checkVisible)
    }
  }, [source, available, revision, refresh])
  useEffect(() => () => write.current?.abort(), [source])

  const update = async (action: W.ProjectAssistantHandoffAction, ref?: W.ProjectAssistantHandoffRef, text = "") => {
    const expected = frame.current
    if (!expected.available || !expected.fence || write.current) return false
    const {fence, source, current} = expected
    read.current?.abort()
    const control = new AbortController()
    write.current = control
    setBusy(true)
    setError("")
    try {
      const request = {command: command(fence.key, "edit"), fence, action, ...(ref ? {ref} : {}), text}
      const answer = await authorized(source, current, signal => source.writeHandoff!(request, signal), control.signal)
      if (!stillCurrent(expected) || control.signal.aborted) return false
      if (!matches(fence, answer)) throw failure("pa_stale_generation")
      setView(answer)
      return true
    } catch (cause) {
      if (stillCurrent(expected) && !control.signal.aborted) setError(errorCode(cause))
      return false
    } finally {
      if (write.current === control) {
        write.current = null
        if (!control.signal.aborted) setBusy(false)
      }
    }
  }

  const supported = available && error !== "pa_unsupported"
  const shown = supported && view && fence && matches(fence, view) ? view : null
  return {
    available: supported, ready: !supported || !!shown, card: shown?.card ?? null, canPrepare: !!shown?.can_prepare,
    busy, mutating: () => !!write.current, error: supported ? error : "",
    refresh, update, conflict: (code: string) => {setView(null);setError(code)},
  }
}
