import { useEffect, useRef, useState, useSyncExternalStore, type KeyboardEvent, type RefObject } from "react"
import type { ViewSource } from "./bridge.js"
import { PluginError, type ViewPackage } from "./package.js"
import { installedPackages, subscribePackages, type Installed } from "./store.js"
import { PLUGIN_LIMITS as B } from "./limits.js"
import { PluginFrame, readPluginSource, type ViewGrant } from "./Frame.js"
import { pluginWords, viewHint } from "./words.js"
import { packageCover } from "./Artwork.js"

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
export function EmbeddedView({ source, language, pinned, hidden, onFullscreen, onBrowse, onManage }: {
  source: ViewSource; language: string; pinned: string; hidden: boolean
  onFullscreen: (button: HTMLElement, pkg: ViewPackage) => void
  onBrowse: (button: HTMLElement) => void; onManage: (button: HTMLElement) => void
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
  const cover = current && packageCover(current.active), title = current ? packageName(current.active, language) : ""
  const trigger = useRef<HTMLButtonElement>(null)
  return <section className="vp-pane" hidden={hidden} aria-label={title || w.launcher}>
    <header className="vp-pane-bar">
      <ViewSwitch language={language} rows={enabled} pinned={pinned} trigger={trigger}
        onBrowse={onBrowse} onManage={onManage} />
      <div className="vp-pane-actions">
        <button type="button" disabled={!grant || !current} onClick={e => current && onFullscreen(e.currentTarget, current.active)}>
          <FullIcon />{w.fullscreen}
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
        : <div className="vp-pane-state" data-state={error ? "error" : "loading"} role={error ? "alert" : "status"}>
          {cover ? <img className="vp-pane-state-cover" src={cover} alt="" /> : <span className="vp-pane-state-cover" aria-hidden="true" />}
          {error ? <>
            <h3>{w.paneFailed.replace("{name}", title || w.launcher)}</h3>
            <p>{w.errors[error] || w.unavailable}<br />{w.othersSafe}</p>
            <div className="vp-pane-state-actions">
              <button type="button" className="vp-pane-primary" onClick={() => setAttempt(n => n + 1)}>{w.reload}</button>
              {enabled.length > 1 && <button type="button" onClick={() => trigger.current?.click()}>{w.otherView}</button>}
            </div>
          </> : <>
            <h3>{w.opening.replace("{name}", title)}</h3>
            <p>{w.openingHint}</p>
            <span className="vp-pane-progress" aria-hidden="true"><i /></span>
          </>}
        </div>}
    </div>
  </section>
}

export function FullIcon() {
  return <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true"><path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5" /></svg>
}
export function DockIcon({ on }: { on: boolean }) {
  return <svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true">
    <rect x="3" y="4" width="18" height="16" rx="3" /><path d="M14 4v16" />{on && <path d="M14 4h4a3 3 0 0 1 3 3v10a3 3 0 0 1-3 3h-4z" fill="currentColor" stroke="none" />}
  </svg>
}

/**
 * The pane's view picker: a menu of enabled views with their covers and a line about
 * each, then the way to every view and to Manage. The browser's own select cannot
 * show a picture, and its open list looks different on every system.
 */
