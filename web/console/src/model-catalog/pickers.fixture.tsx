import { createRoot } from "react-dom/client"
import { useState } from "react"
import { bootCatalog } from "../catalog"
import { DefaultModelsBlock } from "../pages/settings/DefaultModelsBlock"
import { WorkModelChoice, type LaunchModelChoice } from "../pages/work/WorkModelChoice"
import "../legacy/tokens.css"
import "../legacy/base.css"
import "../pages/work/work.css"
import "../ui-language/choice-menu.css"

// Synthetic: the daemon behind this page is pickers.e2e.mjs's server, which
// answers /v1/model-catalog and /v1/settings/default-models with made-up models.
function App() {
  const [choice, setChoice] = useState<LaunchModelChoice | null>(null)
  return <div className="fixture-pickers">
    <section id="settings" className="settings-pane"><DefaultModelsBlock shown /></section>
    <section id="board"><WorkModelChoice id="work-models-fixture" assistant="codex" choice={choice} disabled={false} onPick={setChoice} /></section>
  </div>
}

export function mountFixture() {
  void bootCatalog(tag => `/catalog/${tag}.json`).then(() => createRoot(document.getElementById("fixture")!).render(<App />))
}
