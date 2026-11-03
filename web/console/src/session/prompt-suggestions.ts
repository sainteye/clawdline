import { useEffect, useRef, useState } from "react"
import type { PromptSuggestion, SessionRow } from "@clawdline/contract"
import { client, followsRelay } from "../client.js"
import { readScreen, visibleInterval } from "../legacy/screen-bridge.js"
import { composerDraftKey } from "./composer-drafts.js"

import { PromptSuggestionState } from "./prompt-suggestion-state.js"

/** Watching renews the existing bounded screen lease; idle panes are signalled. */
export function usePromptSuggestion(row: SessionRow | null, enabled: boolean) {
  const eligible = enabled && row?.assistant === "claude" && row.backend === "tmux" && !!row.sessionId && row.prompt_suggestion?.source === "claude-terminal"
  const key = eligible ? `${composerDraftKey(row)}:${row.execution_generation ?? ""}` : ""
  const [held, setHeld] = useState<{ key: string; value: PromptSuggestion | null } | null>(null)
  const owner = useRef<{ key: string; state: PromptSuggestionState } | null>(null)
  const latest = useRef(key); latest.current = key
  const publish = () => {
    const current = owner.current
    if (current && current.key === latest.current) setHeld({ key: current.key, value: current.state.read() })
  }
  const dismiss = () => { owner.current?.state.dismiss(); publish() }
  const take = (id: string): PromptSuggestion | null => {
    const current = owner.current
    const value = current?.state.read()
    if (!current || current.key !== latest.current || !value || value.id !== id) return null
    current.state.dismiss(); publish()
    return value
  }

  useEffect(() => {
    if (!key || !row?.sessionId) { owner.current = null; setHeld(null); return }
    const state = new PromptSuggestionState(row.sessionId)
    owner.current = { key, state }
    let alive = true
    let asking = false
    let wanted = false
    const observe = (value: PromptSuggestion | null | undefined) => {
      if (!alive || latest.current !== key || document.visibilityState === "hidden" || navigator.onLine === false) return
      state.observe(value); publish()
    }
    const load = () => {
      if (!alive || document.visibilityState === "hidden") return
      if (asking) { wanted = true; return }
      asking = true
      void readScreen(row.id).then(screen => {
        if (!alive || latest.current !== key) return
        observe(screen?.prompt_suggestion)
        // A pending initial capture arrives on the native screen event or the
        // Cloud row. It is never polled at a faster invented interval.
      }).catch(() => {
        if (alive) { state.pause(); publish() }
      }).finally(() => { asking = false; if (wanted) { wanted = false; load() } })
    }
    load()
    const lease = visibleInterval(load, 15000)
    lease.start()
    let stream: EventSource | null = null
    if (!followsRelay() && typeof EventSource === "function") {
      stream = new EventSource(client.url("/v1/events"))
      stream.addEventListener("screen", event => {
        let moved: { id?: string }
        try { moved = JSON.parse((event as MessageEvent).data) } catch { return }
        if (moved.id === row.id) load()
      })
      stream.onerror = () => { state.pause(); publish() }
      stream.onopen = load
    }
    const unavailable = () => { state.pause(); publish() }
    const visible = () => {
      if (document.visibilityState === "hidden") unavailable()
      else load()
    }
    document.addEventListener("visibilitychange", visible)
    window.addEventListener("offline", unavailable)
    window.addEventListener("online", load)
    return () => {
      alive = false
      lease.stop(); stream?.close()
      document.removeEventListener("visibilitychange", visible)
      window.removeEventListener("offline", unavailable)
      window.removeEventListener("online", load)
    }
  }, [key])

  useEffect(() => {
    const current = owner.current
    if (!current || current.key !== key || document.visibilityState === "hidden" || navigator.onLine === false) return
    current.state.observe(row?.prompt_suggestion)
    publish()
  }, [key, row?.prompt_suggestion])

  // A newly captured idle native prompt may arrive before the list observes
  // the end of its turn. It must postdate a working row to outrank that row.
  const candidate = held?.key === key ? held.value : null
  const stateAdmits = row?.state === "idle" || row?.state === "working" && !!candidate &&
    candidate.observed_at > Number(row.source?.observed_at ?? Infinity) * 1000
  const available = eligible && stateAdmits && !row?.menu && document.visibilityState !== "hidden" && navigator.onLine !== false &&
    row?.source?.freshness !== "unverified" && row?.source?.freshness !== "missing" ? candidate : null
  return { candidate: available, dismiss, take }
}
