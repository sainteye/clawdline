import { createRoot } from "react-dom/client"
import { useState } from "react"
import { bootCatalog } from "../catalog"
import { BotsSettingsModal, type SharedSettingsReply } from "./BotsSettingsModal"
import type { SourceClient } from "./client"
import type * as W from "@clawdline/contract"
import "./styles.css"

// Synthetic data only: fake Project names, no daemon.
const reply: SharedSettingsReply = {
  settings: { version: 3, default: { agent: "claude" }, rules: [
    { id: "code", kind: "寫程式", hint: "一般 Session 寫程式", agents: [{ agent: "codex", effort: "xhigh" }], scope: { all: true } },
    { id: "image", kind: "生圖", agents: [{ agent: "codex" }, { agent: "antigravity", effort: "high" }, { agent: "grok" }], scope: { all: false, projects: ["p-web"] } },
  ], memory: [{ id: "m1", text: "回答先講結論，再補細節。", scope: { all: true } }] },
  agents: [{ agent: "claude", efforts: [] }, { agent: "codex", efforts: ["high", "xhigh"] }, { agent: "antigravity", efforts: ["low", "medium", "high", "xhigh", "max"] }],
  limits: { rules: 12, memory_entries: 20 },
}
// A synthetic source in place of a machine, as a Cloud or local console passes one.
const answer = (machine: string) => ({ contract_version: "project-assistants.v1" as const, machine_id: machine, ...reply })
// Synthetic model catalogs shaped like each ACP adapter's (Codex lists one entry per effort).
const catalogs: Record<string, { current: string; choices: { value: string; name: string; description?: string }[] }> = {
  claude: { current: "default", choices: [{ value: "default", name: "Default (recommended)", description: "Opus" }, { value: "opus", name: "Opus" }, { value: "sonnet", name: "Sonnet" }, { value: "haiku", name: "Haiku" }] },
  codex: { current: "model-a/high", choices: [{ value: "model-a/high", name: "model-a (high)" }, { value: "model-a/xhigh", name: "model-a (xhigh)" }, { value: "model-b-mini/high", name: "model-b-mini (high)" }] },
  antigravity: { current: "default", choices: [{ value: "default", name: "Default", description: "Pro" }, { value: "flash", name: "Flash" }] },
}
const discovery = new URLSearchParams(location.search)
const delay = Number(discovery.get("delay") ?? 600)
let geminiFailures = discovery.has("geminiFails") ? 1 : 0
;(window as unknown as { asked: string[] }).asked = []
const fakeSource = (machine: string, older = false) => ({
  machineID: machine, authorizationRevision: "fixture",
  async sharedSettings() { if (older) throw { contract_version: "project-assistants.v1", error: { code: "pa_unsupported", field: "", retry_after_seconds: 0 } }; return answer(machine) },
  async configuration(_key: unknown, _conversation: string, agent: string) {
    ;(window as unknown as { asked: string[] }).asked.push(agent)
    await new Promise(r => setTimeout(r, delay))
    if (agent === "antigravity" && geminiFailures-- > 0) throw { contract_version: "project-assistants.v1", error: { code: "pa_unavailable", field: "", retry_after_seconds: 0 } }
    const c = catalogs[agent] ?? { current: "", choices: [] }
    return { contract_version: "project-assistants.v1", catalog: { agent, model: "", options: [{ id: "model", name: "Model", category: "model", current: c.current, choices: c.choices }] } }
  },
  async saveSharedSettings(r: { settings: SharedSettingsReply["settings"] }) {
    reply.settings = { ...r.settings, version: reply.settings.version + 1 }
    ;(window as unknown as { saved: unknown }).saved = reply.settings
    return answer(machine)
  },
}) as unknown as SourceClient
const several = new URLSearchParams(location.search).has("machines")
const machines = several ? [{ source: fakeSource("m-studio"), name: "Studio" }, { source: fakeSource("m-laptop", true), name: "Laptop" }] : [{ source: fakeSource("m-studio"), name: "Studio" }]
const projects = [{ id: "p-web", name: "Sample Web" }, { id: "p-api", name: "Sample API" }, { id: "p-docs", name: "Sample Docs" }]
const catalogBots = new Map(machines.map(m => [m.source.machineID, { key: { machine_id: m.source.machineID, bot_id: "lead" }, conversation_id: "c1", lead: true } as unknown as W.ProjectAssistantBot]))

function App() {
  const [open, setOpen] = useState(false)
  return <div className="project-assistants" style={{ position: "relative", height: "100%" }}>
    <button id="opener" type="button" onClick={() => setOpen(true)}>open</button>
    {open && <BotsSettingsModal projects={projects} machines={machines} catalogBots={catalogBots} onClose={() => setOpen(false)}/>}
  </div>
}

export function mountFixture() {
  void bootCatalog(tag => `/catalog/${tag}.json`).then(() => createRoot(document.getElementById("fixture")!).render(<App/>))
}
