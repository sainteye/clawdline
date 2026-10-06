import { useState } from "react"
import { browserPreference, CATALOG_TAGS, catalogWord, catalogWordLanguage, saveBrowserPreference } from "../catalog.js"
import "./browser-language.css"

const names: Record<(typeof CATALOG_TAGS)[number], string> = {
  en: "English",
  "zh-Hant": "繁體中文",
  ja: "日本語",
  "zh-Hans": "简体中文",
  ko: "한국어",
  es: "Español",
  "pt-BR": "Português (Brasil)",
  fr: "Français",
  de: "Deutsch",
}

/** Browser-local UI choice. The saved value is never the daemon's agent or voice setting. */
export function BrowserLanguageControl({ id, compact = false }: { id: string; compact?: boolean }) {
  const [preference, setPreference] = useState(browserPreference)
  const [state, setState] = useState<"ready" | "saving" | "failed">("ready")
  const unsupported = preference !== "auto" && !CATALOG_TAGS.includes(preference as (typeof CATALOG_TAGS)[number])
  const label = catalogWord("ui", "language")
  const choose = (value: string) => {
    setState("saving")
    if (!saveBrowserPreference(value)) {
      setState("failed")
      return
    }
    setPreference(value)
    location.reload()
  }
  return (
    <div className={`ui-language-control${compact ? " compact" : ""}`}>
      {!compact && <label htmlFor={id} className="ui-language-label" lang={catalogWordLanguage("ui", "language")}>{label}</label>}
      <select id={id} aria-label={compact ? label : undefined} lang={compact ? catalogWordLanguage("ui", "language") : undefined} value={preference} disabled={state === "saving"} onChange={(event) => choose(event.currentTarget.value)}>
        <option value="auto" lang={catalogWordLanguage("ui", "auto")}>{catalogWord("ui", "auto")}</option>
        {CATALOG_TAGS.map((tag) => <option value={tag} key={tag} lang={tag}>{names[tag]}</option>)}
        {unsupported && <option value={preference} lang={catalogWordLanguage("ui", "unsupportedValue")}>{catalogWord("ui", "unsupportedValue").replace("{value}", preference)}</option>}
      </select>
      {!compact && <p className="ui-language-hint" lang={catalogWordLanguage("ui", "browserOnly")}>{catalogWord("ui", "browserOnly")}</p>}
      {unsupported && <p className="ui-language-warning" role="status" lang={catalogWordLanguage("ui", "unsupportedHint")}>{catalogWord("ui", "unsupportedHint")}</p>}
      <p className="ui-language-status" role="status" aria-live="polite" lang={catalogWordLanguage("ui", state === "failed" ? "saveFailed" : "applying")}>
        {state === "saving" ? catalogWord("ui", "applying") : state === "failed" ? catalogWord("ui", "saveFailed") : ""}
      </p>
    </div>
  )
}
