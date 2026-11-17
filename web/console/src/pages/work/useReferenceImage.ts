import { useCallback, useEffect, useRef, useState, type MouseEvent } from "react"
import { referenceImages } from "./reference-images.js"
import { failureWords } from "./shared.js"
import { openImagePreview } from "../../legacy/images-bridge.js"
import "../../session/lightbox-steps.css"

/**
 * One reference picture as a card shows it: the small copy read when the card
 * is drawn, and the original read only when somebody opens it
 * (`reference-images.ts` says why the two are apart). Both are object URLs,
 * because through Cloud the bytes come back through `fetch` and there is no
 * address an `<img>` or the shared viewer can use; each is revoked when the
 * card goes.
 */
export function useReferenceImage(id: string, name: string) {
  const [source, setSource] = useState("")
  const [failed, setFailed] = useState("")
  const [full, setFull] = useState("")
  const [fullFailed, setFullFailed] = useState("")
  const [opening, setOpening] = useState(false)
  const pending = useRef<Promise<string> | null>(null)
  const alive = useRef(true)
  const revision = useRef(0)
  const previewRequest = useRef(0)
  const releasePreview = useRef<(() => void) | null>(null)
  const readFull = useRef<() => Promise<string>>(() => Promise.reject(new Error("not mounted")))

  useEffect(() => {
    const stop = new AbortController()
    let objectURL = ""
    setSource("")
    setFailed("")
    void referenceImages.load(id, "thumb", stop.signal).then((blob) => {
      objectURL = URL.createObjectURL(blob)
      if (stop.signal.aborted) URL.revokeObjectURL(objectURL)
      else setSource(objectURL)
    }).catch((error: unknown) => { if (!stop.signal.aborted) setFailed(failureWords(error)) })
    return () => {
      stop.abort()
      if (objectURL) URL.revokeObjectURL(objectURL)
    }
  }, [id])

  // The original is read once per card and kept until the card goes, so a
  // second press, or the red pen putting the original back, reads nothing.
  useEffect(() => {
    revision.current++
    alive.current = true
    const stop = new AbortController()
    let objectURL = ""
    pending.current = null
    setFull("")
    setFullFailed("")
    const read = () => {
      pending.current ??= referenceImages.load(id, "full", stop.signal).then((blob) => {
        objectURL = URL.createObjectURL(blob)
        if (stop.signal.aborted) {
          URL.revokeObjectURL(objectURL)
          throw new DOMException("the picture is no longer shown", "AbortError")
        }
        setFull(objectURL)
        return objectURL
      }, (error: unknown) => {
        // A failed read is not kept: the next press tries again.
        pending.current = null
        throw error
      })
      return pending.current
    }
    readFull.current = read
    return () => {
      revision.current++
      previewRequest.current++
      alive.current = false
      stop.abort()
      releasePreview.current?.()
      releasePreview.current = null
      if (objectURL) URL.revokeObjectURL(objectURL)
    }
  }, [id])

  /** The original's object URL, read now if it has not been. */
  const loadFull = useCallback(async (): Promise<string> => {
    const current = revision.current
    setFullFailed("")
    setOpening(true)
    try {
      return await readFull.current()
    } catch (error) {
      if (alive.current && revision.current === current) setFullFailed(failureWords(error))
      throw error
    } finally {
      if (alive.current && revision.current === current) setOpening(false)
    }
  }, [])

  /** Read the original on demand and use the App's existing image viewer. */
  const openFull = useCallback((event: MouseEvent<HTMLButtonElement>) => {
    event.preventDefault()
    const trigger = event.currentTarget
    const request = ++previewRequest.current
    void loadFull().then((url) => {
      if (request !== previewRequest.current || !trigger.isConnected) return
      releasePreview.current?.()
      releasePreview.current = openImagePreview(trigger, url, name, () => {
        const release = releasePreview.current
        releasePreview.current = null
        release?.()
      })
    }, () => { /* loadFull keeps the failure beside the picture; another press retries. */ })
  }, [name, loadFull])

  return { source, failed, full, fullFailed, opening, loadFull, openFull }
}
