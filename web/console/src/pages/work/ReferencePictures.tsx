import { useEffect, useMemo, useRef, useState } from "react"
import { createPortal } from "react-dom"
import { isPicture } from "../../legacy/shots-bridge.js"
import { ImageMarkup, type MarkupPicture } from "../../session/ImageMarkup.js"
import { WorkIcon } from "./WorkIcon.js"

export const MAX_REFERENCE_PICTURES = 6

/**
 * The red marks burned into a picture, as a file the upload path already
 * knows how to shrink. PNG keeps a screenshot's text crisp; the Board's own
 * `prepareReferencePicture` re-encodes anything that is still too large.
 */
export function markedFile(canvas: HTMLCanvasElement, name: string): File {
  const url = canvas.toDataURL("image/png")
  const bytes = atob(url.slice(url.indexOf(",") + 1))
  const data = new Uint8Array(bytes.length)
  for (let i = 0; i < bytes.length; i++) data[i] = bytes.charCodeAt(i)
  const base = name.replace(/\.[^./]+$/, "") || "reference image"
  return new File([data], `${base}.png`, { type: "image/png", lastModified: Date.now() })
}

/**
 * The same red-pen sheet the Session composer uses, lifted out of whatever
 * dialog opened it. A Board or to-do dialog is itself a fixed layer, so the
 * sheet lives beside the app root and stacks above it.
 */
export function PictureMarkup({ picture, onCancel, onSave }: {
  picture: MarkupPicture
  onCancel: () => void
  onSave: (canvas: HTMLCanvasElement) => boolean
}) {
  return createPortal(<ImageMarkup shot={picture} onCancel={onCancel} onSave={onSave} />, document.body)
}

/**
 * Pictures chosen in a create dialog and not uploaded yet. Each is shown as a
 * thumbnail; pressing one opens the red pen, and saving swaps the file for
 * the marked copy before anything leaves the browser.
 */
export function PendingPictures({ images, busy, note, onChange }: {
  images: File[]
  busy: boolean
  note: string
  onChange: (images: File[]) => void
}) {
  const picker = useRef<HTMLInputElement>(null)
  const [marking, setMarking] = useState(-1)
  const previews = useMemo(() => images.map((file) => URL.createObjectURL(file)), [images])
  useEffect(() => () => { for (const url of previews) URL.revokeObjectURL(url) }, [previews])
  const markingFile = marking >= 0 ? images[marking] : undefined
  return <div className="work-modal-images">
    <span>參考圖片</span>
    <input ref={picker} type="file" accept="image/*,.heic,.heif" multiple hidden onChange={(event) => {
      const selected = Array.from(event.currentTarget.files ?? []).filter(isPicture)
      event.currentTarget.value = ""
      onChange([...images, ...selected].slice(0, MAX_REFERENCE_PICTURES))
    }} />
    <div className="work-modal-image-tools">
      <button className="chip" type="button" disabled={busy || images.length >= MAX_REFERENCE_PICTURES}
        onClick={() => picker.current?.click()}>＋ 加入參考圖片</button>
      <small>{images.length} / {MAX_REFERENCE_PICTURES} · {note}</small>
    </div>
    {!!images.length && <ul className="work-modal-image-list work-pending-pictures">{images.map((file, index) =>
      <li key={`${file.name}-${file.lastModified}-${index}`}>
        <button className="work-pending-picture" type="button" disabled={busy} title={file.name}
          aria-label={`用紅筆標記 ${file.name}`} onClick={() => setMarking(index)}>
          <img src={previews[index]} alt="" />
          <span>{file.name}</span>
          <span className="work-pending-picture-pen" aria-hidden="true"><WorkIcon name="edit" /></span>
        </button>
        <button type="button" disabled={busy} aria-label={`移除 ${file.name}`}
          onClick={() => onChange(images.filter((_, at) => at !== index))}><WorkIcon name="close" /></button>
      </li>)}</ul>}
    {markingFile && <PictureMarkup picture={{ id: `${marking}-${markingFile.lastModified}`, url: previews[marking] }}
      onCancel={() => setMarking(-1)}
      onSave={(canvas) => {
        const at = marking
        onChange(images.map((file, index) => index === at ? markedFile(canvas, file.name) : file))
        return true
      }} />}
  </div>
}
