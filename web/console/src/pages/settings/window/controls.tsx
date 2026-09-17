import type { ReactNode } from "react"

/**
 * The window's furniture, as the AppKit original draws it.
 *
 * `Settings.swift` and `WindowChrome.swift` hand-draw every control in that
 * window — the switch, the chip, the slider's filled track, the tab strip's
 * accent rule — rather than use the system's, because the window is pinned dark
 * and an `NSButton` in `.rounded` is the one thing in it that arrived from
 * somewhere else. The same reasoning lands here: these are the same shapes at
 * the same measurements, in `window.css`, and the measurements are copied from
 * `Metric` rather than guessed from a screenshot.
 *
 * Nothing here knows what a setting is. A row takes a label, a hint and a
 * control; what the control does is the window's business.
 */

/** `SettingsColumn.row`: what it is on the left, the thing that changes it on the right. */
export function Row({
  label,
  hint,
  children,
  first,
}: {
  label: string
  hint?: string
  children: ReactNode
  /** The original draws a hairline between rows, never above the first one. */
  first?: boolean
}) {
  return (
    <>
      {!first && <div className="sw-hairline" />}
      <div className="sw-row">
        <span className="sw-row-label">{label}</span>
        <span className="sw-row-control">{children}</span>
      </div>
      {hint ? <p className="sw-hint">{hint}</p> : null}
    </>
  )
}

/** `SettingsColumn.block`: a thing that is not a row, with an optional label over it. */
export function Block({ label, hint, children }: { label?: string; hint?: string; children: ReactNode }) {
  return (
    <div className="sw-block">
      {label ? <span className="sw-block-label">{label}</span> : null}
      {children}
      {hint ? <p className="sw-hint sw-hint-block">{hint}</p> : null}
    </div>
  )
}

/** `SettingsColumn.head`: the small capitals over a group inside a column. */
export function Head({ children }: { children: string }) {
  return <div className="sw-head">{children.toUpperCase()}</div>
}

/** `SettingsColumn.mono`: a path, selectable, truncated in the middle. */
export function Mono({ children }: { children: string }) {
  return <div className="sw-mono">{children}</div>
}

/**
 * `SwitchView`: 38×20, the knob at 14, the accent behind it once it is on.
 *
 * A real checkbox underneath, so the keyboard and a screen reader meet the
 * control the platform has always had; the drawn part is the label's.
 */
export function Switch({
  on,
  disabled,
  label,
  onChange,
}: {
  on: boolean
  disabled?: boolean
  label: string
  onChange: (on: boolean) => void
}) {
  return (
    <label className={"sw-switch" + (on ? " on" : "") + (disabled ? " off-limits" : "")}>
      <input
        type="checkbox"
        checked={on}
        disabled={disabled}
        aria-label={label}
        onChange={(e) => onChange(e.currentTarget.checked)}
      />
      <span className="sw-switch-track" aria-hidden="true">
        <span className="sw-switch-knob" />
      </span>
    </label>
  )
}

/** `ChipButton`: the shape a session row's buttons have. `armed` is the recorder listening. */
export function Chip({
  children,
  onClick,
  armed,
  disabled,
  title,
  wide,
}: {
  children: ReactNode
  onClick?: () => void
  armed?: boolean
  disabled?: boolean
  title?: string
  /** `minimumWidth`, for the recorder — a chip must not resize as it records. */
  wide?: boolean
}) {
  return (
    <button
      type="button"
      className={"sw-chip" + (armed ? " armed" : "") + (wide ? " wide" : "")}
      aria-pressed={armed ? true : undefined}
      disabled={disabled}
      title={title}
      onClick={onClick}
    >
      {children}
    </button>
  )
}

