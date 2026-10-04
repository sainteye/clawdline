import { useEffect, useState } from "react"
import { nextWord } from "../next-strings.js"
import { readUpdateStatus } from "./update.js"
import { UPDATE_READ_EVERY_MS, updateNotice } from "./update-model.js"

/**
 * One quiet line on the Settings page when this machine trails the cloud's
 * latest build (docs/updates.md). It reads once on mount and then every ten
 * minutes at most; the daemon answers from its own background check, so a
 * read never waits on the network. Nothing renders unless the state is
 * `update_available` or `differs`.
 */
export function UpdateNotice() {
  const [line, setLine] = useState<string | null>(null)

  useEffect(() => {
    let live = true
    const read = () =>
      void readUpdateStatus().then((status) => {
        if (live) setLine(updateNotice(status, nextWord))
      })
    read()
    const timer = setInterval(read, UPDATE_READ_EVERY_MS)
    return () => {
      live = false
      clearInterval(timer)
    }
  }, [])

  if (!line) return null
  return (
    <p className="say" id="settings-update-notice" role="status">
      {line}
    </p>
  )
}
