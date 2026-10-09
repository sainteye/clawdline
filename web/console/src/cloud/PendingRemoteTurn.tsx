import * as L from "../legacy/bridge.js"
import { PendingTurnFrame } from "../session/Transcript.js"

/** Remote receipts use the original pending-turn frame with a pinned outcome. */
export function PendingRemoteTurn({ text, images, sentAt, status, state }: {
  text: string; images: readonly string[]; sentAt: number; status: string; state: "sending" | "accepted" | "unknown"
}) {
  const body = L.richTextHTML(text) + (images.length
    ? `<div class="pending-images">${L.escapeHTML(L.fillString(images.length === 1
      ? L.strings.webAttachedImage : L.strings.webAttachedImages, { n: images.length }))}</div>` : "") +
    `<div class="pending-state" role="status">${L.escapeHTML(status)}</div>`
  return <div className="tx cloud-pinned-pending"><PendingTurnFrame state={state}
    sentAt={sentAt} bodyHTML={body} />
    {images.length > 0 && <div className="cloud-pinned-draft-images">{images.map((url, index) =>
      <img key={index} src={url} alt={L.strings.webImagePreview} />)}</div>}
  </div>
}
