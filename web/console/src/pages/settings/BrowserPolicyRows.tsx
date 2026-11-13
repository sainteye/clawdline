import { nextWord, type NextWord } from "../../next-strings.js"
import { openBrowserSetup } from "../../session/BrowserSetup.js"
import type { BrowserMode, BrowserPolicy, BrowserProfileReadiness } from "./api.js"
import { Switch } from "./Switch.js"

const TITLES: Record<BrowserMode, NextWord> = { native: "browserModeNative", isolated: "browserModeIsolated", user_chrome: "browserModeUserChrome" }
const HELP: Record<BrowserMode, NextWord> = { native: "browserModeNativeHelp", isolated: "browserModeIsolatedHelp", user_chrome: "browserModeUserChromeHelp" }

export function moveBrowser(policy: BrowserPolicy, browser: BrowserMode, direction: -1 | 1): BrowserPolicy {
  const index = policy.order.indexOf(browser), target = index + direction
  if (index < 0 || target < 0 || target >= policy.order.length) return policy
  const order = [...policy.order]
  ;[order[index], order[target]] = [order[target], order[index]]
  return { ...policy, order }
}

export function BrowserPolicyRows({ policy, profiles, busy, supported, onChange }: {
  policy: BrowserPolicy
  profiles?: Partial<Record<BrowserMode, BrowserProfileReadiness>>
  busy: boolean
  supported: boolean
  onChange: (value: BrowserPolicy) => void
}) {
  return <div className="browser-policy">
    {!supported && <p className="say">{nextWord("browserPolicyNeedsUpdate")}</p>}
    <ol className="browser-policy-list">
      {policy.order.map((browser, index) => {
        const title = nextWord(TITLES[browser])
        const automatic = policy.automatic[browser]
        const ready = browser === "native" || profiles?.[browser]?.ready === true
        const id = "browser-policy-" + browser
        return <li key={browser} data-browser={browser}>
          <div className="browser-policy-row">
            <span className="browser-policy-rank" aria-hidden="true">{index + 1}</span>
            <div className="browser-policy-copy">
              <strong id={id + "-title"}>{title}</strong>
              <p id={id + "-help"}>{nextWord(HELP[browser])}</p>
              <span className={ready ? "browser-policy-ready" : "browser-policy-unready"} role="status">{nextWord(browser === "native" ? "browserPolicyNativeReady" : ready ? "browserPolicyReady" : "browserPolicyDisconnected")}</span>
            </div>
            <div className="browser-policy-order">
              <button type="button" className="chip" aria-label={nextWord("browserPolicyUp", { browser: title })} disabled={busy || !supported || index === 0} onClick={() => onChange(moveBrowser(policy, browser, -1))}>↑</button>
              <button type="button" className="chip" aria-label={nextWord("browserPolicyDown", { browser: title })} disabled={busy || !supported || index === policy.order.length - 1} onClick={() => onChange(moveBrowser(policy, browser, 1))}>↓</button>
            </div>
          </div>
          <div className="browser-policy-auto">
            <span id={id + "-auto"}>{nextWord("browserPolicyAuto")}</span>
            <Switch id={id} labelledBy={id + "-title " + id + "-auto"} describedBy={id + "-help"} on={automatic}
              disabled={busy || !supported || (!automatic && !ready)} stateText={nextWord(automatic ? "browserSettingsOn" : "browserSettingsOff")}
              onToggle={() => onChange({ ...policy, automatic: { ...policy.automatic, [browser]: !automatic } })} />
          </div>
          <details className="browser-policy-setup">
            <summary>{nextWord("browserPolicySetup")}</summary>
            {browser === "native" ? <p>{nextWord("browserPolicyNativeSetup")}</p> : <>
              {!ready && <p>{nextWord("browserPolicyNeedSetup")}</p>}
              <button type="button" className="chip" onClick={() => openBrowserSetup(browser)}>{nextWord("browserPolicySetUp")}</button>
            </>}
          </details>
        </li>
      })}
    </ol>
  </div>
}
