import { useState, useSyncExternalStore } from "react"
import { nextWord } from "../next-strings.js"
import { Switch } from "../pages/settings/Switch.js"
import { quotaVisible, setQuotaVisible, subscribeQuotaVisible } from "./visibility.js"
export function QuotaSetting() {
  const preference = useSyncExternalStore(subscribeQuotaVisible, quotaVisible)
  const [saved, setSaved] = useState(true)
  return <div className="block" id="settings-provider-quota">
    {(["desktop", "mobile"] as const).map((layout) => <div className="settings-control-heading quota-setting-toggle" key={layout}>
      <b id={`settings-provider-quota-${layout}-title`}>{nextWord(layout === "desktop" ? "quotaSettingDesktopTitle" : "quotaSettingMobileTitle")}</b>
      <Switch id={`settings-provider-quota-${layout}-switch`} labelledBy={`settings-provider-quota-${layout}-title`}
        describedBy="settings-provider-quota-hint" on={preference[layout]} disabled={false}
        stateText={nextWord(preference[layout] ? "quotaOn" : "quotaOff")}
        onToggle={() => setSaved(setQuotaVisible(layout, !preference[layout]))} />
    </div>)}
    <p className="say" id="settings-provider-quota-hint">{nextWord("quotaSettingHint")}</p>
    {!saved && <p className="said" role="status">{nextWord("quotaSaveFailed")}</p>}
  </div>
}
