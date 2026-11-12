import { Fragment, useEffect, useId, useRef, useState, type ReactNode } from "react"
import { nextWord } from "../next-strings.js"

export interface HeaderOption { id: string; control: ReactNode }

/** On phones the rightmost available entrance stays beside the disclosure. */
export function HeaderOptions({ options }: { options: HeaderOption[] }) {
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const id = useId()
  const identity = options.map(option => option.id).join(",")
  useEffect(() => { setOpen(false) }, [identity])
  useEffect(() => {
    if (!open) return
    const closeOutside = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false)
    }
    const escape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return
      event.preventDefault()
      event.stopPropagation()
      setOpen(false)
      trigger.current?.focus()
    }
    document.addEventListener("pointerdown", closeOutside)
    document.addEventListener("keydown", escape, true)
    return () => {
      document.removeEventListener("pointerdown", closeOutside)
      document.removeEventListener("keydown", escape, true)
    }
  }, [open])
  if (!options.length) return null
  const label = nextWord(open ? "headerOptionsCollapse" : "headerOptionsExpand")
  return <div className="header-options" ref={root} data-expanded={open ? "true" : undefined}>
    {options.length > 1 && <div className="header-options-extra" id={id}
      onClick={() => { if (open) { setOpen(false); trigger.current?.focus({ preventScroll: true }) } }}>
      {options.slice(0, -1).map(option => <Fragment key={option.id}>{option.control}</Fragment>)}
    </div>}
    <Fragment key={options.at(-1)!.id}>{options.at(-1)!.control}</Fragment>
    {options.length > 1 && <button type="button" className="header-options-toggle" ref={trigger}
      aria-expanded={open} aria-controls={id} aria-label={label} title={label} onClick={() => setOpen(!open)}>
      <svg viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.6" aria-hidden="true"><path d="m4 6 4 4 4-4" /></svg>
    </button>}
  </div>
}
