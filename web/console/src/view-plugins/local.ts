import { useEffect, useMemo, useRef } from "react"
import type { SessionRow } from "@clawdline/contract"
import type { ViewSource } from "./bridge.js"
import { localPluginRows } from "./sources.js"
import { PluginError } from "./package.js"

export function useLocalPluginSource(rows: readonly SessionRow[], connected: boolean, loaded: boolean, open: (id: string) => void): ViewSource {
  const latest = useRef({ rows, connected, loaded, open }); latest.current = { rows, connected, loaded, open }
  const listeners = useRef(new Set<() => void>())
  useEffect(() => { for (const fn of listeners.current) fn() }, [rows, connected, loaded])
  return useMemo(() => ({
    key: "local", label: location.host,
    async read() {
      const current = latest.current
      if (!current.loaded) throw new PluginError("host_unavailable")
      return { sessions: localPluginRows(current.rows, current.connected), connected: current.connected }
    },
    subscribe(fn) { listeners.current.add(fn); return () => { listeners.current.delete(fn) } },
    async prepareOpen(key) {
      const find = () => {
        const current = latest.current
        if (!current.connected) throw new PluginError("host_unavailable")
        const at = localPluginRows(current.rows, true).findIndex(r => r.key === key)
        if (at < 0) throw new PluginError("session_unavailable")
        if (!current.rows[at].execution_generation) throw new PluginError("session_unavailable")
        return current.rows[at].id
      }
      find()
      return () => latest.current.open(find())
    },
  }), [])
}
