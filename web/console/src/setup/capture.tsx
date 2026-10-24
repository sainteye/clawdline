// The setup wizard's capture page, for documentation screenshots.
//
// It draws the real wizard components (`SetupGuide.tsx`) with fixed sample
// data, so a screen can be photographed without a daemon, a Cloud account or a
// live pairing. Nothing here reads or writes anything: every handler is a
// no-op. The QR carries a deliberately invalid pairing link, so scanning it
// pairs nothing.
//
// Run `npm run dev` in web/ and open
// `/setup-capture.html?screen=<screen>&lang=<en|zh-Hant>`, where screen is one
// of welcome, app, browser, phone-apple, phone-android or done.
import { createRoot } from "react-dom/client"
import { activateCatalog, type CatalogTag } from "../catalog.js"
import type { CloudLogin, CloudPairing } from "../pages/settings/cloud.js"
import { AppStep, BrowserStep, Finished, PhoneStep, Welcome, Wizard } from "./SetupGuide.js"
import type { SetupItem } from "./progress.js"
import "../legacy/tokens.css"
import "./setup.css"

/** A pairing link no machine issued: its fragment is not an invitation, so a scan is refused. */
export const SAMPLE_PAIRING_LINK = "https://app.clawdline.com/#pair=sample-not-a-pairing-invitation"

const ITEMS: readonly SetupItem[] = ["app", "browser", "phone"]
const NONE = { app: false, browser: false, phone: false }
const nothing = () => undefined

const params = new URLSearchParams(location.search)
const screen = params.get("screen") ?? "welcome"
const tag: CatalogTag = params.get("lang") === "zh-Hant" ? "zh-Hant" : "en"

const waiting: CloudPairing = {
  phase: "waiting",
  link: SAMPLE_PAIRING_LINK,
  expires_at: Math.floor(Date.now() / 1000) + 600,
}
// A sign-in that has started: the button is the link to the sign-in page. The
// person's code is drawn only once they press it, so none appears here.
const signIn: CloudLogin = { phase: "waiting", verification_url: "https://app.clawdline.com/" }

function Screen() {
  switch (screen) {
    case "app":
      return (
        <Wizard items={ITEMS} current="app" done={NONE} skipped={NONE} onClose={nothing}>
          <AppStep login={signIn} configured={false} lineUp={false} paired={false} running={false} inApp
            onOpenCloud={null} onRun={nothing} onSignIn={nothing} onBack={nothing} onLocal={nothing}
            onSkip={nothing} onNext={nothing} />
        </Wizard>
      )
    case "browser":
      return (
        <Wizard items={ITEMS} current="browser" done={{ ...NONE, app: true }} skipped={NONE} onClose={nothing}>
          <BrowserStep pairing={waiting} opened={false} paired={false} onOpen={nothing} onBack={nothing}
            onLocal={nothing} onSkip={nothing} />
        </Wizard>
      )
    case "phone-apple":
    case "phone-android":
      return (
        <Wizard items={ITEMS} current="phone" done={{ ...NONE, app: true, browser: true }} skipped={NONE} onClose={nothing}>
          <PhoneStep pairing={waiting} platform={screen === "phone-android" ? "android" : "apple"}
            onRenew={nothing} onBack={nothing} onLocal={nothing} onSkip={nothing} />
        </Wizard>
      )
    case "done":
      return (
        <Wizard items={ITEMS} current={null} done={NONE} skipped={NONE} onClose={nothing}>
          <Finished items={ITEMS} done={{ app: true, browser: true, phone: false }} onGo={nothing} onClose={nothing} />
        </Wizard>
      )
    default:
      return (
        <Wizard items={ITEMS} current={null} done={NONE} skipped={NONE} onClose={nothing}>
          <Welcome onLocal={nothing} onStart={nothing} />
        </Wizard>
      )
  }
}

void (async () => {
  const response = await fetch(`/catalogs/${tag}.json`)
  activateCatalog(await response.json(), tag)
  document.documentElement.lang = tag
  document.documentElement.classList.remove("booting")
  createRoot(document.getElementById("root")!).render(<Screen />)
})()
