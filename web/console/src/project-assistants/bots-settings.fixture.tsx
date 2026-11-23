import { createRoot } from "react-dom/client"
import { useState } from "react"
import { bootCatalog } from "../catalog"
import { BotsSettingsModal, type SharedSettingsReply } from "./BotsSettingsModal"
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
const fake: typeof fetch = async (_url, init) => {
  if (init?.method === "PUT") {
    const body = JSON.parse(String(init.body)) as { settings: SharedSettingsReply["settings"] }
    reply.settings = { ...body.settings, version: reply.settings.version + 1 }
    ;(window as unknown as { saved: unknown }).saved = reply.settings
  }
  return new Response(JSON.stringify(reply), { status: 200, headers: { "Content-Type": "application/json" } })
}
const projects = [{ id: "p-web", name: "Sample Web" }, { id: "p-api", name: "Sample API" }, { id: "p-docs", name: "Sample Docs" }]

function App() {
  const [open, setOpen] = useState(false)
  return <div className="project-assistants" style={{ position: "relative", height: "100%" }}>
    <button id="opener" type="button" onClick={() => setOpen(true)}>open</button>
    {open && <BotsSettingsModal projects={projects} fetcher={fake} onClose={() => setOpen(false)}/>}
  </div>
}

export function mountFixture() {
  void bootCatalog(tag => `/catalog/${tag}.json`).then(() => createRoot(document.getElementById("fixture")!).render(<App/>))
}
