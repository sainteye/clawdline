import { readPublishedDocument, type DocumentLocator } from "../legacy/documents-bridge.js"
import { videoPosterScore } from "./video-poster-quality.js"
import { mountStreamingVideo } from "../legacy/js/net/video-stream-player.js"

export const VIDEO_PREVIEW_READS = 1
export const VIDEO_PREVIEW_SIDE = 640
export const VIDEO_PREVIEW_BYTES = 4194304
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

/** Decode bounded candidates from different parts of the movie, retaining the most detailed frame. */
export async function readVideoPoster(locator: DocumentLocator, signal: AbortSignal): Promise<Blob> {
  const controller = new AbortController(), cancel = () => controller.abort()
  signal.addEventListener("abort", cancel, { once: true })
  if (signal.aborted) controller.abort()
  const timeout = setTimeout(cancel, VIDEO_PREVIEW_MILLISECONDS)
  let best: Blob | undefined, score = -1
  try {
    const answer = await readPublishedDocument(locator, { signal: controller.signal, poster: true,
      byteBudget: VIDEO_PREVIEW_BYTES }) as { frames(capture: (stream: unknown, signal: AbortSignal) => Promise<void>): Promise<void> }
    await answer.frames(async (stream, frameSignal) => {
      const candidate = await capturePosterFrame(stream, frameSignal)
      if (candidate.score > score) { best = candidate.blob; score = candidate.score }
    })
    if (!best) throw new Error("Video preview unavailable")
    return best
  } catch (error) {
    // A deadline can end sampling after a useful candidate. External cancellation
    // still discards it; authorization refusals are never treated as a deadline.
    if (best && !signal.aborted && controller.signal.aborted) return best
    throw error
  } finally {
    clearTimeout(timeout); signal.removeEventListener("abort", cancel); controller.abort()
  }
}

function capturePosterFrame(answer: unknown, signal: AbortSignal): Promise<{ blob: Blob; score: number }> {
  const host = document.createElement("div")
  let dispose: (() => void) | undefined
  return new Promise<{ blob: Blob; score: number }>((resolve, reject) => {
    let settled = false, capturing = false
    const finish = (result?: { blob: Blob; score: number }, error?: unknown) => {
      if (settled) return
      settled = true; signal.removeEventListener("abort", aborted)
      dispose?.(); host.replaceChildren()
      if (result) resolve(result)
      else reject(error ?? new Error("Video preview unavailable"))
    }
    const aborted = () => finish(undefined, new DOMException("Aborted", "AbortError"))
    signal.addEventListener("abort", aborted, { once: true })
    if (signal.aborted) { aborted(); return }
    dispose = mountStreamingVideo(host, answer, "", {
      seekToPosterTime: true,
      playable(video: HTMLVideoElement) {
        if (settled || capturing) return
        capturing = true
        const canvas = document.createElement("canvas")
        const scale = Math.min(1, VIDEO_PREVIEW_SIDE / Math.max(video.videoWidth, video.videoHeight))
        canvas.width = Math.max(1, Math.round(video.videoWidth * scale))
        canvas.height = Math.max(1, Math.round(video.videoHeight * scale))
        try {
          const context = canvas.getContext("2d")
          if (!context) throw new Error("No preview canvas")
          context.drawImage(video, 0, 0, canvas.width, canvas.height)
          const quality = videoPosterScore(context.getImageData(0, 0, canvas.width, canvas.height).data, canvas.width, canvas.height)
          // Stop the decoder immediately; the enclosing candidate cancels reads.
          dispose?.()
          canvas.toBlob(blob => blob ? finish({ blob, score: quality }) : finish(), "image/jpeg")
        } catch (caught) { finish(undefined, caught) }
      },
      error(caught: unknown) { if (!capturing) finish(undefined, caught) },
    })
  })
}
