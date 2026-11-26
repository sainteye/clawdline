import { createRoot } from "react-dom/client"
import { useState } from "react"
import { bootCatalog } from "../catalog"
import { BotsSettingsModal, type SharedSettingsReply } from "./BotsSettingsModal"
import type { SourceClient } from "./client"
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
// Synthetic model catalogs as the daemon's GET /v1/model-catalog answers them.
type Row = { agent: string; models: { value: string; name: string; description?: string }[]; default_name: string; efforts: string[]; source: "native" | "agent"; observed_at: string; refreshing: boolean }
const row = (agent: string, default_name: string, models: string[], refreshing = false, source: Row["source"] = "agent"): Row =>
  ({ agent, default_name, models: models.map(m => ({ value: m.toLowerCase(), name: m })), efforts: [], source, observed_at: "2026-11-25T00:00:00Z", refreshing })
const discovery = new URLSearchParams(location.search)
const delay = Number(discovery.get("delay") ?? 600)
// The first read fails once with ?fails, so the list shows its retry.
let failures = discovery.has("fails") ? 1 : 0
const started = Date.now()
;(window as unknown as { asked: number }).asked = 0
const fakeSource = (machine: string, older = false) => ({
  machineID: machine, authorizationRevision: "fixture",
  async sharedSettings() { if (older) throw { contract_version: "project-assistants.v1", error: { code: "pa_unsupported", field: "", retry_after_seconds: 0 } }; return answer(machine) },
  canModelCatalog: () => !older,
  async modelCatalog() {
    ;(window as unknown as { asked: number }).asked++
    await new Promise(r => setTimeout(r, delay))
    if (failures-- > 0) throw { contract_version: "project-assistants.v1", error: { code: "pa_offline", field: "", retry_after_seconds: 0 } }
    // Codex has no native list here: the daemon is still asking it for three seconds.
    const codexDone = Date.now() - started > 3000
    return { agents: [
      row("claude", "Opus", ["Opus", "Sonnet", "Haiku"]),
      codexDone ? row("codex", "model-a", ["model-a", "model-b-mini"]) : row("codex", "", [], true, "native"),
      row("antigravity", "Pro", ["Flash"]),
      row("grok", "", [], false, "native"),
    ] }
  },
  async saveSharedSettings(r: { settings: SharedSettingsReply["settings"] }) {
    reply.settings = { ...r.settings, version: reply.settings.version + 1 }
    ;(window as unknown as { saved: unknown }).saved = reply.settings
    return answer(machine)
  },
}) as unknown as SourceClient
const several = new URLSearchParams(location.search).has("machines")
const machines = several ? [{ source: fakeSource("m-studio"), name: "Studio" }, { source: fakeSource("m-laptop", true), name: "Laptop" }] : [{ source: fakeSource("m-studio"), name: "Studio" }]
// ?projects=many: a machine with many Projects, some with long names, as the scope list sees them.
const projects = discovery.has("projects")
  ? Array.from({ length: 22 }, (_, i) => ({ id: `p-${i}`, name: i % 3 ? `Sample Project ${i + 1}` : `Sample Project With A Rather Long Descriptive Name ${i + 1}` }))
  : [{ id: "p-web", name: "Sample Web" }, { id: "p-api", name: "Sample API" }, { id: "p-docs", name: "Sample Docs" }]

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
