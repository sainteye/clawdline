import { useEffect, useRef, useState } from "react"
import { createPortal } from "react-dom"
import { catalogWord } from "../catalog.js"
import { nextWord } from "../next-strings.js"
import { showModalWithoutChoice } from "../modal-focus.js"
import { COPILOT_DEVSTACK } from "../overlays/index.js"
import { Command } from "../session/Command.js"
import { ProjectDevStack } from "../pages/projects/ProjectDevStack.js"
import type { ProjectPlace } from "../pages/work/api.js"
import "../pages/projects/project-setup.css"

/** The Project a Dev Stack modal shows (`COPILOT_DEVSTACK`). */
export interface DevStackPlace { id: string; label: string }

/**
 * One Project's Dev Stack links in a modal over whatever page is shown, held
 * at the page's root beside the copilot's bubble. The person asked to see them
 * without leaving the page they were on, so it is not the Projects page's
 * setup dialog: it is the same `ProjectDevStack` panel in its own dialog.
 */
export function DevStackModal() {
  const [place, setPlace] = useState<DevStackPlace | null>(null)
  const dialog = useRef<HTMLDialogElement>(null)

  useEffect(() => {
    const onAsk = (event: Event) => {
      const detail = (event as CustomEvent<{ place?: DevStackPlace; answer?: (opened: boolean) => void }>).detail
      if (!detail?.place?.id || !detail.answer) return
      const answer = detail.answer
      detail.answer = undefined
      setPlace(detail.place)
      answer(true)
    }
    document.addEventListener(COPILOT_DEVSTACK, onAsk)
    return () => document.removeEventListener(COPILOT_DEVSTACK, onAsk)
  }, [])

  useEffect(() => {
    if (place) showModalWithoutChoice(dialog.current)
  }, [place])

  const close = () => {
    dialog.current?.close()
    setPlace(null)
  }

  return createPortal(<dialog className="project-setup-dialog devstack-modal" data-view="devstack" ref={dialog} tabIndex={-1}
    aria-labelledby="devstack-modal-title" onCancel={(event) => { event.preventDefault(); close() }}
    onClick={(event) => { if (event.target === dialog.current) close() }}>
    {place && <>
      <div className="project-setup-dialog-heading">
        <div><h2 id="devstack-modal-title">{`${place.label} · ${nextWord("devStackTab")}`}</h2></div>
        <button className="project-setup-close" type="button" aria-label={catalogWord("inline", "dfc889eaa026")} onClick={close}>
          {catalogWord("inline", "c7fdddf79eaa")}
        </button>
      </div>
      <div className="project-setup-dialog-body">
        <ProjectDevStack key={place.id} place={{ id: place.id, label: place.label } as ProjectPlace}
          onAsk={(instructions) => { close(); void Command.reviewProjectSetup(place.id, instructions) }} />
      </div>
    </>}
  </dialog>, document.body)
}