function ViewSwitch({ language, rows, pinned, trigger, onBrowse, onManage }: {
  language: string; rows: Installed[]; pinned: string; trigger: RefObject<HTMLButtonElement | null>
  onBrowse: (button: HTMLElement) => void; onManage: (button: HTMLElement) => void
}) {
  const w = pluginWords(language)
  const [open, setOpen] = useState(false)
  const menu = useRef<HTMLDivElement>(null)
  const current = rows.find(r => r.id === pinned), cover = current && packageCover(current.active)
  const items = () => [...(menu.current?.querySelectorAll<HTMLElement>("[role^=menuitem]") ?? [])]
  const shut = (focus = true) => { setOpen(false); if (focus) trigger.current?.focus({ preventScroll: true }) }
  useEffect(() => {
    if (!open) return
    const selected = items().find(el => el.getAttribute("aria-checked") === "true") ?? items()[0]
    selected?.focus({ preventScroll: true })
    const outside = (e: PointerEvent) => {
      const target = e.target as Node
      if (!menu.current?.contains(target) && !trigger.current?.contains(target)) shut(false)
    }
    document.addEventListener("pointerdown", outside, true)
    return () => document.removeEventListener("pointerdown", outside, true)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])
  const key = (e: KeyboardEvent) => {
    const all = items(), at = all.indexOf(document.activeElement as HTMLElement)
    const move = (n: number) => { e.preventDefault(); all[(n + all.length) % all.length]?.focus() }
    if (e.key === "ArrowDown") move(at + 1)
    else if (e.key === "ArrowUp") move(at < 0 ? all.length - 1 : at - 1)
    else if (e.key === "Home") move(0)
    else if (e.key === "End") move(all.length - 1)
    else if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); shut() }
    else if (e.key === "Tab") shut(false)
  }
  const pick = (id: string) => { shut(); if (id !== pinned) pinView(id) }
  const command = (fn: (button: HTMLElement) => void) => { setOpen(false); const button = trigger.current; if (button) fn(button) }
  return <div className="vp-switch" data-open={open}>
    <button ref={trigger} type="button" className="vp-switch-trigger" aria-haspopup="menu" aria-expanded={open}
      aria-label={`${w.switchView}: ${current ? packageName(current.active, language) : ""}`}
      onClick={() => setOpen(v => !v)} onKeyDown={e => { if (e.key === "ArrowDown" && !open) { e.preventDefault(); setOpen(true) } }}>
      {cover ? <img src={cover} alt="" /> : <span className="vp-switch-blank" aria-hidden="true" />}
      <b>{current ? packageName(current.active, language) : ""}</b>
      <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden="true"><path d="M6 9l6 6 6-6" /></svg>
    </button>
    {open && <div ref={menu} className="vp-switch-menu" role="menu" aria-label={w.switchView} onKeyDown={key}>
      {rows.map(row => {
        const art = packageCover(row.active), on = row.id === pinned
        return <button key={row.id} type="button" role="menuitemradio" aria-checked={on} tabIndex={-1} data-package-id={row.id} onClick={() => pick(row.id)}>
          {art ? <img src={art} alt="" /> : <span className="vp-switch-blank" aria-hidden="true" />}
          <span><b>{packageName(row.active, language)}</b><small>{viewHint(row.id, w)}</small></span>
          {on && <svg className="vp-switch-check" viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2.4" aria-hidden="true"><path d="M5 12.5l4.5 4.5L19 7" /></svg>}
        </button>
      })}
      <div className="vp-switch-sep" role="separator" />
      <button type="button" role="menuitem" tabIndex={-1} className="vp-switch-command" onClick={() => command(onBrowse)}>
        <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.8" aria-hidden="true"><rect x="4" y="4" width="7" height="7" rx="2" /><rect x="13" y="4" width="7" height="7" rx="2" /><rect x="4" y="13" width="7" height="7" rx="2" /><rect x="13" y="13" width="7" height="7" rx="2" /></svg>
        {w.allViews}</button>
      <button type="button" role="menuitem" tabIndex={-1} className="vp-switch-command" onClick={() => command(onManage)}>
        <GearIcon />{w.manageInstall}</button>
    </div>}
  </div>
}

export function GearIcon() {
  return <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" aria-hidden="true">
    <circle cx="12" cy="12" r="3" /><path d="M12 2.5v3M12 18.5v3M2.5 12h3M18.5 12h3M5.3 5.3l2.1 2.1M16.6 16.6l2.1 2.1M5.3 18.7l2.1-2.1M16.6 7.4l2.1-2.1" />
  </svg>
}
