import { useCallback, useEffect, useRef, useState } from "react"
import { curatedPackageIDs } from "./Artwork.js"
import { installedPackages, installPackage, officialUpdatePolicy } from "./store.js"
import { PluginError, newerStableVersion } from "./package.js"
import { automaticUpdateAllowed, downloadOfficialRelease, isOfficialRevision, readOfficialCatalog, whileViewsIdle, type OfficialCatalog } from "./updates.js"

export type UpdateState = "current" | "available" | "updating" | "waitingClose" | "reviewRequired" | "updateFailed"
export function useViewUpdates(blocked: boolean) {
  const [catalog, setCatalog] = useState<OfficialCatalog | null>(null)
  const [states, setStates] = useState<Record<string, UpdateState>>({})
  const [checking, setChecking] = useState(false), [failure, setFailure] = useState("")
  const live = useRef({ blocked }); live.current = { blocked }
  const life = useRef<AbortController | null>(null), running = useRef<Promise<OfficialCatalog | null> | null>(null)
  const check = useCallback((automatic = true): Promise<OfficialCatalog | null> => {
    if (running.current) return running.current
    const signal = life.current?.signal
    if (!signal || signal.aborted) return Promise.resolve(null)
    const wasBlocked = live.current.blocked
    const task = async () => {
      setChecking(true); setFailure("")
      try {
        const next = await readOfficialCatalog(curatedPackageIDs, signal)
        if (signal.aborted) return null
        setCatalog(next)
        for (const initial of await installedPackages()) {
          if (signal.aborted) break
          const release = next.packages.find(item => item.id === initial.id)
          if (!release) continue
          const newer = newerStableVersion(release.version, initial.active.manifest.version)
          const set = (state: UpdateState) => { if (!signal.aborted) setStates(old => ({ ...old, [initial.id]: state })) }
          set(newer ? "available" : "current")
          if (!automatic || !isOfficialRevision(initial, release) || (initial.official && !initial.autoUpdate)) continue
          if (live.current.blocked) { if (newer) set("waitingClose"); continue }
          let row = initial
          if (!row.official) {
            const adopted = await whileViewsIdle(async () => { if (signal.aborted || live.current.blocked) return false; await officialUpdatePolicy(row, true) })
            if (!adopted) { if (newer) set("waitingClose"); continue }
            row = (await installedPackages()).find(item => item.id === row.id) ?? row
          }
          if (!newer) continue
          if (!automaticUpdateAllowed(row, release)) { set("reviewRequired"); continue }
          set("updating")
          try {
            const value = await downloadOfficialRelease(release, signal)
            if (signal.aborted) break
            const applied = await whileViewsIdle(async () => {
              if (live.current.blocked || signal.aborted) return false
              await installPackage(value.archive, row.active.digest, { official: true, automatic: true })
            })
            set(applied && !live.current.blocked ? "current" : "waitingClose")
          } catch (error) { if (!signal.aborted) set(error instanceof PluginError && error.code === "update_review_required" ? "reviewRequired" : "updateFailed") }
        }
        return next
      } catch (error) {
        if (!signal.aborted) setFailure(error instanceof PluginError ? error.code : "update_check_failed")
        return null
      } finally {
        if (!signal.aborted) setChecking(false)
      }
    }
    const result = task().finally(() => {
      if (running.current === result) {
        running.current = null
        if (wasBlocked && !live.current.blocked && !signal.aborted) void check()
      }
    })
    running.current = result; return result
  }, [])
  useEffect(() => {
    const controller = new AbortController(); life.current = controller
    const foreground = () => { if (document.visibilityState === "visible") void check() }
    foreground()
    window.addEventListener("focus", foreground); document.addEventListener("visibilitychange", foreground)
    return () => { controller.abort(); running.current = null; window.removeEventListener("focus", foreground); document.removeEventListener("visibilitychange", foreground) }
  }, [check])
  useEffect(() => { if (!blocked) void check() }, [blocked, check])
  return { catalog, states, checking, failure, check }
}
