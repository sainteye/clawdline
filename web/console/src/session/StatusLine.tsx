import { useRef } from "react"
import type { SessionRow } from "@clawdline/contract"
import * as L from "../legacy/bridge.js"

/**
 * The status line under the open conversation — the original's `footer#status-line`.
 *
 * How to use it: render it in `section.pane-detail`, **directly after
 * `<Composer />`**, as the last child of the pane, and give it the same row:
 *
 *     <Composer row={row} onDid={onDid} />
 *     <StatusLine row={row} />
 *
 * It is inside the column and not across the foot of the window — see the
 * original's markup for why — and `legacy/status-line.css` styles it by
 * `.status-line`, so it needs no wrapper. `listPending` is optional: pass `true`
 * while the session list has not answered yet, and the empty row stays blank
 * instead of asking the reader to pick a session from a list that is not there
 * (the original's `listUnknown`).
 *
 * The four parts are all here, in the original's order: `.open`, `.files`,
 * `.deploy`, `.limits`. What this daemon can fill is filled; the rest is the
 * original's empty element, because the only reads here are `/v1/health` and
 * `/v1/sessions`:
 *
 * - `.open` carries the assistant's logo and name. The model, context use and
 *   cost come from the Swift app's session-info read, which this daemon does not
 *   serve, so those items are not drawn. The original draws "Loading…" in their
 *   place while that read is out; with no read to wait for, that word would be
 *   untrue for as long as the page is open, so it is left off. The button is
 *   disabled because the Session info card it opens does not exist here.
 * - `.files` stays hidden: the working tree is `git status` from the same read.
 * - `.deploy` stays hidden: a running deploy comes from the project-link walk,
 *   which this daemon does not have.
 * - `.limits` stays empty: plan windows come from the same session-info read.
 */
export function StatusLine({ row, listPending = false }: { row: SessionRow | null; listPending?: boolean }) {
  const T = L.strings
  // The original draws only when the open session changes, so a page that has
  // never had one open shows the markup as written: a bare button, no title.
  const drawn = useRef(false)
  if (row) drawn.current = true

  let open
  if (!drawn.current) {
    open = <button className="open" id="status-line-open" type="button" disabled></button>
  } else {
    open = (
      <button
        className="open"
        id="status-line-open"
        type="button"
        title={`${T.webSessionInfo} (⌘I)`}
        aria-label={T.webSessionInfo}
        disabled
      >
        {row ? (
          <span className="item model" dangerouslySetInnerHTML={{ __html: modelHTML(row.assistant) }} />
        ) : listPending ? null : (
          <span className="empty">{T.webPickSession}</span>
        )}
      </button>
    )
  }

  return (
    <footer className="status-line" id="status-line">
      {open}
      <button className="files" id="status-line-files" type="button" hidden></button>
      <a
        className="deploy"
        id="status-line-deploy"
        hidden
        target="_blank"
        rel="noopener noreferrer"
        {...(drawn.current ? { "data-kind": "" } : {})}
      ></a>
      <div className="limits" id="status-line-limits"></div>
    </footer>
  )
}

/**
 * The logo and the name, as `identityHTML` writes them: `pixels.js`'s
 * `assistantLogo` followed by `<span class="word">`. The bridge hands the two over
 * joined (`whoHTML`), so the name's span is given its class here. An assistant
 * with no logo is `assistantName`'s own fallback word.
 */
function modelHTML(assistant: string | undefined): string {
  if (!L.hasLogo(assistant)) return '<span class="word">assistant</span>'
  return L.whoHTML(assistant).replace(/<span>([^<]*)<\/span>$/, '<span class="word">$1</span>')
}
