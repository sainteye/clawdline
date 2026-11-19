import { useEffect, useRef, useState, type MouseEvent } from "react"
import { isAppleTouchDevice, isStandaloneWebApp } from "../legacy/js/net/cloud-onboarding.js"
import { richText } from "../legacy/js/view/markdown.js"
import { bindNativeModalBackdrop } from "../modal-backdrop.js"
import type { Words } from "./words"

export function BotMessageMarkdown({ text, w }: { text: string; w: Words }) {
  const [original, setOriginal] = useState<HTMLAnchorElement | null>(null)
  const dialog = useRef<HTMLDialogElement>(null)

  useEffect(() => {
    const node = dialog.current
    if (!node || !original) return
    bindNativeModalBackdrop(node)
    node.showModal()
    return () => {
      node.close()
      if (original.isConnected) original.focus({ preventScroll: true })
    }
  }, [original])

  function chooseGmailDestination(event: MouseEvent<HTMLDivElement>) {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
    if (!isAppleTouchDevice(window) || !isStandaloneWebApp(window)) return
    const anchor = (event.target as Element).closest?.<HTMLAnchorElement>("a[href]")
    if (!anchor) return
    let url: URL
    try { url = new URL(anchor.href) } catch { return }
    if (url.origin !== "https://mail.google.com" || url.username || url.password || !/^\/mail(?:\/|$)/.test(url.pathname)) return
    event.preventDefault()
    setOriginal(anchor)
  }

  const close = () => setOriginal(null)
  return <>
    <div className="pa-markdown" onClick={chooseGmailDestination} dangerouslySetInnerHTML={{ __html: richText(text, { codeCopy: true }) }} />
    {original && <dialog ref={dialog} aria-label={w.gmailOpen} className="pa-gmail-link" data-modal-dismiss="true"
      onCancel={event => { event.preventDefault(); close() }}>
      <a className="pa-pill" href={`x-safari-${original.href}`} referrerPolicy="no-referrer">{w.gmailSafari}</a>
      <a className="pa-pill" href={original.href} target="_blank" rel="noopener noreferrer" onClick={close}>{w.gmailWeb}</a>
      <button className="pa-pill" type="button" onClick={close}>{w.close}</button>
    </dialog>}
  </>
}
