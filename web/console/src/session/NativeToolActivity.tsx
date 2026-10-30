import { useState } from "react"
import type { TranscriptEntry } from "@clawdline/contract"
import { catalogWord } from "../catalog.js"
import { nextWord } from "../next-strings.js"
import { LazyEntryBody } from "./LazyEntryBody.js"
import { ToolRow, ToolRunToggle } from "./ToolPresentation.js"

export function nativeToolTitle(entry: TranscriptEntry): string {
  const action = entry.activity?.actions?.[0]
  if ((entry.tool === "read" || entry.tool === "search") && action?.path) {
    const name = action.path.split(/[\\/]/).filter(Boolean).pop() || action.path
    const lines = action.line ? `:${action.line}${action.endLine && action.endLine !== action.line ? "–" + action.endLine : ""}` : ""
    return nextWord(entry.tool === "read" ? "nativeToolRead" : "nativeToolSearch") + " " + name + lines
  }
  return entry.activity?.title || entry.text || entry.tool || ""
}

/** Only contiguous activity with the same explicit owner can share a group.
 * Unscoped reads can compact together, but never acquire a guessed agent. */
export function nativeRunLength(entries: readonly TranscriptEntry[], at: number): number {
  const first = entries[at]
  const source = first.activity?.agentID || ""
  const reads = (e: TranscriptEntry) => e.tool === "read" || e.tool === "search"
  if (!first.activity || (!source && !reads(first))) return 1
  let end = at + 1
  while (end < entries.length) {
    const entry = entries[end]
    if (entry.role !== "tool" || !entry.activity || (entry.activity.agentID || "") !== source || !source && !reads(entry)) break
    end++
  }
  return end - at
}

function NativeToolBody({ entry }: { entry: TranscriptEntry }) {
  return <>
    {entry.activity?.result ? <pre className="code">{entry.activity.result}</pre>
      : <p className="tool-result-note">{nextWord(entry.activity?.resultKnown ? "nativeToolEmpty" : "nativeToolNoResult")}</p>}
    {entry.fileChanges?.map((change, n) => <div className="native-tool-change" key={n}>
      <code>{change.path}</code><pre className="code">{change.unifiedDiff || change.content}</pre>
    </div>)}
  </>
}

type NativeToolLabels = { toolLabel: string; countLabel: (count: number) => string }

export function NativeToolCard({ entry, sessionID, expanded = false, toolLabel }: { entry: TranscriptEntry; sessionID?: string; expanded?: boolean; toolLabel: string }) {
  const [open, setOpen] = useState(expanded)
  const activity = entry.activity
  const statuses: Record<string, string> = { completed: "toolCompleted", inProgress: "toolRunning", in_progress: "toolRunning", pending: "toolPending", failed: "toolFailed", cancelled: "toolCancelled" }
  const status = activity?.status ? (statuses[activity.status] ? catalogWord("antigravity", statuses[activity.status]) : activity.status) : ""
  const agent = activity?.agentID
  const title = nativeToolTitle(entry)
  return <ToolRow label={agent ? nextWord("nativeToolAgent", { id: agent.slice(0, 8) }) : toolLabel} labelTitle={agent}
    subject={title} name={entry.tool === "execute" ? "shell" : undefined} meta={status}
    open={open} onToggle={() => setOpen(value => !value)} agentID={agent}
    live={["pending", "in_progress", "inProgress"].includes(activity?.status || "")}>
    {open && <div className="toolbody native-tool-details">
      <p className="native-tool-command"><code>{title}</code></p>
      {agent && <p className="native-tool-path"><code>{agent}</code></p>}
      {activity?.actions?.map((action, n) => <p className="native-tool-path" key={n}><code>{action.path}{action.line ? ":" + action.line : ""}{action.endLine && action.endLine !== action.line ? "–" + action.endLine : ""}</code></p>)}
      {activity?.delegates?.length ? <section><b>{nextWord("nativeToolDelegates")}</b><ul>
        {activity.delegates.map((delegate, n) => <li key={n}>{delegate.name}{delegate.model ? " · " + delegate.model : ""}</li>)}
      </ul></section> : null}
      {entry.detail ? <LazyEntryBody key={(sessionID || "") + entry.detail.reference} sessionID={sessionID} detail={entry.detail} open={open}>
        {full => <NativeToolBody entry={full} />}
      </LazyEntryBody> : <NativeToolBody entry={entry} />}
    </div>}
  </ToolRow>
}

export function NativeToolRun({ entries, sessionID, toolLabel, countLabel }: { entries: readonly TranscriptEntry[]; sessionID?: string } & NativeToolLabels) {
  const [open, setOpen] = useState(false)
  if (entries.length === 1) return <NativeToolCard entry={entries[0]} sessionID={sessionID} toolLabel={toolLabel} />
  const agent = entries[0].activity?.agentID
  const waiting = entries.filter(e => ["pending", "in_progress", "inProgress"].includes(e.activity?.status || "")).length
  return <>
    <ToolRunToggle label={agent ? nextWord("nativeToolAgent", { id: agent.slice(0, 8) }) : toolLabel} labelTitle={agent}
      count={countLabel(entries.length)} description={entries.map(nativeToolTitle).filter((name, n, names) => names.indexOf(name) === n).join(" · ")}
      meta={waiting > 0 ? nextWord("nativeToolAwaiting", { count: waiting }) : undefined}
      open={open} onToggle={() => setOpen(value => !value)} live={waiting > 0} agentID={agent} />
    {open && entries.map((entry, n) => <NativeToolCard key={entry.id || entry.activity?.toolCallID || n} entry={entry} sessionID={sessionID} toolLabel={toolLabel} />)}
  </>
}
