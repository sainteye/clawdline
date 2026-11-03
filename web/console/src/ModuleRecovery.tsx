import { Component, useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode } from "react"
import { isModuleLoadError } from "./module-load-error.js"
import { nextWord } from "./next-strings.js"
import "./module-recovery.css"

/** Reload is a person's action: a failed chunk must not refresh a live composer. */
export function ModuleRecovery({ shown = true, startup = false, onSessions, reload = () => location.reload() }: {
  shown?: boolean
  startup?: boolean
  onSessions?: () => void
  reload?: () => void
}) {
  const titleId = useId()
  const title = useRef<HTMLHeadingElement>(null)
  const [reloading, setReloading] = useState(false)
  useLayoutEffect(() => {
    if (startup) document.documentElement.classList.remove("booting")
  }, [startup])
  useEffect(() => { if (shown) title.current?.focus({ preventScroll: true }) }, [shown])
  return <section className={`module-recovery${startup ? " module-recovery-startup" : ""}`} hidden={!shown} role="alert" aria-labelledby={titleId}>
    <div className="module-recovery-card">
      <div className="module-recovery-symbol" aria-hidden="true"><RetryIcon /></div>
      <h1 id={titleId} ref={title} tabIndex={-1}>{nextWord("moduleLoadTitle")}</h1>
      <p className="module-recovery-description">{nextWord("moduleLoadRecovery")}
        {onSessions && <><br />{nextWord("moduleLoadReturn")}</>}
      </p>
      <div className="module-recovery-actions">
        <button type="button" className="module-recovery-primary" disabled={reloading} onClick={() => {
          setReloading(true)
          reload()
        }}>{nextWord("newBuildReload")}<RetryIcon /></button>
        {onSessions && <button type="button" className="module-recovery-secondary" disabled={reloading}
          onClick={onSessions}>{nextWord("moduleLoadBack")}</button>}
      </div>
    </div>
    <p className="module-recovery-control">{nextWord("moduleLoadManual")}</p>
  </section>
}

function RetryIcon() {
  return <svg width="25" height="25" viewBox="0 0 24 24" fill="none" stroke="currentColor"
    strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" focusable="false">
    <path d="M19.5 8.5A8 8 0 1 0 20 14" /><path d="M19.5 3.5v5h-5" />
  </svg>
}

/** A missing lazy page must not unmount the shell or a neighbouring Session. */
export class ModuleLoadBoundary extends Component<{
  children: ReactNode
  shown?: boolean
  startup?: boolean
  onSessions?: () => void
}, { failed: boolean; error?: unknown }> {
  state: { failed: boolean; error?: unknown } = { failed: false }
  static getDerivedStateFromError(error: unknown) { return { failed: true, error } }
  render() {
    if (!this.state.failed) return this.props.children
    if (!isModuleLoadError(this.state.error)) throw this.state.error
    return <ModuleRecovery shown={this.props.shown} startup={this.props.startup} onSessions={this.props.onSessions} />
  }
}
