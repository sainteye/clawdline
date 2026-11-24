import { useEffect, useRef, useState, type MouseEvent } from "react"
import { isAppleTouchDevice, isStandaloneWebApp } from "../legacy/js/net/cloud-onboarding.js"
import { richText } from "../legacy/js/view/markdown.js"
import { bindNativeModalBackdrop } from "../modal-backdrop.js"
import type { Words } from "./words"

/** A run of text to draw as a mention chip; offsets are UTF-16 units of text. */
export type MarkdownMention = { start: number; end: number; id: string; title?: string }

const escape = (s: string) => s.replace(/[&<>"']/g, c => `&#${c.charCodeAt(0)};`)

// Each run is swapped for a private-use placeholder before Markdown and for
// its chip after, so the chip covers exactly the source's run.
function markdownWithMentions(text: string, mentions: readonly MarkdownMention[]): string {
  if (!mentions.length) return richText(text, { codeCopy: true })
  let source = "", at = 0
  mentions.forEach((m, i) => { source += text.slice(at, m.start) + `\uE000${i}\uE001`; at = m.end })
  source += text.slice(at)
  return richText(source, { codeCopy: true }).replace(/\uE000(\d+)\uE001/g, (_, i: string) => {
    const m = mentions[Number(i)]
    return `<span class="pa-mention-chip" data-mention="${escape(m.id)}"${m.title ? ` title="${escape(m.title)}"` : ""}>${escape(text.slice(m.start, m.end))}</span>`
  })
}

export function BotMessageMarkdown({ text, w, mentions = [] }: { text: string; w: Words; mentions?: readonly MarkdownMention[] }) {
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
    <div className="pa-markdown" onClick={chooseGmailDestination} dangerouslySetInnerHTML={{ __html: markdownWithMentions(text, mentions) }} />
    {original && <dialog ref={dialog} aria-label={w.gmailOpen} className="pa-gmail-link" data-modal-dismiss="true"
      onCancel={event => { event.preventDefault(); close() }}>
      <a className="pa-pill" href={`x-safari-${original.href}`} referrerPolicy="no-referrer">{w.gmailSafari}</a>
      <a className="pa-pill" href={original.href} target="_blank" rel="noopener noreferrer" onClick={close}>{w.gmailWeb}</a>
      <button className="pa-pill" type="button" onClick={close}>{w.close}</button>
    </dialog>}
  </>
}
