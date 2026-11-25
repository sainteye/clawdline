import { useEffect, useRef, useState } from "react"
import type { ProjectAssistantContextUsage } from "@clawdline/contract"
import { ContextUsage } from "./ContextUsage"
import type { Words } from "./words"

/** Why "Compact now" cannot run right now; null when it can. */
export type CompactBlock = "working" | "noSession" | "agent" | "readOnly" | null

/**
 * The context meter as a quiet button. It opens a small menu (a bottom sheet
 * on a phone) with Compact now, New session and Cancel.
 * Compact now is offered only while the Bot is idle; otherwise it is disabled
 * and says why.
 */
export function ContextMenu({usage, w, language, compactBlock, compacting, onCompact, onNewSession}: {
  usage: ProjectAssistantContextUsage
  w: Words
  language?: string
  compactBlock: CompactBlock
  compacting: boolean
  onCompact: () => void
  onNewSession?: () => void
}) {
  const [open, setOpen] = useState(false)
  const opener = useRef<HTMLButtonElement>(null), list = useRef<HTMLDivElement>(null)
  const close = (focus = true) => {setOpen(false); if (focus) opener.current?.focus({preventScroll: true})}
  useEffect(() => {
    if (!open) return
    list.current?.querySelector<HTMLButtonElement>("button:not([disabled])")?.focus({preventScroll: true})
    const away = (event: PointerEvent) => {
      if (!list.current?.contains(event.target as Node) && !opener.current?.contains(event.target as Node)) close(false)
    }
    const keys = (event: KeyboardEvent) => {if (event.key === "Escape") {event.preventDefault(); close()}}
    document.addEventListener("pointerdown", away, true); document.addEventListener("keydown", keys)
    return () => {document.removeEventListener("pointerdown", away, true); document.removeEventListener("keydown", keys)}
  }, [open])
  const reason = compacting ? w.compactRunning : compactBlock === "working" ? w.compactBusy : compactBlock === "noSession" ? w.compactNoSession
    : compactBlock === "agent" ? w.compactAgent : compactBlock === "readOnly" ? w.compactReadOnly : ""
  return <span className="pa-context-control">
    <button ref={opener} type="button" className="pa-context-button" aria-haspopup="menu" aria-expanded={open}
      aria-label={w.contextMenu} aria-busy={compacting} onClick={() => open ? close() : setOpen(true)}>
      {compacting && <span className="pa-send-spinner" aria-hidden="true"/>}
      <ContextUsage usage={usage} w={w} language={language}/>
    </button>
    {open && <>
      <div className="pa-context-backdrop" aria-hidden="true"/>
      <div className="pa-context-menu" role="menu" aria-label={w.contextMenu} ref={list}>
        <button type="button" role="menuitem" disabled={!!reason} aria-describedby={reason ? "pa-compact-reason" : undefined}
          onClick={() => {close(); onCompact()}}>
          <span>{w.compactNow}</span>
          <small id="pa-compact-reason">{reason || w.compactNowHint}</small>
        </button>
        {onNewSession && <button type="button" role="menuitem" onClick={() => {close(false); onNewSession()}}>
          <span>{w.compactNewSession}</span>
          <small>{w.compactNewSessionHint}</small>
        </button>}
        <button type="button" role="menuitem" className="pa-context-cancel" onClick={() => close()}><span>{w.cancel}</span></button>
      </div>
    </>}
  </span>
}
