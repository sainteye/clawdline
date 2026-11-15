import { useEffect, useLayoutEffect, useRef, useState } from "react"
import { createPortal } from "react-dom"
import { useStatusDockAvailable } from "../project-assistants/BotOrb.js"
import type { ViewSource, ViewReading } from "./bridge.js"
import { PluginError, parsePackage, type ViewPackage } from "./package.js"
import { installedPackages, subscribePackages, installPackage, enablePackage, removePackage, rollbackPackage, type Installed } from "./store.js"
import { PLUGIN_LIMITS as B } from "./limits.js"
import { PluginFrame, readPluginSource, type ViewGrant } from "./Frame.js"
import { pluginWords } from "./words.js"
import "./plugins.css"

const code = (e: unknown) => e instanceof PluginError ? e.code : "storage_unavailable"
const name = (p: ViewPackage, language: string) => p.manifest.name[language.startsWith("zh") ? "zh-Hant" : "en"]

export function useViewPlugins(sources: readonly ViewSource[], language: string) {
  const [opened, setOpened] = useState(false), openedRef = useRef(false), dock = useStatusDockAvailable()
  const [rows, setRows] = useState<Installed[]>([]), [error, setError] = useState("")
  const [busy, setBusy] = useState(false), [review, setReview] = useState<{ archive: ArrayBuffer; pkg: ViewPackage; expected: string | null } | null>(null)
  const [selected, setSelected] = useState<ViewPackage | null>(null), [sourceKey, setSourceKey] = useState("")
  const [projectKey, setProjectKey] = useState<string | null>(null), [preview, setPreview] = useState<ViewReading | null>(null)
  const [grant, setGrant] = useState<ViewGrant | null>(null)
  const modal = useRef<HTMLDivElement>(null), opener = useRef<HTMLElement | null>(null)
  const owned = useRef<string | null>(null), navigate = useRef<(() => void) | null>(null)
  const closing = useRef(false)
  const operations = useRef(0), liveSources = useRef(sources); liveSources.current = sources
  const w = pluginWords(language), source = sources.find(s => s.key === sourceKey)
  const finish = () => {
    openedRef.current = false; owned.current = null; closing.current = false; operations.current++
    setOpened(false); setGrant(null); setSelected(null); setPreview(null); setReview(null); setBusy(false)
    const next = navigate.current; navigate.current = null
    requestAnimationFrame(() => { (opener.current?.isConnected ? opener.current : document.querySelector<HTMLButtonElement>(".vp-dock,.vp-float"))?.focus({ preventScroll: true });
      try { next?.() } catch { /* A source revoked during Back never receives navigation. */ }
    })
  }
  const finishRef = useRef(finish); finishRef.current = finish
  const close = () => {
    if (closing.current || !openedRef.current) return
    closing.current = true
    setGrant(null)
    if (owned.current && history.state?.viewPlugin === owned.current) history.back()
    else finishRef.current()
  }
  const closeRef = useRef(close); closeRef.current = close
  const refresh = async () => { try { setRows(await installedPackages()) } catch (e) { setError(code(e)) } }
  useEffect(() => subscribePackages(() => {
    // A hint revokes the running grant immediately, before asynchronous storage verification.
    operations.current++; setGrant(null); setSelected(null); setPreview(null); setReview(null); setBusy(false)
    void refresh()
  }), [])
  useEffect(() => {
    if (!grant || sources.some(s => s.key === grant.sourceKey)) return
    setGrant(null); setSelected(null); setPreview(null); setError("scope_changed")
  }, [sources, grant])
  useEffect(() => {
    if (!opened || !selected || !source) { setPreview(null); return }
    const abort = new AbortController(); setPreview(null); setError("")
    void readPluginSource(source, abort.signal).then(r => { if (!abort.signal.aborted) setPreview(r) }, e => {
      if (!abort.signal.aborted) setError(e instanceof PluginError ? e.code : "host_unavailable")
    })
    return () => abort.abort()
  }, [opened, selected?.digest, sourceKey])
  useLayoutEffect(() => {
    if (!opened) return
    const node = modal.current!, siblings = [...document.body.children].filter(el => el !== node && el instanceof HTMLElement) as HTMLElement[]
    const prior = siblings.map(el => [el, el.inert] as const), overflow = document.body.style.overflow
    for (const [el] of prior) el.inert = true
    document.body.style.overflow = "hidden"
    node.querySelector<HTMLButtonElement>("button")?.focus()
    const pop = (e: PopStateEvent) => {
      if (!openedRef.current) return
      e.stopImmediatePropagation(); finishRef.current()
    }
    const key = (e: KeyboardEvent) => {
      if (e.key === "Escape") { e.preventDefault(); e.stopImmediatePropagation(); closeRef.current(); return }
      if (e.key !== "Tab") return
      const all = [...node.querySelectorAll<HTMLElement>("button:not([disabled]),a[href],input,select,iframe")].filter(el => el.getClientRects().length)
      const first = all[0], last = all[all.length - 1]
      if (e.shiftKey && document.activeElement === first) { e.preventDefault(); last?.focus() }
      else if (!e.shiftKey && document.activeElement === last) { e.preventDefault(); first?.focus() }
    }
    window.addEventListener("popstate", pop, true); window.addEventListener("keydown", key, true)
    return () => {
      window.removeEventListener("popstate", pop, true); window.removeEventListener("keydown", key, true)
      for (const [el, inert] of prior) el.inert = inert
      document.body.style.overflow = overflow
    }
  }, [opened])
  const open = (button: HTMLButtonElement) => {
    if (openedRef.current) return
    opener.current = button; owned.current = crypto.randomUUID(); openedRef.current = true
    history.pushState({ ...history.state, viewPlugin: owned.current }, "", location.href)
    setError(""); setOpened(true); void refresh()
  }
  const act = async (operation: () => Promise<void>) => {
    if (busy) return
    setBusy(true); setError("")
    try { await operation(); await refresh() } catch (e) { setError(code(e)) }
    finally { setBusy(false) }
  }
  const inspect = async (archive: ArrayBuffer) => {
    const generation = operations.current
    const pkg = await parsePackage(archive), current = (await installedPackages()).find(r => r.id === pkg.manifest.id)
    if (!openedRef.current || generation !== operations.current) return
    setReview({ archive, pkg, expected: current?.active.digest ?? null }); setSelected(null)
  }
  const example = (id: string) => act(async () => {
    const answer = await fetch(`/view-plugins/${id}.zip`, { credentials: "omit" })
    if (!answer.ok || !answer.body) throw new PluginError("invalid_package")
    const reader = answer.body.getReader(), chunks: Uint8Array[] = []; let size = 0
    try { while (true) { const part = await reader.read(); if (part.done) break; size += part.value.length
      if (size > B.archiveBytes) throw new PluginError("package_too_large"); chunks.push(part.value) }
    } finally { await reader.cancel() }
    const bytes = new Uint8Array(size); let at = 0; for (const part of chunks) { bytes.set(part, at); at += part.length }
    await inspect(bytes.buffer)
  })
  const allow = () => act(async () => {
    if (!selected || !source) throw new PluginError("scope_changed")
    const operation = ++operations.current, captured = source.key, pkg = selected, project = projectKey
    const initial = await readPluginSource(source, new AbortController().signal)
    if (operation !== operations.current || !openedRef.current || !liveSources.current.some(s => s.key === captured)) throw new PluginError("scope_changed")
    const count = initial.sessions.filter(s => project === null || s.project?.key === project).length
    if (count > B.sessions) throw new PluginError("too_many_sessions")
    setGrant({ pkg, sourceKey: captured, projectKey: project, initial, instanceId: crypto.randomUUID() })
  })
  const entry = <button type="button" className={dock ? "vp-dock" : "vp-float"} aria-label={w.entry} title={w.title} onClick={e => open(e.currentTarget)}>
    <svg viewBox="0 0 32 32" width="25" height="25" aria-hidden="true"><ellipse cx="16" cy="16" rx="14" ry="6" transform="rotate(-30 16 16)" fill="none" stroke="currentColor" strokeWidth="1.3"/><ellipse cx="16" cy="16" rx="13" ry="6" transform="rotate(40 16 16)" fill="none" stroke="currentColor" strokeWidth="1"/><circle cx="16" cy="16" r="3" fill="currentColor"/><circle cx="27" cy="10" r="2" fill="currentColor"/></svg>
  </button>
  const projects = new Map(preview?.sessions.flatMap(s => s.project ? [[s.project.key, s.project.label] as const] : []) ?? [])
  const content = opened ? createPortal(<div className="vp-host" ref={modal} role="dialog" aria-modal="true" aria-label={w.title}>
    {grant && source ? <PluginFrame key={grant.instanceId} grant={grant} source={source} language={language} onClose={close}
      onNavigate={fn => { navigate.current = fn }} onFault={error => { setGrant(null); setSelected(null); setError(error) }} /> : <>
      <header className="vp-bar"><h1>{w.title}</h1><button type="button" aria-label={w.close} onClick={close}>{w.close} ✕</button></header>
      <main className="vp-manager">
        <p>{w.local}</p><p>{w.trust}</p>
        {error && <p className="vp-error" role="alert">{w.errors[error] || w.unavailable}</p>}
        <label className="vp-import">{w.install}<input type="file" accept=".zip,application/zip" disabled={busy} onChange={e => {
          const file = e.currentTarget.files?.[0]; e.currentTarget.value = ""
          if (file) void act(async () => { if (file.size > B.archiveBytes) throw new PluginError("package_too_large"); await inspect(await file.arrayBuffer()) })
        }} /></label>
        <section><h2>{w.preset}</h2><div className="vp-actions"><button disabled={busy} onClick={() => void example("session-galaxy")}>{w.addGalaxy}</button>
          <button disabled={busy} onClick={() => void example("session-list")}>{w.addList}</button></div></section>
        {review && <section className="vp-card"><h2>{w.review}: {name(review.pkg, language)}</h2><p>{review.pkg.manifest.id} · {review.pkg.manifest.version}</p>
          <p>{w.permissions}: {w.read}{review.pkg.manifest.capabilities.includes("sessions.open") ? ` · ${w.navigate}` : ""}</p>
          <div className="vp-actions"><button disabled={busy} onClick={() => void act(async () => { await installPackage(review.archive, review.expected); setReview(null) })}>{review.expected ? w.replace : w.confirmInstall}</button>
          <button disabled={busy} onClick={() => setReview(null)}>{w.cancel}</button></div></section>}
        {selected && <section className="vp-card"><h2>{w.scope}: {name(selected, language)}</h2>
          <label>{w.source}<select value={sourceKey} disabled={busy} onChange={e => { setSourceKey(e.target.value); setProjectKey(null) }}>
            {sources.map(s => <option key={s.key} value={s.key}>{s.label}</option>)}</select></label>
          {!source ? <p>{w.noSource}</p> : !preview ? (!error && <p role="status">{w.loading}</p>) : <label>{w.project}<select value={projectKey ?? ""} disabled={busy} onChange={e => setProjectKey(e.target.value || null)}>
            <option value="">{w.all} ({preview.sessions.length})</option>{[...projects].map(([key, label]) => <option key={key} value={key}>{label}</option>)}
          </select></label>}
          <div className="vp-actions"><button disabled={busy || !source || !preview} onClick={allow}>{w.allow}</button><button disabled={busy} onClick={() => setSelected(null)}>{w.cancel}</button></div>
        </section>}
        <section><h2>{w.installed}</h2>{!rows.length && <p>{w.empty}</p>}{rows.map(row => <article className="vp-card" key={row.id}>
          <h3>{name(row.active, language)}</h3><p>{row.id} · {row.active.manifest.version}</p>
          <div className="vp-actions"><button disabled={busy || !row.enabled} onClick={() => { setSelected(row.active); setSourceKey(sources[0]?.key || ""); setProjectKey(null); setError(""); setReview(null) }}>{w.open}</button>
          <button disabled={busy} onClick={() => void act(() => enablePackage(row, !row.enabled))}>{row.enabled ? w.disable : w.enable}</button>
          {row.previous && <button disabled={busy} onClick={() => void act(() => rollbackPackage(row))}>{w.rollback} · {row.previous.manifest.version}</button>}
          <button disabled={busy} onClick={() => void act(() => removePackage(row))}>{w.remove}</button></div>
        </article>)}</section>
      </main></>}
  </div>, document.body) : null
  return { opened, openedRef, dock: !opened && dock ? entry : null, entry: !opened && !dock ? entry : null, content }
}
