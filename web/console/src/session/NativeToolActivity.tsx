import { useState } from "react"
import type { TranscriptEntry } from "@clawdline/contract"
import { catalogWord } from "../catalog.js"
import { nextWord } from "../next-strings.js"
import { LazyEntryBody } from "./LazyEntryBody.js"

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
    {entry.activity?.result ? <pre className="activity-result">{entry.activity.result}</pre>
      : <p className="tool-result-note">{nextWord(entry.activity?.resultKnown ? "nativeToolEmpty" : "nativeToolNoResult")}</p>}
    {entry.fileChanges?.map((change, n) => <div className="native-tool-change" key={n}>
      <code>{change.path}</code><pre>{change.unifiedDiff || change.content}</pre>
    </div>)}
  </>
}

export function NativeToolCard({ entry, sessionID, expanded = false }: { entry: TranscriptEntry; sessionID?: string; expanded?: boolean }) {
  const [open, setOpen] = useState(expanded)
  const activity = entry.activity
  const statuses: Record<string, string> = {completed:"toolCompleted",inProgress:"toolRunning",in_progress:"toolRunning",pending:"toolPending",failed:"toolFailed",cancelled:"toolCancelled"}
  const status = activity?.status ? (statuses[activity.status] ? catalogWord("antigravity", statuses[activity.status]) : activity.status) : ""
  return <div className="entry activity native-tool" data-role="tool" data-agent={activity?.agentID || undefined}>
    <div className="who" title={activity?.agentID}>{activity?.agentID ? nextWord("nativeToolAgent", {id:activity.agentID.slice(0,8)}) : catalogWord("antigravity","toolCalled")}</div>
    <div className="body"><details className="activity-card" open={open} onToggle={event => setOpen(event.currentTarget.open)}>
      <summary className="native-tool-summary"><span aria-hidden="true">{open ? "⌄" : "›"}</span><span className="activity-title">{nativeToolTitle(entry)}</span><span className="activity-meta">{status}</span></summary>
      {open && <div className="native-tool-body">
        {activity?.agentID && <p className="native-tool-path"><code>{activity.agentID}</code></p>}
        {activity?.actions?.map((action, n) => <p className="native-tool-path" key={n}><code>{action.path}{action.line ? ":" + action.line : ""}{action.endLine && action.endLine !== action.line ? "–" + action.endLine : ""}</code></p>)}
        {activity?.delegates?.length ? <section className="native-tool-delegates"><b>{nextWord("nativeToolDelegates")}</b><ul>
          {activity.delegates.map((delegate, n) => <li key={n}>{delegate.name}{delegate.model ? " · " + delegate.model : ""}</li>)}
        </ul></section> : null}
        {entry.detail ? <LazyEntryBody key={(sessionID || "") + entry.detail.reference} sessionID={sessionID} detail={entry.detail} open={open}>
          {full => <NativeToolBody entry={full} />}
        </LazyEntryBody> : <NativeToolBody entry={entry} />}
      </div>}
    </details></div>
  </div>
}

export function NativeToolRun({ entries, sessionID }: { entries: readonly TranscriptEntry[]; sessionID?: string }) {
  const [open,setOpen]=useState(false)
  if (entries.length===1) return <NativeToolCard entry={entries[0]} sessionID={sessionID}/>
  const agent=entries[0].activity?.agentID
  const waiting=entries.filter(e=>["pending","in_progress","inProgress"].includes(e.activity?.status || "")).length
  return <div className="entry folded native-tool-run" data-role="tool" data-agent={agent || undefined}>
    <div className="who" title={agent}>{agent ? nextWord("nativeToolAgent",{id:agent.slice(0,8)}) : catalogWord("antigravity","toolExplored")}</div>
    <div className="body"><details open={open} onToggle={event=>setOpen(event.currentTarget.open)}>
      <summary className="pill"><span aria-hidden="true">{open ? "⌄" : "›"}</span><span>{nextWord("nativeToolRun",{count:entries.length})}</span>
        {waiting>0 && <span className="activity-meta">{nextWord("nativeToolAwaiting",{count:waiting})}</span>}
        <span className="what">{entries.map(nativeToolTitle).filter((name,n,names)=>names.indexOf(name)===n).join(" · ")}</span>
      </summary>
      {open && entries.map((entry,n)=><NativeToolCard key={entry.id || entry.activity?.toolCallID || n} entry={entry} sessionID={sessionID}/>)}
    </details></div>
  </div>
}
