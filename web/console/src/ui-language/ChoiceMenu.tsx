import { useEffect, useRef, useState } from "react"
import "./choice-menu.css"

export interface ChoiceOption { value: string; label: string; icon?: string; lang?: string }

export function ChoiceMenu({ value, options, label, disabled, onPick, compact = false, id }: {
  value: string
  options: ChoiceOption[]
  label: string
  disabled?: boolean
  onPick: (value: string) => void
  compact?: boolean
  id?: string
}) {
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLDivElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const selected = options.find((option) => option.value === value) ?? { value, label: value }
  useEffect(() => {
    if (!open) return
    const outside = (event: PointerEvent) => { if (!root.current?.contains(event.target as Node)) setOpen(false) }
    const escape = (event: KeyboardEvent) => { if (event.key === "Escape") { event.preventDefault(); setOpen(false); trigger.current?.focus() } }
    document.addEventListener("pointerdown", outside)
    document.addEventListener("keydown", escape)
    return () => { document.removeEventListener("pointerdown", outside); document.removeEventListener("keydown", escape) }
  }, [open])
  const move = (direction: number) => {
    const buttons = Array.from(root.current?.querySelectorAll<HTMLButtonElement>('[role="option"]') ?? [])
    const active = buttons.indexOf(document.activeElement as HTMLButtonElement)
    buttons[active < 0 ? (direction > 0 ? 0 : buttons.length - 1) : (active + direction + buttons.length) % buttons.length]?.focus()
  }
  return <div className={`choice-menu${compact ? " compact" : ""}`} ref={root}>
    <button id={id} ref={trigger} type="button" className="choice-menu-trigger" aria-label={label} aria-haspopup="listbox"
      aria-expanded={open} disabled={disabled} onClick={() => setOpen(!open)}
      onKeyDown={(event) => { if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); setOpen(true); const direction = event.key === "ArrowDown" ? 1 : -1; setTimeout(() => move(direction), 0) } }}>
      {selected.icon && <span className="choice-menu-icon" aria-hidden="true">{selected.icon}</span>}
      <span className="choice-menu-text" lang={selected.lang}>{selected.label}</span><span className="choice-menu-chevron" aria-hidden="true">⌄</span>
    </button>
    {open && <div className="choice-menu-list" role="listbox" aria-label={label}>
      {options.map((option) => <button key={option.value} type="button" role="option" aria-selected={option.value === value}
        className="choice-menu-option" onClick={() => { onPick(option.value); setOpen(false); trigger.current?.focus() }}
        onKeyDown={(event) => { if (event.key === "ArrowDown" || event.key === "ArrowUp") { event.preventDefault(); move(event.key === "ArrowDown" ? 1 : -1) } }}>
        {option.icon && <span className="choice-menu-icon" aria-hidden="true">{option.icon}</span>}
        <span className="choice-menu-text" lang={option.lang}>{option.label}</span>
        {option.value === value && <span className="choice-menu-check" aria-hidden="true">✓</span>}
      </button>)}
    </div>}
  </div>
}
