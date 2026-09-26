import type { SessionAgent, SessionRow } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"
import { nextWord } from "../next-strings.js"
import { agentDisplayName, workTree } from "./work-tree.js"

/**
 * The session and the provider-native subagents it sent away, as one short
 * tree: the session at the root, one ring per agent under it. Choosing an
 * agent opens its transcript in the pane (`onProvider`); choosing the root
 * goes back to the conversation.
 *
 * Drawn in the background sheet (`BackgroundStrip.tsx`), beside the commands
 * the session left running.
 */
export function WorkTree({
  row, selected, onProvider,
}: {
  row: SessionRow
  selected: string | null
  onProvider: (id: string | null) => void
}) {
  const nodes = workTree(row)
  const reading = row.agents_reading
  if (!nodes.length && reading?.state === "complete" && !reading.truncated) return null
  const providerReason = reading?.state === "complete" ? "" : nextWord(
    !reading ? "agentsUnknown" : reading.reason === "unreadable" ? "agentsUnreadable" :
      reading.reason === "unrecognized" ? "agentsUnrecognized" : "agentsNoRecord",
  )
  return (
    <section className="agents" aria-label={L.strings.webAgents}>
      <div className="head"><span>{L.strings.webAgents}</span><span className="n">{providerReason || nodes.length}</span></div>
      <button className="one root" type="button" aria-current={!selected} onClick={() => onProvider(null)}>
        <span className="mark"/><span className="kind">session</span><span className="what">{row.label || row.id}</span>
      </button>
      {nodes.map((node, index) => {
        const what = agentName(node.agent, row.assistant, index + 1)
        const detail = node.agent.doing && node.agent.doing !== what && node.agent.doing !== node.agent.type
          ? node.agent.doing
          : node.agent.result && node.agent.result !== what && node.agent.result !== node.agent.type
            ? node.agent.result
            : node.agent.at ? agentStarted(node.agent.at) : ""
        const stateWord = agentStateWord(node.agent, row.assistant)
        return (
          <button
            className="one child" type="button" key={`${node.source}:${node.id}`}
            data-state={node.state} aria-current={selected === node.id}
            title={`${nextWord("providerWork")} · ${stateWord}`}
            onClick={() => onProvider(node.id)}
          >
            <span className="mark"/><span className="kind">{node.agent.type}</span>
            <span className="what">{what}</span><span className="doing">{detail}</span>
            <span className="said">{stateWord}</span>
          </button>
        )
      })}
      {reading?.truncated ? <div className="head"><span>{nextWord("agentsTruncated", { count: reading.truncated })}</span></div> : null}
    </section>
  )
}

export function agentName(agent: SessionAgent, assistant: string | undefined, ordinal: number): string {
  return agentDisplayName(agent, assistant, ordinal, agentWords().codexMissing, L.strings.webAgents)
}

export function agentStateWord(agent: SessionAgent, assistant: string | undefined): string {
  if (agent.state === "done") return L.strings.webAgentDone
  if (agent.state === "failed") return L.strings.webAgentFailed
  if (agent.state === "unknown") return nextWord("agentsUnknown")
  return assistant === "claude" ? agentWords().claudeRunning : L.strings.agentRunning
}

function agentStarted(at: number): string {
  const time = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" }).format(new Date(at * 1000))
  return `${agentWords().started} ${time}`
}

function agentWords(): { codexMissing: string; claudeRunning: string; started: string } {
  const lang = (document.documentElement.lang || navigator.language || "en").toLowerCase()
  return lang.startsWith("zh")
    ? { codexMissing: "Codex 沒有記下這個 thread 在做什麼", claudeRunning: "推測仍在跑", started: "開始" }
    : { codexMissing: "Codex did not record what this thread is doing", claudeRunning: "appears to be running", started: "started" }
}
