import { readPublishedDocument, type DocumentLocator } from "../legacy/documents-bridge.js"
import { mountStreamingVideo } from "../legacy/js/net/video-stream-player.js"

export const VIDEO_PREVIEW_READS = 1
export const VIDEO_PREVIEW_SIDE = 640
export const VIDEO_PREVIEW_BYTES = 2097152
export const VIDEO_PREVIEW_MILLISECONDS = 30000

let activeReads = 0
export function takeVideoPreviewSlot(): (() => void) | null {
  if (activeReads >= VIDEO_PREVIEW_READS) return null
  activeReads++
  let released = false
  return () => {
    if (released) return
    released = true; activeReads--
    document.dispatchEvent(new Event("clawdline:video-preview-slot"))
  }
}

/** A visible card borrows the authorized stream only until its first decoded frame. */
export async function readVideoPoster(locator: DocumentLocator, signal: AbortSignal): Promise<Blob> {
  const controller = new AbortController()
  const cancel = () => controller.abort()
  signal.addEventListener("abort", cancel, { once: true })
  if (signal.aborted) controller.abort()
  const timeout = setTimeout(cancel, VIDEO_PREVIEW_MILLISECONDS)
  let dispose: (() => void) | undefined
  const host = document.createElement("div")
  try {
    const answer = await readPublishedDocument(locator, { signal: controller.signal, stream: true, byteBudget: VIDEO_PREVIEW_BYTES })
    return await new Promise<Blob>((resolve, reject) => {
      let settled = false, capturing = false
      const finish = (blob?: Blob | null, error?: unknown) => {
        if (settled) return
        settled = true
        controller.signal.removeEventListener("abort", aborted)
        if (blob) resolve(blob)
        else reject(error ?? new Error("Video preview unavailable"))
      }
      const aborted = () => finish(undefined, new DOMException("Aborted", "AbortError"))
      controller.signal.addEventListener("abort", aborted, { once: true })
      if (controller.signal.aborted) { aborted(); return }
      dispose = mountStreamingVideo(host, answer, "", {
        playable(video: HTMLVideoElement) {
          if (settled || capturing) return
          capturing = true
          // loadeddata and canplay can both fire; take a frame only once.
          controller.signal.removeEventListener("abort", aborted)
          const canvas = document.createElement("canvas")
          const scale = Math.min(1, VIDEO_PREVIEW_SIDE / Math.max(video.videoWidth, video.videoHeight))
          canvas.width = Math.max(1, Math.round(video.videoWidth * scale))
          canvas.height = Math.max(1, Math.round(video.videoHeight * scale))
          try {
            const context = canvas.getContext("2d")
            if (!context) throw new Error("No preview canvas")
            context.drawImage(video, 0, 0, canvas.width, canvas.height)
            controller.abort(); dispose?.()
            canvas.toBlob(blob => finish(blob), "image/jpeg")
          } catch (caught) { finish(undefined, caught) }
        },
        error(caught: unknown) { if (!capturing) finish(undefined, caught) },
      })
    })
  } finally {
    clearTimeout(timeout); signal.removeEventListener("abort", cancel)
    controller.abort(); dispose?.(); host.replaceChildren()
  }
}
