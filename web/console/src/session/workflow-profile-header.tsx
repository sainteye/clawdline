import { useEffect, useRef, useState, useSyncExternalStore } from "react"
import type { SessionRow } from "@clawdline/contract"
import { client } from "../client.js"
import { onMachineChanged } from "../cloud/machine-switch.js"
import { workflowLabel, workflowWord, type WorkflowRecord } from "./workflow-profile.js"
import "./workflow-profile.css"

let transportGeneration = 0
const listeners = new Set<() => void>()
onMachineChanged(() => { transportGeneration++; for (const listener of listeners) listener() })
function subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener) } }

/** An immutable launch read is scoped to the current machine, conversation and execution. */
export function WorkflowProfileHeader({ row, machine }: { row: SessionRow | null; machine?: string | null }) {
  const transport = useSyncExternalStore(subscribe, () => transportGeneration)
  const identity = JSON.stringify([machine, row?.id, row?.sessionId, row?.execution_generation, transport])
  const [answer, setAnswer] = useState<{ identity: string; workflow?: WorkflowRecord } | null>(null)
  const current = useRef(identity); current.current = identity
  useEffect(() => {
    if (!row) return
    const controller = new AbortController()
    const load = async () => {
      try {
        const path = "/v1/sessions/" + encodeURIComponent(row.id) + "/info?parts=summary"
        const response = await fetch(client.url(path), { signal: controller.signal })
        if (!response.ok) throw new Error("Workflow launch record unavailable")
        const body = await response.json()
        if (controller.signal.aborted || current.current !== identity) return
        if (!body?.info?.session || (row.sessionId && body.info.session.sessionId !== row.sessionId)) throw new Error("Workflow launch identity changed")
        setAnswer({ identity, workflow: body.info.workflow })
      } catch {
        if (!controller.signal.aborted && current.current === identity) setAnswer({ identity, workflow: { state: "unavailable" } })
      }
    }
    void load()
    return () => controller.abort()
  }, [identity])
  if (!row || answer?.identity !== identity) return null
  return <span className="detail-workflow" title={workflowWord("title") + ": " + workflowLabel(answer.workflow)}>{workflowLabel(answer.workflow)}<span aria-hidden="true"> · </span></span>
}
