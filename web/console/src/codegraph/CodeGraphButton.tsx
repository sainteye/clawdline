import { useEffect, useState } from "react"
import { readIntegrationSettings } from "../pages/settings/api.js"
import { CODEGRAPH_SETTING, graphWord, openCodeGraph, type CodeGraphTarget } from "./codegraph.js"

export function CodeGraphButton({ target, shown = true }: { target: CodeGraphTarget; shown?: boolean }) {
 const [enabled, setEnabled] = useState(false)
 useEffect(() => {
  if (!shown) {setEnabled(false);return}
  const controller = new AbortController()
  let generation = 0
  const read = () => { const mine = ++generation; void readIntegrationSettings(controller.signal).then(answer => {
   if (mine === generation && !controller.signal.aborted) setEnabled(answer.codegraph_enabled === true)
  }, () => { if (mine === generation && !controller.signal.aborted) setEnabled(false) }) }
  const change = (event: Event) => {generation++;setEnabled((event as CustomEvent<boolean>).detail === true)}
  read(); window.addEventListener("focus", read); window.addEventListener(CODEGRAPH_SETTING, change)
  return () => { controller.abort(); window.removeEventListener("focus", read); window.removeEventListener(CODEGRAPH_SETTING, change) }
 }, [shown, target.id])
 return enabled ? <button className="chip codegraph-open" type="button" onClick={() => openCodeGraph(target)}>{graphWord("open")}</button> : null
}
