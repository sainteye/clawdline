import { useCallback, useEffect, useRef, useState, type MouseEvent } from "react"
import { referenceImages } from "./reference-images.js"
import { failureWords } from "./shared.js"

/**
 * One reference picture as a card shows it: the small copy read when the card
 * is drawn, and the original read only when somebody opens it
 * (`reference-images.ts` says why the two are apart). Both are object URLs,
 * because through Cloud the bytes come back through `fetch` and there is no
 * address an `<img>` or a new tab could be pointed at; each is revoked when the
 * card goes.
 */
export function useReferenceImage(id: string) {
  const [source, setSource] = useState("")
  const [failed, setFailed] = useState("")
  const [full, setFull] = useState("")
  const [fullFailed, setFullFailed] = useState("")
  const [opening, setOpening] = useState(false)
  const pending = useRef<Promise<string> | null>(null)
  const alive = useRef(true)
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
      alive.current = false
      stop.abort()
      if (objectURL) URL.revokeObjectURL(objectURL)
    }
  }, [id])

  /** The original's object URL, read now if it has not been. */
  const loadFull = useCallback(async (): Promise<string> => {
    setFullFailed("")
    setOpening(true)
    try {
      return await readFull.current()
    } catch (error) {
      if (alive.current) setFullFailed(failureWords(error))
      throw error
    } finally {
      if (alive.current) setOpening(false)
    }
  }, [])

  /**
   * A link's press that opens the original in a new tab. Once the original is
   * here the link already points at it and the press is left alone; before
   * that the tab is opened during the press, which is the only moment a
   * browser lets a page open one, and pointed at the picture when it arrives.
   */
  const openFull = useCallback((event: MouseEvent<HTMLAnchorElement>) => {
    if (full) return
    event.preventDefault()
    const tab = window.open("", "_blank")
    // The tab only ever shows this page's own picture; it has no business
    // reaching back into the console.
    if (tab) tab.opener = null
    void loadFull().then((url) => {
      if (tab && !tab.closed) tab.location.href = url
      else window.open(url, "_blank", "noreferrer")
    }, () => { tab?.close() })
  }, [full, loadFull])

  return { source, failed, full, fullFailed, opening, loadFull, openFull }
}
