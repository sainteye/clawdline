import type { SessionRow } from "@clawdline/contract"
import { RefusalError } from "@clawdline/core"
import { useState } from "react"
import { client } from "../client.js"
import * as L from "../legacy/bridge.js"
import { Mark } from "./List.js"
import { Transcript } from "./Transcript.js"
import { Composer } from "./Composer.js"

/**
 * The conversation, which is what this pane is for.
 *
 * In the original the transcript is the pane and the composer is under it, so a
 * person reads and answers without changing what they are looking at. That is
 * the arrangement being replicated, not a list with a detail popover.
 */
export function Detail({
  row,
  onBack,
  onDid,
}: {
  row: SessionRow | null
  onBack: () => void
  onDid: () => void
}) {
  const T = L.strings
  return (
    <section className="pane pane-detail" id="pane-detail">
      <div className="detail-head" id="detail-head">
        <button className="back" id="back" onClick={onBack} aria-label={T.webBackLabel}>
          ‹ {T.webBack}
        </button>
        <div className="detail-identity-block">
          <span className="detail-mark-go">
            <span className="detail-identity">
              <Mark icon={row?.icon} cellPx={6} id="detail-mark" />
            </span>
          </span>
          <span className="detail-session">
            <span className="who detail-who">
              <span className="name" id="detail-name">
                {row ? row.label || row.tty || row.id : T.webNoSessionOpen}
              </span>
              <span className="sub" id="detail-sub">
                {row ? [L.path(row.cwd), row.tty, row.id].filter(Boolean).join(" · ") : ""}
              </span>
            </span>
          </span>
        </div>
        <div className="tools">
          <Tools row={row} onDid={onDid} />
        </div>
      </div>

      <div className="scroller tx-scroll" id="tx-scroll">
        <div className="tx" id="tx">
          {row ? <Transcript id={row.id} /> : null}
        </div>
      </div>

      <Composer row={row} onDid={onDid} />
    </section>
  )
}

/**
 * The detail pane's controls, as the original has them.
 *
 * One chip and a menu of six, and no more: the original has no interrupt button
 * here, so neither does this. The daemon can interrupt and the dashboard offers
 * it, but adding a control the screen being replicated does not have would make
 * this a different screen with the same paint.
 *
 * Items whose route this daemon does not own yet are disabled rather than
 * dropped. A menu that is missing rows is a menu somebody will assume they
 * imagined; a disabled row says which part is not here.
 */
function Tools({ row, onDid }: { row: SessionRow | null; onDid: () => void }) {
  const T = L.strings
  const [open, setOpen] = useState(false)
  const [confirm, setConfirm] = useState(false)
  const [said, setSaid] = useState<string | null>(null)

  const close = async () => {
    if (!row) return
    try {
      await client.close(row.id)
      setSaid(null)
      onDid()
    } catch (err) {
      setSaid(err instanceof RefusalError ? err.detail : String(err))
    } finally {
      setConfirm(false)
      setOpen(false)
    }
  }

  return (
    <>
      <button className="chip" id="tx-focus" type="button" disabled title={T.webShowOnMac}>
        <span id="tx-focus-label">{T.webShowOnMac}</span>
      </button>
      <div className="detail-actions">
        <button
          className="detail-more"
          id="detail-actions-trigger"
          type="button"
          aria-haspopup="menu"
          aria-expanded={open}
          disabled={!row}
          onClick={() => setOpen((v) => !v)}
        >
          <svg viewBox="0 0 18 14" aria-hidden="true" focusable="false">
            <circle cx="3" cy="7" r="1.5" />
            <circle cx="9" cy="7" r="1.5" />
            <circle cx="15" cy="7" r="1.5" />
          </svg>
        </button>
        <div className="session-actions" id="session-actions" role="menu" hidden={!open}>
          <div className="session-action-stage">
            <div className="session-action-level" data-place="current">
              <button type="button" role="menuitem" disabled>
                {T.webShowOnMac}
              </button>
              <button type="button" role="menuitem" disabled>
                {T.webSessionInfo}
              </button>
              <button type="button" role="menuitem" disabled>
                {T.webSessionScreen}
              </button>
              <button type="button" role="menuitem" disabled>
                {T.webSessionGit}
              </button>
              {!confirm ? (
                <button className="end" type="button" role="menuitem" onClick={() => setConfirm(true)}>
                  {T.webEndSession}
                </button>
              ) : (
                <button className="end" type="button" role="menuitem" onClick={() => void close()}>
                  {T.webConfirm}
                </button>
              )}
            </div>
          </div>
        </div>
      </div>
      {said && (
        <span className="chip" title={said}>
          {said}
        </span>
      )}
    </>
  )
}

