import { Component, useEffect, useLayoutEffect, useRef, useState, type ReactNode } from "react"
import { isModuleLoadError } from "./module-load-error.js"
import { nextWord } from "./next-strings.js"
import "./module-recovery.css"

/** Reload is a person's action: a failed chunk must not refresh a live composer. */
export function ModuleRecovery({ shown = true, startup = false, reload = () => location.reload() }: {
  shown?: boolean
  startup?: boolean
  reload?: () => void
}) {
  const title = useRef<HTMLHeadingElement>(null)
  const [reloading, setReloading] = useState(false)
  useLayoutEffect(() => {
    if (startup) document.documentElement.classList.remove("booting")
  }, [startup])
  useEffect(() => { if (shown) title.current?.focus({ preventScroll: true }) }, [shown])
  return <section className={`module-recovery${startup ? " module-recovery-startup" : ""}`} hidden={!shown} role="alert">
    <h1 ref={title} tabIndex={-1}>{nextWord("moduleLoadTitle")}</h1>
    <p>{nextWord("moduleLoadRecovery")}</p>
    <button type="button" className="go" disabled={reloading} onClick={() => {
      setReloading(true)
      reload()
    }}>{nextWord("newBuildReload")}</button>
  </section>
}

/** A missing lazy page must not unmount the shell or a neighbouring Session. */
export class ModuleLoadBoundary extends Component<{
  children: ReactNode
  shown?: boolean
  startup?: boolean
}, { failed: boolean; error?: unknown }> {
  state: { failed: boolean; error?: unknown } = { failed: false }
  static getDerivedStateFromError(error: unknown) { return { failed: true, error } }
  render() {
    if (!this.state.failed) return this.props.children
    if (!isModuleLoadError(this.state.error)) throw this.state.error
    return <ModuleRecovery shown={this.props.shown} startup={this.props.startup} />
  }
}
