import { ChevronIcon } from "./ChevronIcon.js"
import { useEffect, useId, useRef, useState } from "react"
import type { SessionRow } from "@clawdline/contract"
import { catalogWord } from "../catalog.js"
import { Mark } from "../session/List.js"
import "./project-picker.css"

export interface ProjectPickerOption { id: string; label: string; icon?: unknown }

/** Callers own their layout and option identity; this control owns selection interaction. */
export function ProjectPicker({ places, value, onChange, onOpen, allowAll = false, allLabel, label, placeholder }: {
  places: ProjectPickerOption[]
  value: string
  onChange: (id: string) => void
  onOpen?: () => Promise<void>
  allowAll?: boolean
  allLabel?: string
  label?: string
  placeholder?: string
}) {
  const [open, setOpen] = useState(false)
  const [refreshing, setRefreshing] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const menuID = useId()
  const selected = places.find(place => place.id === value)
  const name = label ?? catalogWord("inline", "985959785319")
  const close = (restore = false) => { setOpen(false); if (restore) trigger.current?.focus() }
  useEffect(() => {
    if (!open) return
    const options = root.current?.querySelectorAll<HTMLButtonElement>('[role="option"]')
    const active = root.current?.querySelector<HTMLButtonElement>('[aria-selected="true"]')
    ;(active ?? options?.[0])?.focus({ preventScroll: true })
    const outside = (event: PointerEvent) => {
      if (!root.current?.contains(event.target as Node)) setOpen(false)
    }
    document.addEventListener("pointerdown", outside)
    return () => document.removeEventListener("pointerdown", outside)
  }, [open])
  const toggle = () => {
    const opening = !open
    setOpen(opening)
    if (!opening || !onOpen) return
    setRefreshing(true)
    void onOpen().catch(() => {}).finally(() => setRefreshing(false))
  }
  const choose = (id: string) => { onChange(id); close(true) }
  const mark = (icon: unknown) => icon
    ? <Mark icon={icon as SessionRow["icon"]} cellPx={3} />
    : <span className="project-picker-placeholder" aria-hidden="true">▦</span>
  const option = (place: ProjectPickerOption) => <button type="button" role="option" tabIndex={-1}
    aria-selected={place.id === value} className="project-picker-option" key={place.id} onClick={() => choose(place.id)}>
    {mark(place.icon)}<span className="project-picker-label" title={place.label}>{place.label}</span>
    {place.id === value && <span className="project-picker-check" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="m5 12 4 4L19 6" /></svg></span>}
  </button>
  return <div className="project-picker" ref={root} onBlur={event => {
    if (!event.currentTarget.contains(event.relatedTarget as Node | null)) close()
  }} onKeyDown={event => {
    if (event.key === "Escape" && open) { event.preventDefault(); event.stopPropagation(); close(true); return }
    if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return
    event.preventDefault()
    if (!open) { toggle(); return }
    const options = Array.from(root.current?.querySelectorAll<HTMLButtonElement>('[role="option"]') ?? [])
    const index = options.indexOf(document.activeElement as HTMLButtonElement)
    const next = event.key === "Home" ? 0 : event.key === "End" ? options.length - 1
      : (index + (event.key === "ArrowDown" ? 1 : -1) + options.length) % options.length
    options[next]?.focus()
  }}>
    <button className="project-picker-trigger" ref={trigger} type="button" aria-label={name} aria-haspopup="listbox"
      aria-expanded={open} aria-controls={open ? menuID : undefined} onClick={toggle}>
      {mark(selected?.icon)}<span className="project-picker-label" title={selected?.label}>{selected?.label || (allowAll ? allLabel ?? catalogWord("literal", "b2ee45d95857") : placeholder ?? catalogWord("literal", "ef8fe1c8ac54"))}</span>
      <span className="project-picker-chevron" aria-hidden="true"><ChevronIcon /></span>
    </button>
    <div className="project-picker-menu" data-motion-id="AN-014" id={menuID} role="listbox" aria-label={name} hidden={!open} inert={!open}>
      {allowAll && option({ id: "", label: allLabel ?? catalogWord("inline", "24926b27517e") })}
      {places.map(option)}
      {!places.length && <p className="project-picker-empty">{refreshing ? catalogWord("literal", "eec20299e1b1") : catalogWord("literal", "dc9a2139ef07")}</p>}
    </div>
  </div>
}
