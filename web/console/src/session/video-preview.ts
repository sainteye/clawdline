import { catalogFormat, catalogWord } from "../catalog.js"
import { mountVideo } from "../legacy/js/net/video.js"
import { failureSentence } from "../legacy/js/core/failure-text.js"
import { showModalWithoutChoice } from "../modal-focus.js"
import type { DocumentLocator } from "../legacy/documents-bridge.js"
import { enterVideoHistory, closeVideoHistory } from "./video-history.js"
import { videoCardPosters } from "./video-card-state.js"
import { videoLocatorFromHash, videoLocatorFromLink } from "./video-preview-links.js"

type ReadVideo = (locator: DocumentLocator, options: {
  signal: AbortSignal; stream: boolean; progress(percent: number): void
}) => Promise<unknown>

/** One modal, one bounded transfer. Closing destroys the decoder and cancels pending reads. */
export function bindVideoPreview(doc: Document, read: ReadVideo): () => void {
  const win = doc.defaultView!
  const dialog = doc.createElement("dialog")
  dialog.className = "video-lightbox"
  dialog.setAttribute("aria-labelledby", "video-preview-title")
  dialog.innerHTML = '<section class="video-lightbox-frame"><header>' +
    '<h2 id="video-preview-title"></h2><button class="video-lightbox-close" type="button"></button>' +
    '</header><div class="video-lightbox-body"><div class="video-lightbox-player"></div>' +
    '<div class="video-lightbox-cover"><span class="video-lightbox-spinner" aria-hidden="true"></span>' +
    '<button class="video-lightbox-play" type="button">▶</button></div></div><p class="video-lightbox-status" role="status" aria-live="polite"></p>' +
    '<button class="video-lightbox-retry" type="button" hidden></button></section>'
  doc.body.append(dialog)
  const title = dialog.querySelector<HTMLElement>("h2")!
  const close = dialog.querySelector<HTMLButtonElement>(".video-lightbox-close")!
  const body = dialog.querySelector<HTMLElement>(".video-lightbox-body")!
  const player = dialog.querySelector<HTMLElement>(".video-lightbox-player")!
  const cover = dialog.querySelector<HTMLElement>(".video-lightbox-cover")!
  const play = dialog.querySelector<HTMLButtonElement>(".video-lightbox-play")!
  const status = dialog.querySelector<HTMLElement>(".video-lightbox-status")!
  const retry = dialog.querySelector<HTMLButtonElement>(".video-lightbox-retry")!
  let pending: AbortController | null = null
  let dispose: (() => void) | null = null
  let locator: DocumentLocator | null = null
  let opener: HTMLElement | null = null
  let openerHref = ""
  let overflow = ""
  let posterURL: string | null = null
  let started = false
  let lastHash = ""
  const step = win.crypto.randomUUID()
  const ownsStep = () => win.history.state?.videoPreview === step
  const stop = () => {
    pending?.abort(); pending = null
    dispose?.(); dispose = null
    player.replaceChildren()
  }
  const restoreFocus = () => {
    const target = opener?.isConnected ? opener : openerHref
      ? [...doc.querySelectorAll<HTMLAnchorElement>("a[href]")].find(link => link.href === openerHref)
      : doc.getElementById("detail-info")
    target?.focus({ preventScroll: true })
  }
  const shut = () => {
    stop()
    if (!dialog.open) return
    dialog.close()
    doc.body.style.overflow = overflow
    if (posterURL) { win.URL.revokeObjectURL(posterURL); posterURL = null }
    body.style.backgroundImage = ""
    doc.dispatchEvent(new Event("clawdline:video-preview-slot"))
    restoreFocus()
  }
  const dismiss = () => {
    if (!dialog.open) return
    closeVideoHistory(win.history, step)
    shut()
  }
  const load = async () => {
    if (!locator || !dialog.open) return
    stop()
    const controller = new AbortController()
    pending = controller
    retry.hidden = true
    started = false
    cover.hidden = false
    cover.dataset.state = "loading"
    play.hidden = false
    play.disabled = true
    status.textContent = catalogFormat("documents", "videoLoading", [0])
    status.dataset.state = "loading"
    try {
      const answer = await read(locator, { signal: controller.signal, stream: true, progress(percent) {
        if (pending === controller && !controller.signal.aborted)
          status.textContent = catalogFormat("documents", "videoLoading", [percent])
      } }) as { media_type: string; blob?: Blob; stream?: unknown; path: string }
      if (pending !== controller || controller.signal.aborted || !dialog.open) return
      if (answer.media_type !== "video/mp4" || (!(answer.blob instanceof Blob) && typeof answer.stream !== "function"))
        throw Object.assign(new Error("Invalid video answer"), { code: "video_answer_invalid" })
      const failed = (caught: unknown) => {
        if (pending !== controller || controller.signal.aborted || !dialog.open) return
        controller.abort()
        status.dataset.state = "error"
        cover.dataset.state = "error"
        play.disabled = true
        status.textContent = failureSentence(caught, catalogWord("documents", "videoReadFailed"))
        retry.hidden = false
      }
      const ready = () => {
        if (pending !== controller || controller.signal.aborted || !dialog.open) return
        status.textContent = ""
        status.dataset.state = "ready"
      }
      if (!answer.stream) ready()
      if (!answer.stream) cover.hidden = true
      dispose = mountVideo(player, answer, catalogWord("documents", "videoError"), dismiss, {
        error: failed, ready, poster: posterURL,
        playable() {
          if (pending !== controller || controller.signal.aborted) return
          play.disabled = false
          if (!started) { cover.dataset.state = "ready"; play.hidden = false }
        },
        playing() { started = true; cover.hidden = true },
        waiting() { if (started) { cover.hidden = false; cover.dataset.state = "loading"; play.hidden = true } },
        paused() { if (!controller.signal.aborted) { started = false; cover.hidden = false; cover.dataset.state = "ready"; play.hidden = false } },
      })
    } catch (error) {
      if (pending !== controller || controller.signal.aborted || !dialog.open) return
      status.dataset.state = "error"
      cover.dataset.state = "error"
      play.disabled = true
      status.textContent = failureSentence(error, catalogWord("documents", "videoReadFailed"))
      retry.hidden = false
    }
  }
  const open = (value: DocumentLocator, trigger: HTMLElement | null, push = true) => {
    locator = value
    opener = trigger === doc.body ? null : trigger
    openerHref = trigger?.matches("a[href]") ? (trigger as HTMLAnchorElement).href : ""
    if (posterURL) win.URL.revokeObjectURL(posterURL)
    const card = trigger?.closest("a.video-preview-card") ??
      [...doc.querySelectorAll<HTMLAnchorElement>("a.video-preview-card")].find(link =>
        JSON.stringify(videoLocatorFromLink(link.href, win.location.href)) === JSON.stringify(value))
    const poster = card ? videoCardPosters.get(card) : undefined
    posterURL = poster ? win.URL.createObjectURL(poster) : null
    body.style.backgroundImage = posterURL ? 'url("' + posterURL + '")' : ""
    title.textContent = catalogWord("documents", "videoPreview")
    title.title = value.path
    close.textContent = catalogWord("documents", "videoClose")
    retry.textContent = catalogWord("documents", "retry")
    play.setAttribute("aria-label", catalogWord("documents", "videoPlay"))
    if (!dialog.open) {
      overflow = doc.body.style.overflow
      doc.body.style.overflow = "hidden"
      showModalWithoutChoice(dialog)
    }
    if (push) enterVideoHistory(win.history, step)
    doc.dispatchEvent(new Event("clawdline:video-preview-pause"))
    void load()
  }
  const click = (event: MouseEvent) => {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
    const link = (event.target as Element | null)?.closest<HTMLAnchorElement>("a[href]")
    if (!link || link.hasAttribute("download")) return
    const value = videoLocatorFromLink(link.href, win.location.href)
    if (!value) return
    event.preventDefault()
    event.stopImmediatePropagation()
    open(value, link)
  }
  const followHash = () => {
    if (win.location.hash === lastHash) return
    lastHash = win.location.hash
    const value = videoLocatorFromHash(lastHash)
    if (value) open(value, doc.activeElement as HTMLElement | null)
    else if (dialog.open) shut()
  }
  const pop = (event: PopStateEvent) => {
    if (dialog.open && !ownsStep()) {
      lastHash = win.location.hash
      shut()
      restoreFocus()
      event.stopImmediatePropagation()
    } else if (ownsStep() && locator && !dialog.open) {
      open(locator, opener, false)
      event.stopImmediatePropagation()
    }
  }
  const cancel = (event: Event) => { event.preventDefault(); dismiss() }
  const key = (event: KeyboardEvent) => {
    if (!dialog.open || event.key !== "Escape" || doc.fullscreenElement) return
    event.preventDefault()
    event.stopImmediatePropagation()
    dismiss()
  }
  const backdrop = (event: MouseEvent) => { if (event.target === dialog) dismiss() }
  play.addEventListener("click", () => {
    const video = player.querySelector<HTMLVideoElement>("video")
    if (video) void video.play().catch(() => { status.textContent = catalogWord("documents", "videoError") })
  })
  close.addEventListener("click", dismiss)
  retry.addEventListener("click", load)
  dialog.addEventListener("cancel", cancel)
  dialog.addEventListener("click", backdrop)
  doc.addEventListener("click", click, true)
  doc.addEventListener("keydown", key, true)
  win.addEventListener("popstate", pop, true)
  win.addEventListener("hashchange", followHash)
  followHash()
  return () => {
    shut()
    doc.removeEventListener("click", click, true)
    doc.removeEventListener("keydown", key, true)
    win.removeEventListener("popstate", pop, true)
    win.removeEventListener("hashchange", followHash)
    dialog.remove()
  }
}
