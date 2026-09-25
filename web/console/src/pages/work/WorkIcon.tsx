export type WorkIconName = "add" | "box" | "boxChecked" | "check" | "circle" | "close" | "delete" | "dot" | "edit" | "half" | "open" | "radio" | "remind" | "search"

/** Font glyph boxes are not optically centered. Work controls use one geometric icon canvas instead. */
export function WorkIcon({ name }: { name: WorkIconName }) {
  return <svg className="work-icon" viewBox="0 0 24 24" aria-hidden="true" focusable="false">
    {name === "add" && <path d="M12 5v14M5 12h14" />}
    {name === "box" && <rect x="5" y="5" width="14" height="14" rx="3.5" />}
    {name === "boxChecked" && <><rect x="5" y="5" width="14" height="14" rx="3.5" /><path d="m8.5 12.25 2.5 2.5 4.75-5" /></>}
    {name === "check" && <path d="m5.5 12.5 4 4 9-9" />}
    {name === "circle" && <circle cx="12" cy="12" r="7" />}
    {name === "close" && <path d="m7 7 10 10M17 7 7 17" />}
    {name === "delete" && <path d="M5 7h14M9 7V5h6v2m2 0-1 12H8L7 7m3 4v5m4-5v5" />}
    {name === "dot" && <circle cx="12" cy="12" r="3.25" className="work-icon-fill" />}
    {name === "edit" && <path d="m5 19 1-4 9.6-9.6 3 3L9 18l-4 1Zm8.7-11.7 3 3" />}
    {name === "half" && <><circle cx="12" cy="12" r="7" /><path d="M12 5a7 7 0 0 1 0 14Z" className="work-icon-fill" /></>}
    {name === "open" && <path d="M5 12h14m-5-5 5 5-5 5" />}
    {name === "radio" && <><circle cx="12" cy="12" r="7" /><circle cx="12" cy="12" r="3.25" className="work-icon-fill" /></>}
    {name === "remind" && <path d="M6 18 18 6m-8 0h8v8" />}
    {name === "search" && <><circle cx="10.5" cy="10.5" r="5.5" /><path d="m15 15 4 4" /></>}
  </svg>
}
