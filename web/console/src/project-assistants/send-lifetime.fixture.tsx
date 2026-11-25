/** Production Bots UI and source clients backed by the owned Go fixture. */
import { useSyncExternalStore } from "react"
import { createRoot } from "react-dom/client"
import { Bots } from "./Bots"
import { capturedAssistantRelayFetch, ProjectAssistantRegistry, type AssistantFetch, type AssistantRelayConnection } from "../cloud/project-assistants"
import { bootCatalog } from "../catalog"
import "../legacy/tokens.css"
import "../legacy/base.css"
import "./styles.css"

const query = new URLSearchParams(location.search)
const cloud = query.get("mode") === "cloud"
const language = query.get("lang") ?? "en"
localStorage.setItem("ui_language", language)
await bootCatalog(tag => "/catalog/" + tag + ".json")
const { machine } = await fetch("/fixture.json").then(r => r.json())
const accountKey = cloud ? "fixture-cloud" : "local"
const grant = { accountKey, readerID: "reader", machineID: machine, authorizationRevision: "send-lifetime-1", apiLevel: 112, paired: true, online: true, writable: true }
const carried = ["status", "list", "view", "get", "handoff-card", "read", "send", "send-pictures", "send-receipt", "observed", "attachment"]
  .map(word => "project-assistants." + word)
const call = async (word: string, fields: unknown, signal?: AbortSignal, correlation?: string) => {
  const response = await fetch("/relay/" + word.slice("project-assistants.".length), {
    method: "POST", body: JSON.stringify(fields), signal, headers: correlation ? { "X-Fixture-Correlation": correlation } : {},
  })
  return response.json()
}
const relay = {
  machineDescriptor: () => ({ machine: { commands: carried } }),
  _machineRequest: (_machine, word, fields, _kind, _timeout, options) => call(word, fields, options?.signal),
  _machineRequestAs: (id, _machine, word, fields, _kind, _timeout, options) => call(word, fields, options?.signal, id),
} satisfies AssistantRelayConnection
const transport: AssistantFetch = cloud ? capturedAssistantRelayFetch(machine, () => relay, carried) : (input, init) => fetch(input, init)
const registry = new ProjectAssistantRegistry(accountKey, "reader")
function sync(authorizationRevision: string) {
  registry.sync([{ ...grant, authorizationRevision }], () => transport)
  // This fixture exercises HTTP reads and writes without a roster event stream.
  for (const source of registry.getSnapshot()) source.canRoster = () => false
}
sync(grant.authorizationRevision)
window.addEventListener("fixture-renew-grant", () => sync("send-lifetime-2"))

function LifetimeFixture() {
  const sources = useSyncExternalStore(registry.subscribe, registry.getSnapshot)
  return <Bots sources={sources} viewState={registry.viewState} language={language} accountKey={accountKey} readerID="reader" onClose={() => {}} manageHistory />
}
createRoot(document.querySelector("#app")!).render(<LifetimeFixture />)
