import { useLayoutEffect, useRef } from "react"
import type { Icon, Persona } from "@clawdline/contract"
import { BRAND_MARK } from "../brand-mark.js"
import { paintIcon } from "../legacy/bridge.js"
import { personaById } from "../personas.js"
import { TerminalGlyph } from "./TerminalGlyph.js"

export function sessionBoardAvatar(persona: string | undefined, lead: boolean, personas: readonly Persona[]): Icon | null {
  return lead ? BRAND_MARK : personaById(personas, persona)?.icon ?? null
}

export function SessionBoardMark({ icon, lead = false, project = false }: { icon: Icon | null; lead?: boolean; project?: boolean }) {
  const ref = useRef<HTMLCanvasElement>(null)
  useLayoutEffect(() => { paintIcon(ref.current, icon ?? undefined, lead ? 4 : 2) }, [icon, lead])
  return <span className={project ? "session-board-project-mark" : "session-board-avatar"} data-lead={lead || undefined} aria-hidden="true">
    {icon ? <canvas ref={ref} width={0} height={0} /> : <TerminalGlyph />}
    {lead && <span className="clawdfather-crown" />}
  </span>
}