/** `ChoicePopUp`: an `NSPopUpButton` clamped between 130 and 210 points. */
export function PopUp({
  value,
  options,
  label,
  disabled,
  onPick,
}: {
  value: string
  options: { label: string; value: string }[]
  label: string
  disabled?: boolean
  onPick: (value: string) => void
}) {
  // A hand-edited value the list does not hold still has to be shown, or the
  // popup would silently read as the first option and a pick would write it.
  const known = options.some((o) => o.value === value)
  return (
    <span className="sw-popup">
      <select
        aria-label={label}
        value={value}
        disabled={disabled}
        onChange={(e) => onPick(e.currentTarget.value)}
      >
        {!known && value ? <option value={value}>{value}</option> : null}
        {options.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </select>
      <svg className="sw-popup-mark" viewBox="0 0 8 10" aria-hidden="true" focusable="false">
        <path d="M1 4 4 1l3 3M1 6l3 3 3-3" fill="none" stroke="currentColor" strokeWidth="1.2" />
      </svg>
    </span>
  )
}

/**
 * `ValueSlider`: the slider and the number it is at, as one control.
 *
 * The readout is not decoration. Width and opacity are both a knob between two
 * ends, and without it the only way to find out what was just set is to go and
 * read the config file.
 */
export function Slider({
  value,
  min,
  max,
  step,
  label,
  format,
  disabled,
  onChange,
  onCommit,
}: {
  value: number
  min: number
  max: number
  step: number
  label: string
  format: (value: number) => string
  disabled?: boolean
  /** Every move, for the readout. */
  onChange: (value: number) => void
  /** The end of a drag, which is what is written. */
  onCommit: (value: number) => void
}) {
  const filled = max > min ? ((value - min) / (max - min)) * 100 : 0
  return (
    <span className="sw-slider">
      <input
        type="range"
        aria-label={label}
        min={min}
        max={max}
        step={step}
        value={value}
        disabled={disabled}
        style={{ ["--sw-filled" as string]: `${filled}%` }}
        onChange={(e) => onChange(Number(e.currentTarget.value))}
        onPointerUp={(e) => onCommit(Number(e.currentTarget.value))}
        onKeyUp={(e) => onCommit(Number(e.currentTarget.value))}
      />
      <span className="sw-readout">{format(value)}</span>
    </span>
  )
}

/**
 * `NoteCard`: a sentence with a mark in front of it and sometimes a button
 * after it — the places in this window that report rather than ask.
 *
 * Setting those apart from the rows is what keeps a reading from looking like a
 * setting you failed to change.
 */
export function Note({
  dot = "idle",
  mono,
  children,
  trailing,
}: {
  dot?: "idle" | "warn" | "live"
  mono?: boolean
  children: ReactNode
  trailing?: ReactNode
}) {
  return (
    <div className="sw-note">
      <span className={"sw-dot " + dot} aria-hidden="true" />
      <span className={mono ? "sw-note-text mono" : "sw-note-text"}>{children}</span>
      {trailing ? <span className="sw-note-trailing">{trailing}</span> : null}
    </div>
  )
}

/** `MemoField`: a text box that leaves behind what it used to hold as its placeholder. */
export function MemoField({
  value,
  example,
  label,
  disabled,
  onCommit,
}: {
  value: string
  example: string
  label: string
  disabled?: boolean
  onCommit: (value: string) => void
}) {
  return (
    <input
      className="sw-field"
      type="text"
      aria-label={label}
      defaultValue={value}
      key={value}
      disabled={disabled}
      placeholder={value || example}
      spellCheck={false}
      autoComplete="off"
      onBlur={(e) => onCommit(e.currentTarget.value.trim())}
      onKeyDown={(e) => {
        if (e.key === "Enter") e.currentTarget.blur()
      }}
    />
  )
}

/** `TabStrip`: the words, with an accent rule under the one you are on. */
export function TabStrip({
  titles,
  current,
  onPick,
}: {
  titles: string[]
  current: number
  onPick: (index: number) => void
}) {
  return (
    <div className="sw-strip" role="tablist" aria-label="Clawdline">
      <span className="sw-strip-mark" aria-hidden="true" />
      {titles.map((title, i) => (
        <button
          key={title}
          type="button"
          role="tab"
          id={`sw-tab-${i}`}
          aria-selected={i === current}
          aria-controls={`sw-pane-${i}`}
          tabIndex={i === current ? 0 : -1}
          className={"sw-tab" + (i === current ? " current" : "")}
          onClick={() => onPick(i)}
          onKeyDown={(e) => {
            if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return
            e.preventDefault()
            const step = e.key === "ArrowRight" ? 1 : -1
            const next = (current + step + titles.length) % titles.length
            onPick(next)
            document.getElementById(`sw-tab-${next}`)?.focus()
          }}
        >
          {title}
        </button>
      ))}
    </div>
  )
}
