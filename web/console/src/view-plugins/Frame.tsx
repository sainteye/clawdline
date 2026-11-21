import { useEffect, useRef, useState } from "react"
import { PLUGIN_LIMITS as B } from "./limits.js"
import { PluginError, plain, exact, type ViewPackage } from "./package.js"
import { pluginBridge, pluginDocument, type Presentation, type ViewContext, type ViewReading, type ViewSource } from "./bridge.js"
import { LoadingSkeleton } from "../LoadingSkeleton.js"
import { pluginWords } from "./words.js"

export async function readPluginSource(source: ViewSource, outer: AbortSignal): Promise<ViewReading> {
  if (outer.aborted) throw new PluginError("scope_changed")
  const control = new AbortController()
  let timer: ReturnType<typeof setTimeout>
  let cancel: () => void = () => {}
  try {
    return await Promise.race([source.read(control.signal), new Promise<never>((_, reject) => {
      cancel = () => { control.abort(); reject(new PluginError("scope_changed")) }
      if (outer.aborted) { cancel(); return }
      outer.addEventListener("abort", cancel, { once: true })
      timer = setTimeout(() => { control.abort(); reject(new PluginError("host_unavailable")) }, B.requestMilliseconds)
    })])
  } finally { clearTimeout(timer!); outer.removeEventListener("abort", cancel) }
}
export interface ViewGrant {
  pkg: ViewPackage; sourceKey: string
  initial: ViewReading; instanceId: string
}
export function PluginFrame({ grant, source, language, onClose, onNavigate, onFault, presentation = "fullscreen", onPin, hidden = false }: {
  grant: ViewGrant; source: ViewSource; language: string
  onClose: () => void; onNavigate: (fn: () => void) => void; onFault: (code: string) => void
  /** Embedded frames have their own header; the footer keeps only connection state. */
  presentation?: Presentation
  /** Offered on a desktop's full-screen view: show this view beside the Session list instead. */
  onPin?: () => void
  /** An embedded view behind an open Session is mounted but not visible. */
  hidden?: boolean
}) {
  const frame = useRef<HTMLIFrameElement>(null)
  const [ready, setReady] = useState(false)
  const live = useRef({ source, language, onClose, onNavigate, onFault, hidden })
  live.current = { source, language, onClose, onNavigate, onFault, hidden }
  const [disconnected, setDisconnected] = useState(!grant.initial.connected || !!grant.initial.partial)
  const refreshRef = useRef<() => void>(() => {})
  const contextRef = useRef<() => void>(() => {})
  const w = pluginWords(language)
  const label = grant.pkg.manifest.name[language] || grant.pkg.manifest.name[language.startsWith("zh") ? "zh-Hant" : "en"] || grant.pkg.manifest.name.en
  useEffect(() => {
    const node = frame.current!
    let alive = true, accepted = false, reading = grant.initial, bridge: ReturnType<typeof pluginBridge> | null = null
    let busy = false, dirty = false
    const abort = new AbortController(), theme = matchMedia("(prefers-color-scheme: dark)"), motion = matchMedia("(prefers-reduced-motion: reduce)")
    const context = (): ViewContext => ({
      language: document.documentElement.lang || live.current.language, theme: document.documentElement.dataset.theme === "light" ? "light" :
        document.documentElement.dataset.theme === "dark" ? "dark" : theme.matches ? "dark" : "light",
      reducedMotion: motion.matches, visible: document.visibilityState === "visible" && !live.current.hidden,
      scopeLabel: live.current.source.label, connection: reading.connected ? "connected" : "disconnected", presentation,
    })
    const send = () => { if (alive) bridge?.update(reading, context()) }
    contextRef.current = send
    const fault = (code: string) => { if (alive) live.current.onFault(code) }
    const boot = (event: MessageEvent) => {
      if (!alive || accepted || event.source !== node.contentWindow || !plain(event.data) ||
          !exact(event.data, ["type", "protocolVersion"]) || event.data.type !== "clawdline.plugin.hello" || event.data.protocolVersion !== 1) return
      const channel = new MessageChannel()
      try {
        bridge = pluginBridge({ port: channel.port1, instanceId: grant.instanceId, capabilities: grant.pkg.manifest.capabilities,
          reading, context: context(), close: () => live.current.onClose(), fault,
          open: async key => {
            const control = new AbortController()
            let timer: ReturnType<typeof setTimeout> | undefined
            try {
              const navigate = await Promise.race([live.current.source.prepareOpen(key, control.signal), new Promise<never>((_, reject) => {
                timer = setTimeout(() => { control.abort(); reject(new PluginError("host_unavailable")) }, B.requestMilliseconds)
              })])
              if (!alive || control.signal.aborted || live.current.source.key !== grant.sourceKey) throw new PluginError("scope_changed")
              live.current.onNavigate(navigate)
            } finally { clearTimeout(timer); control.abort() }
          } })
        accepted = true; clearTimeout(bootTimer)
        node.contentWindow!.postMessage({ type: "clawdline.plugin.connect", protocolVersion: 1,
          instanceId: grant.instanceId, context: context(), limits: { pending: B.pending, requestMilliseconds: B.requestMilliseconds } }, "*", [channel.port2])
        setReady(true)
      } catch (e) { channel.port1.close(); channel.port2.close(); fault(e instanceof PluginError ? e.code : "host_unavailable") }
    }
    const bootTimer = setTimeout(() => fault("host_unavailable"), B.requestMilliseconds)
    window.addEventListener("message", boot)
    // The listener exists before package scripts are parsed.
    // StrictMode rehearses setup/cleanup before paint. Cancel its first document
    // rather than letting two navigations race their hello on one WindowProxy.
    queueMicrotask(() => { if (alive) node.srcdoc = pluginDocument(grant.pkg.html) })
    const refresh = async () => {
      if (!alive) return
      if (busy) { dirty = true; return }
      busy = true
      try {
        const value = await readPluginSource(live.current.source, abort.signal)
        if (!alive) return
        reading = value; setDisconnected(!value.connected || !!value.partial); send()
      } catch (e) {
        if (!alive) return
        if (e instanceof PluginError && ["permission_denied", "scope_changed"].includes(e.code)) { fault(e.code); return }
        reading = { ...reading, sessions: reading.sessions.map(row => ({ ...row, connected: false })), connected: false, partial: true }; setDisconnected(true); send()
      } finally { busy = false; if (dirty && alive) { dirty = false; void refresh() } }
    }
    const unsubscribe = source.subscribe(() => { void refresh() })
    refreshRef.current = () => { void refresh() }
    const visibility = () => { send(); if (!document.hidden) void refresh() }
    const observer = new MutationObserver(send)
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["lang", "data-theme", "class"] })
    document.addEventListener("visibilitychange", visibility)
    theme.addEventListener("change", send); motion.addEventListener("change", send)
    // A re-open always obtains a fresh authorized reading before it reaches this component.
    return () => {
      alive = false; abort.abort(); clearTimeout(bootTimer); bridge?.dispose(); unsubscribe(); observer.disconnect()
      window.removeEventListener("message", boot); document.removeEventListener("visibilitychange", visibility)
      theme.removeEventListener("change", send); motion.removeEventListener("change", send)
      node.removeAttribute("srcdoc")
    }
  }, [grant.instanceId])
  useEffect(() => { contextRef.current() }, [language, hidden])
  const embedded = presentation === "embedded"
  return <section className="vp-view" data-presentation={presentation} aria-label={label}>
    <div className="vp-frame-content" aria-busy={!ready}>
    {!ready && <div className="vp-frame-loading"><LoadingSkeleton label={w.loading} id="view-plugin-frame" kind="detail" /></div>}
    <iframe ref={frame} title={label} sandbox="allow-scripts" referrerPolicy="no-referrer"
      allow="camera 'none'; microphone 'none'; geolocation 'none'; clipboard-read 'none'; clipboard-write 'none'" />
    </div>
    {(!embedded || disconnected) && <footer className="vp-frame-bar">
      {!embedded && <button type="button" onClick={onClose} aria-label={w.viewClose}><span aria-hidden="true">←</span>{w.returnToApp}</button>}
      {disconnected && <span role="status">{w.offline}</span>}
      {disconnected && <button type="button" onClick={() => refreshRef.current()}>{w.retry}</button>}
      {!embedded && onPin && <button type="button" className="vp-pin" onClick={onPin}>{w.pin}</button>}
    </footer>}
  </section>
}
