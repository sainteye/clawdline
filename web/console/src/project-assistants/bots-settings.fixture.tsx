import { createRoot } from "react-dom/client"
import { useState } from "react"
import { bootCatalog } from "../catalog"
import { BotsSettingsModal, type SharedSettingsReply } from "./BotsSettingsModal"
import type { SourceClient } from "./client"
import "./styles.css"

// Synthetic data only: fake Project names, no daemon.
const reply: SharedSettingsReply = {
  settings: { version: 3, rules: [
    { id: "code", kind: "寫程式", hint: "一般 Session 寫程式", agents: [{ agent: "codex", effort: "xhigh" }], scope: { all: true } },
    { id: "image", kind: "生圖", agents: [{ agent: "codex" }, { agent: "antigravity", effort: "high" }, { agent: "grok" }], scope: { all: false, projects: ["p-web"] } },
  ], memory: [{ id: "m1", text: "回答先講結論，再補細節。", scope: { all: true } }] },
  agents: [{ agent: "claude", efforts: [] }, { agent: "codex", efforts: ["high", "xhigh"] }, { agent: "antigravity", efforts: ["low", "medium", "high", "xhigh", "max"] }],
  limits: { rules: 12, memory_entries: 20 },
}
// A synthetic source in place of a machine, as a Cloud or local console passes one.
const answer = (machine: string) => ({ contract_version: "project-assistants.v1" as const, machine_id: machine, ...reply })
const fakeSource = (machine: string, older = false) => ({
  machineID: machine, authorizationRevision: "fixture",
  async sharedSettings() { if (older) throw { contract_version: "project-assistants.v1", error: { code: "pa_unsupported", field: "", retry_after_seconds: 0 } }; return answer(machine) },
  async saveSharedSettings(r: { settings: SharedSettingsReply["settings"] }) {
    reply.settings = { ...r.settings, version: reply.settings.version + 1 }
    ;(window as unknown as { saved: unknown }).saved = reply.settings
    return answer(machine)
  },
}) as unknown as SourceClient
const several = new URLSearchParams(location.search).has("machines")
const machines = several ? [{ source: fakeSource("m-studio"), name: "Studio" }, { source: fakeSource("m-laptop", true), name: "Laptop" }] : [{ source: fakeSource("m-studio"), name: "Studio" }]
const projects = [{ id: "p-web", name: "Sample Web" }, { id: "p-api", name: "Sample API" }, { id: "p-docs", name: "Sample Docs" }]

function App() {
  const [open, setOpen] = useState(false)
  return <div className="project-assistants" style={{ position: "relative", height: "100%" }}>
    <button id="opener" type="button" onClick={() => setOpen(true)}>open</button>
    {open && <BotsSettingsModal projects={projects} machines={machines} onClose={() => setOpen(false)}/>}
  </div>
}

export function mountFixture() {
  void bootCatalog(tag => `/catalog/${tag}.json`).then(() => createRoot(document.getElementById("fixture")!).render(<App/>))
}
