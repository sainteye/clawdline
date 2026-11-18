export function StatusIcon({ name }: { name: "clock" | "alert" | "up" | "down" }) {
  return <svg className="control-status-icon" width="16" height="16" viewBox="0 0 24 24"
    fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"
    aria-hidden="true" focusable="false">
    {name === "clock" && <><circle cx="12" cy="12" r="8" /><path d="M12 7v5l3 2" /></>}
    {name === "alert" && <><path d="M12 3 2.5 20h19L12 3Z" /><path d="M12 9v4m0 3h.01" /></>}
    {name === "up" && <path d="M12 19V5m-5 5 5-5 5 5" />}
    {name === "down" && <path d="M12 5v14m-5-5 5 5 5-5" />}
  </svg>
}
