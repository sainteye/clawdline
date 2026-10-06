import "./switch.css"

/**
 * One on/off setting on the Settings page: a `role="switch"` whose accessible
 * name is the setting's title and never its state, with the state written
 * beside the track (開／關) so it reads the same on a phone, where the row
 * stacks. The update panel's automatic updates and the two work gates share
 * it, so 「關」 means one thing on the page: this setting is off.
 */
export function Switch({
  id,
  labelledBy,
  describedBy,
  on,
  stateText,
  disabled,
  onToggle,
}: {
  id?: string
  /** The id of the setting's title; the switch is named by it alone. */
  labelledBy: string
  describedBy?: string
  on: boolean
  /** The word beside the track: on, off, or loading. */
  stateText: string
  disabled: boolean
  onToggle: () => void
}) {
  return (
    <button
      className={on ? "settings-switch on" : "settings-switch"}
      id={id}
      type="button"
      role="switch"
      aria-checked={on}
      aria-labelledby={labelledBy}
      aria-describedby={describedBy}
      disabled={disabled}
      onClick={onToggle}
    >
      <span className="settings-switch-track" aria-hidden="true">
        <span className="settings-switch-thumb" />
      </span>
      <span className="settings-switch-state" aria-hidden="true">{stateText}</span>
    </button>
  )
}
