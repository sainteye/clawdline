import { useEffect, useState } from "react"
import { readIntegrationSettings } from "../pages/settings/api.js"
import { GRAPHIFY_SETTING, graphWord, openGraphify, type GraphifyTarget } from "./graphify.js"

export function GraphifyButton({ target, shown = true }: { target: GraphifyTarget; shown?: boolean }) {
 const [enabled, setEnabled] = useState(false)
 useEffect(() => {
  if (!shown) {setEnabled(false);return}
  const controller = new AbortController()
  let generation = 0
  const read = () => { const mine = ++generation; void readIntegrationSettings(controller.signal).then(answer => {
   if (mine === generation && !controller.signal.aborted) setEnabled(answer.graphify_enabled === true)
  }, () => { if (mine === generation && !controller.signal.aborted) setEnabled(false) }) }
  const change = (event: Event) => {generation++;setEnabled((event as CustomEvent<boolean>).detail === true)}
  read(); window.addEventListener("focus", read); window.addEventListener(GRAPHIFY_SETTING, change)
  return () => { controller.abort(); window.removeEventListener("focus", read); window.removeEventListener(GRAPHIFY_SETTING, change) }
 }, [shown, target.id])
 return enabled ? <button className="chip graphify-open" type="button" onClick={() => openGraphify(target)}>{graphWord("open")}</button> : null
}
