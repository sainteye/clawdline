import { useEffect, useState } from "react"

/** How long a done notice stays on screen. */
const NOTICE_SHOWN_MS = 5000

/**
 * The last thing the copilot did on the machine (`actionNotice`), shown for a
 * moment beside the call: in the sheet, and above the bubble when the call is
 * shrunk. Announced politely to a screen reader.
 */
export function CopilotNotice({ notice, className }: { notice?: { text: string; at: number; warn?: boolean }; className: string }) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!notice) return
    setNow(Date.now())
    const left = notice.at + NOTICE_SHOWN_MS - Date.now()
    if (left <= 0) return
    const timer = setTimeout(() => setNow(Date.now()), left)
    return () => clearTimeout(timer)
  }, [notice])
  const shown = !!notice && now < notice.at + NOTICE_SHOWN_MS
  return <div className={`${className}${notice?.warn ? " warn" : ""}`} role="status" aria-live="polite" hidden={!shown}>
    {shown ? <><span className="copilot-notice-tick" aria-hidden="true">{notice!.warn ? "!" : "✓"}</span>{notice!.text}</> : null}
  </div>
}
