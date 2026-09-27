import { useEffect, useRef, useState } from "react"
import type { Persona } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"
import { loadPersonas, onPersonas, personaById, personaName, personaTitle, personasNow } from "../personas.js"
import "./persona.css"

/** The catalog, re-rendering once it arrives; empty until then and when it cannot be read. */
export function usePersonas(): Persona[] {
  const [list, setList] = useState<Persona[]>(() => personasNow() ?? [])
  useEffect(() => {
    const off = onPersonas(() => setList(personasNow() ?? []))
    void loadPersonas().then((l) => setList(l))
    return off
  }, [])
  return list
}

/** A persona's pixel bot, drawn by the same drawer as a project's mark. */
export function PersonaBot({ persona, cellPx, className }: { persona: Persona; cellPx: number; className?: string }) {
  const ref = useRef<HTMLCanvasElement>(null)
  useEffect(() => {
    L.paintIcon(ref.current, persona.icon, cellPx)
  }, [persona, cellPx])
  return <canvas className={className ?? "persona-bot"} ref={ref} width={0} height={0} aria-hidden="true" />
}

/** The bot and the short name, for a picker or a session choice; nothing for no persona. */
export function PersonaTag({ id, personas }: { id: string | null | undefined; personas: Persona[] }) {
  const persona = personaById(personas, id)
  if (!persona) return null
  return (
    <span className="persona-tag" title={personaTitle(persona)}>
      <PersonaBot persona={persona} cellPx={2} className="persona-tag-bot" />
      <span className="persona-tag-name">{personaName(persona)}</span>
    </span>
  )
}
