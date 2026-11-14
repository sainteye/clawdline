import { nextWord } from "../../next-strings.js"
import {
  ROLES_AFTER_STARTS, rolesMode, setRolesMode, setVoiceStartShown, useFirstRun, voiceStartShown, type RolesMode,
} from "../../session/first-run.js"
import { Switch } from "./Switch.js"
import "../../session/first-run.css"

const ROLE_CHOICES: [RolesMode, "displayRolesAuto" | "displayRolesShow" | "displayRolesHide"][] = [
  ["auto", "displayRolesAuto"],
  ["show", "displayRolesShow"],
  ["hide", "displayRolesHide"],
]

/**
 * This browser's display choices for first-run guidance: when the start
 * sheet's role row appears, and whether the voice start button is shown.
 * Like the font scale above it, nothing here reaches the machine.
 */
export function DisplayPrefsBlock() {
  useFirstRun()
  const roles = rolesMode()
  const voice = voiceStartShown()
  return (
    <div className="block" id="settings-display">
      <b id="settings-display-title">{nextWord("displayPrefsTitle")}</b>
      <p className="say">{nextWord("displayPrefsHint")}</p>

      <div className="settings-control-heading">
        <b id="settings-display-roles-title">{nextWord("displayRolesTitle")}</b>
        <div className="row display-roles-row" role="group" aria-labelledby="settings-display-roles-title">
          {ROLE_CHOICES.map(([mode, key]) => (
            <button key={mode} className="chip" type="button" id={`settings-display-roles-${mode}`}
              aria-pressed={roles === mode ? "true" : "false"} onClick={() => setRolesMode(mode)}>
              {nextWord(key)}
            </button>
          ))}
        </div>
      </div>
      <p className="say" id="settings-display-roles-say">{nextWord("displayRolesHint", { count: ROLES_AFTER_STARTS })}</p>

      <div className="settings-control-heading">
        <b id="settings-display-voice-title">{nextWord("displayVoiceTitle")}</b>
        <Switch id="settings-display-voice" labelledBy="settings-display-voice-title" describedBy="settings-display-voice-say"
          on={voice} disabled={false} onToggle={() => setVoiceStartShown(!voice)}
          stateText={nextWord(voice ? "browserSettingsOn" : "browserSettingsOff")} />
      </div>
      <p className="say" id="settings-display-voice-say">{nextWord("displayVoiceHint")}</p>
    </div>
  )
}
