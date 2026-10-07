import { useState, useSyncExternalStore } from "react"
import { nextWord } from "../next-strings.js"
import { SETTINGS_UPDATE_HREF } from "./needs-update-model.js"
import { currentUpdateRead, subscribeUpdate } from "./update.js"
import { UPDATE_BANNER_DISMISSED_KEY, updateBannerVersion } from "./update-model.js"
import "./update.css"

function dismissedVersion(): string | null {
  try {
    return localStorage.getItem(UPDATE_BANNER_DISMISSED_KEY)
  } catch {
    return null
  }
}

/**
 * One small line at the top of the session list when this machine's release
 * install has a newer release: the way to the Settings page, where the update
 * is one press. Dismissed per version — a dismissal is forgotten when the
 * next release arrives — and only in this browser; when storage is refused
 * the dismissal lasts as long as the page does.
 *
 * It shares the Settings panel's reading (`update.ts`), so it adds no request
 * of its own beyond one read every ten minutes while it is on screen.
 */
export function UpdateBanner() {
  const read = useSyncExternalStore(subscribeUpdate, currentUpdateRead, currentUpdateRead)
  const [dismissed, setDismissed] = useState(dismissedVersion)
  const version = updateBannerVersion(read?.kind === "status" ? read.status : null, dismissed)
  if (!version) return null

  const dismiss = () => {
    setDismissed(version)
    try {
      localStorage.setItem(UPDATE_BANNER_DISMISSED_KEY, version)
    } catch {
      /* not remembered past this page; nothing else depends on it */
    }
  }

  return (
    <div className="update-banner" id="update-banner" role="status" data-version={version}>
      <span className="update-banner-say">{nextWord("updateBanner", { version })}</span>
      <a className="update-banner-go" href={SETTINGS_UPDATE_HREF}>{nextWord("updateBannerGo")}</a>
      <button className="x" type="button" aria-label={nextWord("updateBannerDismiss")} title={nextWord("updateBannerDismiss")} onClick={dismiss}>
        ×
      </button>
    </div>
  )
}
