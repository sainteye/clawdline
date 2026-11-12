import { useEffect, useRef } from 'react'
import { createPortal } from 'react-dom'
import { nextWord } from '../next-strings.js'
import { showModalWithoutChoice } from '../modal-focus.js'
import { currentCatalogTag } from '../catalog.js'
import { demoFilmFor } from './copilot-demo-films.js'

/**
 * The voice copilot's demonstration film in a modal: for a person without an
 * OpenAI key, who can watch what a call does before deciding to add one. The
 * film loads only when this opens, and plays only when pressed.
 */
export function CopilotDemoFilm({ open, onClose }: { open: boolean; onClose: () => void }) {
  const dialog = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    if (open) showModalWithoutChoice(dialog.current)
    else dialog.current?.close()
  }, [open])
  if (!open) return null
  const narrow = typeof window !== 'undefined' && window.matchMedia?.('(max-width: 720px)').matches === true
  const film = demoFilmFor(currentCatalogTag() || 'en', narrow)
  return createPortal(<dialog ref={dialog} className="copilot-demo-film" aria-labelledby="copilot-demo-title" tabIndex={-1}
    onCancel={(event) => { event.preventDefault(); onClose() }}
    onClick={(event) => { if (event.target === dialog.current) onClose() }}>
    <div className="copilot-demo-head">
      <h2 id="copilot-demo-title">{nextWord('copilotDemoTitle')}</h2>
      <button type="button" className="copilot-demo-close" aria-label={nextWord('copilotDemoClose')} onClick={onClose}>✕</button>
    </div>
    <video className={narrow ? 'phone' : 'desktop'} src={film.video} poster={film.poster} controls playsInline preload="metadata" />
    <p className="copilot-demo-note">{nextWord('copilotDemoNote')}</p>
  </dialog>, document.body)
}
