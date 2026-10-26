import { useEffect, useState } from "react"
import type { CloudMachine } from "./copied.js"
import type { CloudReadClient } from "./relay-reader.js"
import { nextWord } from "../next-strings.js"
import { UPDATE_READ_EVERY_MS, devStrip, updateBannerVersion, type UpdateRead } from "../machine/update-model.js"
import { readFleetUpdate } from "./fleet-update.js"
import "../machine/update.css"

function dismissedKey(machine: string): string {
  return `clawdline.cloud-update-banner.dismissed.${machine}`
}

function dismissedVersion(machine: string): string | null {
  try { return sessionStorage.getItem(dismissedKey(machine)) } catch { return null }
}

/** The all-machine list keeps update notices except dev builds ahead of the release. */
export function FleetUpdateNotice({ client, machine, onChoose }: {
  client: CloudReadClient | null
  machine: CloudMachine
  onChoose: (machine: CloudMachine) => void
}) {
  const [read, setRead] = useState<UpdateRead | null>(null)
  const [dismissed, setDismissed] = useState(() => dismissedVersion(machine.id))

  useEffect(() => {
    setRead(null)
    if (!client || machine.pairing !== "paired" || machine.freshness !== "current") return
    const controller = new AbortController()
    let checking = false
    const refresh = async () => {
      if (document.visibilityState === "hidden" || checking) return
      checking = true
      try {
        const answer = await readFleetUpdate(client, machine.id, controller.signal, true)
        if (!controller.signal.aborted) setRead(answer)
      } finally {
        checking = false
      }
    }
    const visible = () => { if (document.visibilityState !== "hidden") void refresh() }
    void refresh()
    const timer = window.setInterval(() => void refresh(), UPDATE_READ_EVERY_MS)
    document.addEventListener("visibilitychange", visible)
    window.addEventListener("focus", visible)
    return () => {
      controller.abort()
      window.clearInterval(timer)
      document.removeEventListener("visibilitychange", visible)
      window.removeEventListener("focus", visible)
    }
  }, [client, machine.id, machine.pairing, machine.freshness])

  const version = updateBannerVersion(read?.kind === "status" ? read.status : null, dismissed)
  const dev = devStrip(read?.kind === "status" ? read.status : null, true, nextWord)
  if (dev?.relation === "ahead") return null
  if (!version && !dev) return null
  const name = machine.name || machine.label || machine.id
  return <div className="update-strip cloud-fleet-update-notice" role="status" data-machine={machine.id} data-version={version ?? undefined} data-dev={dev?.relation}>
    <span className={"update-strip-tag" + (dev ? "" : " is-new")}>{nextWord(dev ? "updateStripTagDev" : "updateStripTagNew")}</span>
    <span className="update-strip-say">{dev ? <>{name} · <b className="update-strip-commit">{dev.commit}</b>{dev.standing ? <> · {dev.standing}</> : null}</> : nextWord("cloudFleetUpdateNotice", { machine: name, version: version! })}</span>
    <button className="update-strip-link is-go" type="button" title={nextWord("cloudFleetUpdateOpen")}
      aria-label={nextWord("cloudFleetUpdateOpen")} onClick={() => onChoose(machine)}>
      {nextWord("updateStripOpen")}
    </button>
    {!dev && <button className="update-strip-x" type="button" aria-label={nextWord("updateBannerDismiss")} title={nextWord("updateBannerDismiss")}
      onClick={() => {
        setDismissed(version!)
        try { sessionStorage.setItem(dismissedKey(machine.id), version!) } catch { /* this view only */ }
      }}>×</button>}
  </div>
}
