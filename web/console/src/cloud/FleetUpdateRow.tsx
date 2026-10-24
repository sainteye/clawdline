import { useEffect, useState } from "react"
import type { CloudMachine } from "./copied.js"
import type { CloudReadClient } from "./relay-reader.js"
import { nextWord } from "../next-strings.js"
import { fleetUpdateFacts, readFleetUpdate } from "./fleet-update.js"
import { applyMoving, createUpdateReadStore, UPDATE_FOLLOW_EVERY_MS, type UpdateRead } from "../machine/update-model.js"
import "./fleet-update.css"

/** Version, update availability, compatibility and last result for one machine. */
export function FleetUpdateRow({ client, machine, appVersion }: {
  client: CloudReadClient | null
  machine: CloudMachine
  appVersion?: string
}) {
  const [read, setRead] = useState<UpdateRead | null>(null)
  useEffect(() => {
    setRead(null)
    if (!client || machine.pairing !== "paired" || machine.freshness !== "current") return
    let active = true
    const store = createUpdateReadStore(
      (fresh) => readFleetUpdate(client, machine.id, undefined, fresh),
      {
        now: () => Date.now(), visible: () => document.visibilityState !== "hidden",
        onVisibilityChange: (fn) => { document.addEventListener("visibilitychange", fn); return () => document.removeEventListener("visibilitychange", fn) },
        onFocus: (fn) => { window.addEventListener("focus", fn); return () => window.removeEventListener("focus", fn) },
        onPageShow: (fn) => { const restored = (event: PageTransitionEvent) => { if (event.persisted) fn() }; window.addEventListener("pageshow", restored); return () => window.removeEventListener("pageshow", restored) },
        onBlur: (fn) => { window.addEventListener("blur", fn); return () => window.removeEventListener("blur", fn) },
        onOnline: (fn) => { window.addEventListener("online", fn); return () => window.removeEventListener("online", fn) },
        setInterval: (fn, ms) => setInterval(fn, ms), clearInterval: (timer) => clearInterval(timer),
      },
    )
    const stop = store.subscribe(() => { if (active) setRead(store.current()) })
    const follow = setInterval(() => {
      const current = store.current()
      if (current?.kind === "status" && applyMoving(current.status)) void store.refresh()
    }, UPDATE_FOLLOW_EVERY_MS)
    return () => {
      active = false
      stop()
      clearInterval(follow)
    }
  }, [client, machine.id, machine.pairing, machine.freshness])

  const facts = fleetUpdateFacts(machine, appVersion, read)
  const compatible = facts.compatibility === "supported" ? nextWord("fleetUpdateCompatible")
    : facts.compatibility === "older" ? nextWord("fleetUpdateOlder")
      : facts.compatibility === "offline" ? nextWord("fleetUpdateOffline")
        : facts.compatibility === "unpaired" ? nextWord("fleetUpdateUnpaired")
          : read?.kind === "unreachable" ? nextWord("fleetUpdateUnanswered")
            : read?.kind === "refused" ? nextWord("fleetUpdateRefused")
            : nextWord("fleetUpdateChecking")
  const state = facts.state === "update_available" && facts.latest
    ? nextWord("fleetUpdateAvailable", { version: facts.latest })
    : facts.state === "current" ? nextWord("fleetUpdateCurrent")
      : facts.state === "ahead" ? nextWord("fleetUpdateAhead")
        : facts.state === "unknown" || facts.state === "differs" ? nextWord("fleetUpdateUnknown")
          : null
  const result = facts.result === "healthy" && facts.to
    ? nextWord("fleetUpdateHealthy", { version: facts.to })
    : facts.result === "rolled_back"
      ? nextWord("fleetUpdateRolledBack", { code: facts.errorCode || "unknown" })
      : facts.result === "failed"
        ? nextWord("fleetUpdateFailed", { code: facts.errorCode || "unknown" })
        : facts.result && facts.result !== "idle"
          ? nextWord("fleetUpdateMoving")
          : null

  // A successful update to the running, current version repeats the version row.
  // Keep failures, rollbacks, in-flight work and other historical results visible.
  const showResult = result && !(facts.result === "healthy" && facts.state === "current" && facts.to === facts.running)

  return (
    <dl className="cloud-fleet-update cloud-machine-details" data-compatibility={facts.compatibility} data-update-result={facts.result ?? undefined}>
      <div>
        <dt>{nextWord(machine.freshness === "current" ? "fleetVersion" : "fleetLastVersion")}</dt>
        <dd>
          <span>{facts.running || nextWord("updateUnknownVersion")}</span>
          {state && <span>{state}</span>}
        </dd>
      </div>
      {facts.latest && facts.latest !== facts.running && <div><dt>{nextWord("updateLatest")}</dt><dd>{facts.latest}</dd></div>}
      <div><dt>{nextWord("fleetCompatibility")}</dt><dd>{compatible}</dd></div>
      {showResult && <div>
        <dt>{nextWord("cloudMachineUpdateResultLabel")}</dt>
        <dd className={facts.result === "rolled_back" || facts.result === "failed" ? "cloud-fleet-update-problem" : ""}>{result}</dd>
      </div>}
    </dl>
  )
}
