import * as L from "../legacy/bridge.js"
import { PendingTurnFrame } from "../session/Transcript.js"

/** Remote receipts use the original pending-turn frame with a pinned outcome. */
export function PendingRemoteTurn({ text, pictures, sentAt, status, state }: {
  text: string; pictures: number; sentAt: number; status: string; state: "sending" | "accepted" | "unknown"
}) {
  const body = L.richTextHTML(text) + (pictures
    ? `<div class="pending-images">${L.escapeHTML(L.fillString(pictures === 1
      ? L.strings.webAttachedImage : L.strings.webAttachedImages, { n: pictures }))}</div>` : "") +
    `<div class="pending-state" role="status">${L.escapeHTML(status)}</div>`
  return <div className="tx cloud-pinned-pending"><PendingTurnFrame state={state}
    sentAt={sentAt} bodyHTML={body} /></div>
}
