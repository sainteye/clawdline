import type * as W from "@clawdline/contract"

/** One line of a machine's checklist: passed, failed (with the fix) or not known yet. */
export interface StatusRow { label: string; state: "ok" | "bad" | "unknown"; detail: string }
export interface MachineStatus { id: string; name: string; rows: StatusRow[]; problem: string; fix: string }
export interface SourceHealth { ok: boolean; summary: string; fix: string; machines: MachineStatus[]; withoutBots: { id: string; name: string }[] }

const AGENT: Record<string, string> = { claude: "Claude Code", codex: "Codex" }
const fill = (text: string, name: string) => text.replace("{name}", name)

/** Why an agent cannot run, in the person's words; the code stays for the details line. */
function agentProblem(code: string, w: Record<string, string>) {
  if (code === "auth_required" || code === "auth_rejected") return w.agentNeedsLogin
  if (code === "cli_not_installed") return w.agentNotInstalled
  if (code === "provider_version_unsupported") return w.agentTooOld
  // refusal-ok: the checklist row shows the agent's own code next to this phrase, so no refusal is lost.
  return w.agentUnavailable
}

/**
 * The Bots roster's status line and checklist (the person's choice on
 * 2026-11-14: a line at the top of the list, a card when something is wrong,
 * and a short checklist behind it). Every paired device may act, so the only
 * write problem shown is a machine that turned Cloud operation off.
 */
export function sourceHealth(statuses: readonly W.ProjectAssistantSourceStatus[], names: ReadonlyMap<string, string>, withoutBots: readonly string[], w: Record<string, string>): SourceHealth {
  const name = (id: string) => names.get(id) || w.thisMachine
  const machines = statuses.map(s => {
    const n = name(s.machine_id), rows: StatusRow[] = []
    let problem = "", fix = ""
    const connected = s.state === "online" && s.fresh
    rows.push({ label: w.statusConnected, state: connected ? "ok" : "bad", detail: connected ? "" : s.state !== "online" ? (w[s.state] ?? s.state) : w.stale })
    if (!connected) { problem = fill(s.state !== "online" ? w.statusOffline : w.statusStale, n); fix = w.fixOffline }
    const on = s.access === "interactive"
    rows.push({ label: w.statusBotsOn, state: on ? "ok" : "bad", detail: on ? "" : (w[s.access] ?? s.access) })
    if (!on && !problem) { problem = fill(w.statusBotsOff, n); fix = w.fixBotsOff }
    if (connected && on && !s.writable) {
      rows.push({ label: w.statusCloudOperation, state: "bad", detail: "" })
      if (!problem) { problem = fill(w.statusCloudOff, n); fix = w.fixCloudOff }
    }
    const agents = new Map<string, W.ProjectAssistantCapabilities>()
    for (const c of s.capabilities) if (!agents.has(c.agent) || c.unavailable) agents.set(c.agent, c)
    for (const agent of ["claude", "codex"]) {
      const c = agents.get(agent)
      if (!c) { rows.push({ label: AGENT[agent], state: "unknown", detail: w.statusNotUsed }); continue }
      if (c.unavailable) {
        const why = agentProblem(c.unavailable.code, w)
        rows.push({ label: AGENT[agent], state: "bad", detail: `${why} (${c.unavailable.code})` })
        if (!problem) { problem = fill(w.statusAgent, n).replace("{agent}", AGENT[agent]).replace("{why}", why); fix = w.fixAgent }
      } else rows.push({ label: AGENT[agent], state: "ok", detail: c.cli_version })
    }
    return { id: s.machine_id, name: n, rows, problem, fix }
  })
  const failing = machines.find(m => m.problem)
  const usable = machines.filter(m => !m.problem).length
  const summary = failing ? failing.problem : machines.length === 1 ? fill(w.statusOneOk, machines[0].name) : machines.length ? w.statusManyOk.replace("{n}", String(usable)) : ""
  return { ok: !failing, summary, fix: failing?.fix ?? "", machines, withoutBots: withoutBots.map(id => ({ id, name: name(id) })) }
}
