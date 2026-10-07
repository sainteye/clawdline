import { catalogWord } from "../catalog.js"
import { useEffect, useRef, useState } from "react"
import { createPortal } from "react-dom"
import { SidebarIcon, type SidebarIconName } from "../SidebarIcon.js"
import { appendToComposer } from "../legacy/snippets-bridge.js"
import { ResourceCoordination } from "./ResourceCoordination.js"
import "./clawdfather-suggestions.css"

type Suggestion = { title: string; prompt: string; icon: SidebarIconName }

const suggestions = [
  { title: "4c4eebeb189f", prompt: "d3167f38f5fb", icon: "verify" },
  { title: "4fb4ad1b7401", prompt: "365604b32858", icon: "devices" },
  { title: "3d37fb3af4ee", prompt: "6c7e323f158d", icon: "settings" },
  { title: "98091e56303b", prompt: "673f223fa434", icon: "transfer" },
  { title: "a956b8eeb16c", prompt: "3e8c30b7e30d", icon: "work" },
] as const

export function ClawdfatherSuggestions({ onClose }: { onClose: () => void }) {
  const first = useRef<HTMLButtonElement>(null)
  const [coordination, setCoordination] = useState(false)
  const closeRef = useRef(onClose)
  closeRef.current = onClose
  const rows: Suggestion[] = suggestions.map((item) => ({
    icon: item.icon,
    title: catalogWord("literal", item.title),
    prompt: catalogWord("literal", item.prompt),
  }))

  useEffect(() => {
    first.current?.focus({ preventScroll: true })
    const key = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault()
        event.stopImmediatePropagation()
        close()
      }
      if (event.key === "Tab") {
        const controls = [...document.querySelectorAll<HTMLElement>("#clawdfather-suggestions button")]
        const at = controls.indexOf(document.activeElement as HTMLElement)
        const next = (at + (event.shiftKey ? -1 : 1) + controls.length) % controls.length
        event.preventDefault()
        controls[next]?.focus()
      }
    }
    document.addEventListener("keydown", key, true)
    return () => document.removeEventListener("keydown", key, true)
  }, [])

  const close = () => {
    closeRef.current()
    document.getElementById("detail-snippets")?.focus({ preventScroll: true })
  }
  const pick = (prompt: string) => {
    closeRef.current()
    appendToComposer(prompt)
    document.getElementById("msg")?.focus({ preventScroll: true })
  }

  return createPortal(
    <div className="overlay" id="clawdfather-suggestions" onClick={close}>
      <div className="sheet clawdfather-suggestions" role="dialog" aria-modal="true"
        aria-labelledby="clawdfather-suggestions-title" onClick={(event) => event.stopPropagation()}>
        <div className="clawdfather-suggestions-head">
          <h2 id="clawdfather-suggestions-title">{coordination ? catalogWord("resourceCoordination", "title") : catalogWord("literal", "56aed5587136")}</h2>
          <button type="button" className="clawdfather-suggestions-close" onClick={close}
            aria-label={catalogWord("literal", "4d8822d96b94")}>×</button>
        </div>
        {coordination ? <ResourceCoordination onBack={() => setCoordination(false)}
          onAsk={() => pick(catalogWord("resourceCoordination", "prompt"))} /> : <div className="clawdfather-suggestions-list">
          <button type="button" onClick={() => setCoordination(true)}>
            <SidebarIcon name="work" />
            <span>{catalogWord("resourceCoordination", "title")}</span>
          </button>
          {rows.map((item, index) => (
            <button key={item.title} type="button" ref={index === 0 ? first : undefined}
              onClick={() => pick(item.prompt)}>
              <SidebarIcon name={item.icon} />
              <span>{item.title}</span>
            </button>
          ))}
        </div>}
      </div>
    </div>,
    document.body,
  )
}
