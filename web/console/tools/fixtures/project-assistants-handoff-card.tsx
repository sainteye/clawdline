import React from "react"
import {createRoot} from "react-dom/client"
import {Bots} from "../../src/project-assistants/Bots"
import {ProjectAssistantSourceClient} from "../../src/cloud/project-assistants"
import {bootCatalog} from "../../src/catalog"
import "../../src/project-assistants/styles.css"
import "../../src/legacy/tokens.css"

declare const FIXTURE_MACHINE: string, FIXTURE_BOT: string
const query = new URL(location.href).searchParams
const language = query.get("lang") ?? "en"
localStorage.setItem("ui_language", language)
await bootCatalog(tag => "/catalog/" + tag + ".json")
const grant = {accountKey: "synthetic-account", readerID: "synthetic-reader", machineID: FIXTURE_MACHINE,
  authorizationRevision: "synthetic-grant", apiLevel: Number(query.get("api") ?? "93"), paired: true, online: true, writable: true}
const source = new ProjectAssistantSourceClient(grant, (input, init) => fetch(input, init), () => grant)
source.canRoster = () => false
const key = {machine_id: FIXTURE_MACHINE, bot_id: FIXTURE_BOT}
const bot = await source.get(key, new AbortController().signal)
history.replaceState(null, "", location.pathname + location.search + "#" + new URLSearchParams({bot: key.bot_id, conversation: bot.conversation.id, machine: key.machine_id}))
createRoot(document.getElementById("app")!).render(<Bots sources={[source]} accountKey={grant.accountKey} readerID={grant.readerID} language={language} onClose={() => {}} />)
