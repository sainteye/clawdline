import { useEffect, useMemo, useRef, useState } from "react"
import { catalogWord } from "../catalog.js"
import type { DocumentLocator } from "../legacy/documents-bridge.js"
import { videoCardsInMarkup } from "./video-card-links.js"
import { videoCardPosters } from "./video-card-state.js"
import { readVideoPoster, takeVideoPreviewSlot } from "./video-poster.js"
import "./video-cards.css"

function VideoCard({ href, locator }: { href: string; locator: DocumentLocator }) {
  const link = useRef<HTMLAnchorElement>(null)
  const [poster, setPoster] = useState("")
  const [state, setState] = useState("loading")
  useEffect(() => {
    const node = link.current!
    let visible = false, done = false, cancelled = false
    let controller: AbortController | null = null
    let release: (() => void) | null = null
    let url = ""
    const pause = () => controller?.abort()
    const attempt = () => {
      if (cancelled || done || !visible || controller || document.querySelector("dialog.video-lightbox[open]")) return
      const slot = takeVideoPreviewSlot()
      if (!slot) return
      release = slot
      const reading = new AbortController()
      controller = reading
      void readVideoPoster(locator, reading.signal).then(blob => {
        if (cancelled || reading.signal.aborted) return
        url = URL.createObjectURL(blob)
        videoCardPosters.set(node, blob)
        done = true; setPoster(url); setState("ready")
      }).catch(() => {
        if (!cancelled && !reading.signal.aborted) { done = true; setState("unavailable") }
      }).finally(() => {
        controller = null
        const free = release; release = null
        free?.()
      })
    }
    const observer = new IntersectionObserver(entries => {
      visible = entries.some(entry => entry.isIntersecting)
      if (visible) attempt()
      else pause()
    })
    observer.observe(node)
    document.addEventListener("clawdline:video-preview-slot", attempt)
    document.addEventListener("clawdline:video-preview-pause", pause)
    return () => {
      cancelled = true; observer.disconnect(); pause()
      document.removeEventListener("clawdline:video-preview-slot", attempt)
      document.removeEventListener("clawdline:video-preview-pause", pause)
      videoCardPosters.delete(node)
      if (url) URL.revokeObjectURL(url)
    }
    // The canonical href pins this card's complete publication locator.
  }, [href])
  return <a ref={link} className="video-preview-card" href={href} aria-label={catalogWord("documents", "videoPreview")} data-preview-state={state}>
    <span className="video-preview-card-image">
      {poster && <img src={poster} alt="" />}
      <span className="video-preview-card-play" aria-hidden="true">▶</span>
      {state === "loading" && <span className="video-preview-card-loading" aria-hidden="true" />}
    </span>
    <span className="video-preview-card-caption">{locator.path.split("/").at(-1)}</span>
    {state === "unavailable" && <span className="video-preview-card-note">{catalogWord("documents", "videoPosterUnavailable")}</span>}
  </a>
}

export function VideoCards({ markup }: { markup: string }) {
  const base = typeof location === "undefined" ? "https://app.clawdline.com/" : location.href
  const cards = useMemo(() => videoCardsInMarkup(markup, base), [markup, base])
  if (!cards.length) return null
  return <div className="video-preview-cards">{cards.map(card => <VideoCard key={card.href} {...card} />)}</div>
}
