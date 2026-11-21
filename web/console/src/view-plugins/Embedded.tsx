import { useEffect, useState, useSyncExternalStore } from "react"
import type { ViewSource } from "./bridge.js"
import { PluginError, type ViewPackage } from "./package.js"
import { installedPackages, subscribePackages, type Installed } from "./store.js"
import { PLUGIN_LIMITS as B } from "./limits.js"
import { PluginFrame, readPluginSource, type ViewGrant } from "./Frame.js"
import { pluginWords } from "./words.js"
import { packageCover } from "./Artwork.js"
import { LoadingSkeleton } from "../LoadingSkeleton.js"

// The person's pinned view is a per-browser convenience, like the Bots tower
// choice. Packages already live in this browser only, so the pin does too.
const preference = "clawdline.views.pinned"
const listeners = new Set<() => void>()
let fallback: string | null = null
function readPinned(): string | null {
  try { return localStorage.getItem(preference) } catch { return fallback }
}
/** Pin a view beside the Session list, or give the pane back with null. */
export function pinView(id: string | null) {
  fallback = id
  try { if (id) localStorage.setItem(preference, id); else localStorage.removeItem(preference) } catch { /* This page keeps the choice. */ }
  for (const fn of listeners) fn()
}
function subscribePinned(fn: () => void) {
  listeners.add(fn); window.addEventListener("storage", fn)
  return () => { listeners.delete(fn); window.removeEventListener("storage", fn) }
}
export function usePinnedView(): string | null { return useSyncExternalStore(subscribePinned, readPinned) }

// The same breakpoint as the legacy shell: below it there is no second column.
const desk = "(min-width: 900px)"
function subscribeDesk(fn: () => void) { const q = matchMedia(desk); q.addEventListener("change", fn); return () => q.removeEventListener("change", fn) }
export function useDesk(): boolean { return useSyncExternalStore(subscribeDesk, () => matchMedia(desk).matches) }

export const packageName = (p: ViewPackage, language: string) =>
  p.manifest.name[language] || p.manifest.name[language.toLowerCase().startsWith("zh") ? "zh-Hant" : "en"] || p.manifest.name.en

/**
 * The pinned view in the Session pane. It stays mounted while a Session is open
 * so returning shows the same scene; hidden, it tells the package it is not visible.
 * A removed or disabled package gives the pane back to the ordinary home screen.
 */
export function EmbeddedView({ source, language, pinned, hidden, onFullscreen }: {
  source: ViewSource; language: string; pinned: string; hidden: boolean
  onFullscreen: (button: HTMLElement, pkg: ViewPackage) => void
}) {
  const w = pluginWords(language)
  const [rows, setRows] = useState<Installed[]>([])
  const [grant, setGrant] = useState<ViewGrant | null>(null)
  const [error, setError] = useState("")
  const [attempt, setAttempt] = useState(0)
  useEffect(() => subscribePackages(() => setAttempt(n => n + 1)), [])
  useEffect(() => {
    const control = new AbortController()
    let alive = true
    setGrant(null); setError("")
    void (async () => {
      try {
        const installed = await installedPackages()
        if (!alive) return
        setRows(installed)
        const row = installed.find(r => r.id === pinned && r.enabled)
        if (!row) { pinView(null); return }
        const initial = await readPluginSource(source, control.signal)
        if (!alive) return
        if (initial.sessions.length > B.sessions) throw new PluginError("too_many_sessions")
        setGrant({ pkg: row.active, sourceKey: source.key, initial, instanceId: crypto.randomUUID() })
      } catch (e) { if (alive) setError(e instanceof PluginError ? e.code : "host_unavailable") }
    })()
    return () => { alive = false; control.abort() }
    // A fresh authorized reading on every pin, scope or registry change.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pinned, source.key, attempt])
  const enabled = rows.filter(r => r.enabled), current = enabled.find(r => r.id === pinned)
  const cover = current && packageCover(current.active)
  return <section className="vp-pane" hidden={hidden} aria-label={current ? packageName(current.active, language) : w.launcher}>
    <header className="vp-pane-bar">
      {cover ? <img className="vp-pane-cover" src={cover} alt="" /> : <span className="vp-pane-cover" aria-hidden="true" />}
      {enabled.length > 1 ? <label className="vp-pane-switch">
        <span className="vp-sr">{w.switchView}</span>
        <select value={pinned} onChange={e => pinView(e.currentTarget.value)}>
          {enabled.map(r => <option key={r.id} value={r.id}>{packageName(r.active, language)}</option>)}
        </select>
      </label> : <h2 className="vp-pane-title">{current ? packageName(current.active, language) : ""}</h2>}
      <div className="vp-pane-actions">
        <button type="button" disabled={!current} onClick={e => current && onFullscreen(e.currentTarget, current.active)}>
          <span aria-hidden="true">⤢</span>{w.fullscreen}
        </button>
        <button type="button" className="vp-pane-unpin" aria-label={w.unpin} title={w.unpin} onClick={() => pinView(null)}>✕</button>
      </div>
    </header>
    <div className="vp-pane-body">
      {grant ? <PluginFrame key={grant.instanceId} grant={grant} source={source} language={language} presentation="embedded" hidden={hidden}
        onNavigate={fn => { try { fn() } catch { /* A revoked source never navigates. */ } }}
        // A package's own close (its button, its Escape, or the one after opening a
        // Session) leaves the pane alone; only the header's remove button unpins.
        onClose={() => {}}
        onFault={code => { setGrant(null); setError(code) }} />
        : error ? <div className="vp-pane-error" role="alert"><p>{w.errors[error] || w.unavailable}</p>
          <button type="button" onClick={() => setAttempt(n => n + 1)}>{w.retry}</button></div>
        : <div className="vp-pane-loading"><LoadingSkeleton label={w.loading} id="view-pane" kind="detail" /></div>}
    </div>
  </section>
}
